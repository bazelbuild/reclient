// Copyright 2023 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bigquery

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"cloud.google.com/go/bigquery"
	lpb "github.com/bazelbuild/reclient/api/log"
	"github.com/bazelbuild/reclient/internal/pkg/bigquerytranslator"
	log "github.com/golang/glog"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/option"
	"google.golang.org/grpc/credentials/oauth"
)

// parseResourceSpec parses spec as per the bq command-line tool format described
// here: https://cloud.google.com/bigquery/docs/reference/bq-cli-reference#resource_specification.
// spec should be in the form <project>:<dataset>.<table> or <dataset>.<table>
func parseResourceSpec(spec, defaultProject string) (project, dataset, table string, err error) {
	project = defaultProject
	if strings.ContainsRune(spec, ':') {
		chunks := strings.Split(spec, ":")
		if len(chunks) != 2 {
			return "", "", "", fmt.Errorf("bigquery resource spec should have form <project>:<dataset>.<table> or <dataset>.<table>, got %s", spec)
		}
		project = chunks[0]
		spec = chunks[1]
	}
	if project == "" {
		return "", "", "", fmt.Errorf("project not specified, either BQ resource spec should have form <project>:<dataset>.<table> or a default project should be specified")
	}
	chunks := strings.Split(spec, ".")
	if len(chunks) != 2 {
		return "", "", "", fmt.Errorf("bigquery resource spec should have form <project>:<dataset>.<table> or <dataset>.<table>, got %s", spec)
	}
	return project, chunks[0], chunks[1], nil
}

// NewInserter creates an inserter for the table specified by the given resourceSpec in
// the form <project>:<dataset>.<table> or <dataset>.<table>
func NewInserter(ctx context.Context, resourceSpec, defaultProject string, ts *oauth.TokenSource) (inserter *bigquery.Inserter, cleanup func() error, err error) {
	project, dataset, table, err := parseResourceSpec(resourceSpec, defaultProject)
	cleanup = func() error { return nil }
	if err != nil {
		return nil, cleanup, err
	}
	var opts []option.ClientOption
	if ts != nil {
		opts = append(opts, option.WithTokenSource(ts))
	}
	client, err := bigquery.NewClient(ctx, project, opts...)
	if err != nil {
		return nil, cleanup, err
	}
	return client.Dataset(dataset).Table(table).Inserter(), client.Close, nil
}

// BQSpec defines which bigquery table the LogRecords will be saved.
type BQSpec struct {
	ProjectID  string
	TableSpec  string
	BatchSize  int
	Concurrent int
	Timeout    time.Duration
}

// InsertRows inserts a slice of LogRecords into a BigQuery table and returns
// the number of rows that failed to insert, along with any associated errors.
func InsertRows(ctx context.Context, logs []*lpb.LogRecord, bqSpec BQSpec, logEnabled bool) (int32, error) {
	processed := int32(0)
	total := int32(len(logs))
	var failed atomic.Int32
	startTime := time.Now()

	defer func() {
		elapsedTime := time.Since(startTime)
		log.Infof("Uploading %v rows of LogRecords to bigquery, %v failed to insert, elapsed time: %v", total, failed.Load(), elapsedTime)
	}()

	//No items to load to bigquery.
	if total < 1 {
		return failed.Load(), nil
	}

	inserter, cleanup, err := NewInserter(ctx, bqSpec.TableSpec, bqSpec.ProjectID, nil)
	defer cleanup()
	if err != nil {
		return failed.Load(), fmt.Errorf("bigquery.NewInserter: %v", err)
	}

	items := make(chan *bigquerytranslator.Item, bqSpec.BatchSize)

	g, _ := errgroup.WithContext(context.Background())

	for i := 0; i < bqSpec.Concurrent; i++ {
		g.Go(func() error {
			for item := range items {
				if err := inserter.Put(ctx, item); err != nil {
					// In case of error (gpaste/6313679673360384), retrying the job with
					// back-off as described in BigQuery Service Level Agreement
					// https://cloud.google.com/bigquery/sla
					time.Sleep(1 * time.Second)
					if err := inserter.Put(ctx, item); err != nil {
						failed.Add(1)
						return err
					}
				}
				atomic.AddInt32(&processed, 1)
			}
			return nil
		})
	}
	if logEnabled {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		go func() {
			for range t.C {
				log.Infof("Finished %v/%v items...", atomic.LoadInt32(&processed), total)
			}
		}()
	}
	for _, r := range logs {
		items <- &bigquerytranslator.Item{r}
	}
	close(items)

	if err := g.Wait(); err != nil {
		log.Errorf("%v rows having error while uploading to bigquery: %v", failed.Load(), err)
	}
	return failed.Load(), nil
}
