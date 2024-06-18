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
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/bazelbuild/reclient/internal/pkg/bigquerytranslator"
	"github.com/eapache/go-resiliency/retrier"
	log "github.com/golang/glog"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"google.golang.org/grpc/credentials/oauth"
	"google.golang.org/protobuf/encoding/protojson"
)

const MaxUploadSize = 8 * 1000 * 1000 // slightly less than 10MB to compensate overhead.

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
	Err         atomic.Pointer[error]
	ProjectID   string
	TableSpec   string
	BatchSizeMB int
	Client      *bigquery.Inserter
	CleanUp     func() error
	Ctx         context.Context
	Cancel      context.CancelFunc
}

// batch fetches LogRecords from a channel, groups and saves them in batches.
func batch(items <-chan *bigquerytranslator.Item, batches chan<- []*bigquerytranslator.Item, batchSizeMB int) {
	var buffer []*bigquerytranslator.Item
	batchSizeBytes := batchSizeMB * 1000 * 1000
	if batchSizeBytes > MaxUploadSize {
		batchSizeBytes = MaxUploadSize
	}
	bufferSize := 0
	for {
		select {
		case data, ok := <-items:
			if !ok {
				batches <- buffer
				buffer = []*bigquerytranslator.Item{}
				bufferSize = 0
				close(batches)
				return
			}
			bytes, err := protojson.Marshal(*data)
			if err != nil {
				log.Errorf("marshal LogRecord error: %v", err)
				continue
			}
			size := len(bytes)
			if size > batchSizeBytes {
				log.Errorf("single log record size (%v Bytes) is larger than the max batch size (%v Bytes)", size, batchSizeBytes)
				continue
			}
			if size+bufferSize > batchSizeBytes {
				batches <- buffer
				buffer = []*bigquerytranslator.Item{}
				bufferSize = 0
			}
			buffer = append(buffer, data)
			bufferSize += size
		}
	}
}

// upload uploads batches of LogRecord to bigquery and updates the status.
func upload(bqSpec *BQSpec, batches <-chan []*bigquerytranslator.Item, successful *int32, failed *int32) {
	var wg sync.WaitGroup
	for {
		select {
		case items, ok := <-batches:
			if !ok {
				wg.Wait()
				return
			} else {
				wg.Add(1)
				go func(bqSpec *BQSpec, items []*bigquerytranslator.Item, successful *int32, failed *int32) {
					uploadWithRetry(bqSpec, items, successful, failed)
					wg.Done()
				}(bqSpec, items, successful, failed)
			}
		}
	}
}

// BQClassifier can classify bigquery related Errors for a Retrier.
// This Classifier interface is defined in
// https://github.com/eapache/go-resiliency/blob/main/retrier/classifier.go#L15
type BQClassifier struct {
	bQSpec *BQSpec
}

// Classify makes BQClassifier implements the Classifier interface, it
// classifies 400-level error codes as non-retryable.
func (b BQClassifier) Classify(err error) retrier.Action {
	if err == nil {
		return retrier.Succeed
	}
	var apiError *googleapi.Error
	errors.As(err, &apiError)
	// For errors, such as no permission, wrong project name, wrong dataset/table name,
	// they are considered as non-retryable errors.
	if apiError.Code >= 400 {
		fatalErr := fmt.Errorf("non retryable error occurred in uploading LogRecords to BigQuery: %v", err)
		b.bQSpec.Err.Store(&fatalErr)
		return retrier.Fail
	}
	return retrier.Retry
}

func uploadWithRetry(bqSpec *BQSpec, items []*bigquerytranslator.Item, successful *int32, failed *int32) {
	r := retrier.New(retrier.ExponentialBackoff(3, 1*time.Second), BQClassifier{bqSpec})
	start := time.Now()
	// From experiments, uploading 10M LogRecords takes about 1s, setting
	// timeout here for 1 minute should be more than enough, and we will not retry
	// if this function runs more than 1 minute.
	ctx, cancel := context.WithTimeout(bqSpec.Ctx, 1*time.Minute)
	defer cancel()
	err := r.RunCtx(ctx, func(ctx context.Context) error {
		return bqSpec.Client.Put(ctx, items)
	})

	elapsed := time.Since(start)
	if err != nil {
		atomic.AddInt32(failed, int32(len(items)))
		log.Errorf("%v records failed to be uploaded to BigQuery in %v: %v", len(items), elapsed, err)
		return
	}
	atomic.AddInt32(successful, int32(len(items)))
	// TODO: add the elapsed time to RRPL log so we can get aggregated data.
	log.Infof("%v records successfully uploaded to BigQuery in %v", len(items), elapsed)
	return
}

// LogRecordsToBigQuery batches LogRecords, and uploads each batch to bigquery.
func LogRecordsToBigQuery(bqSpec *BQSpec, items <-chan *bigquerytranslator.Item, successful *int32, failed *int32) {
	var wg sync.WaitGroup
	batches := make(chan []*bigquerytranslator.Item, 100)
	// A goroutine to batch, and upload from the channel, this should block the main thread.
	wg.Add(1)
	go func() {
		batch(items, batches, bqSpec.BatchSizeMB)
		wg.Done()
	}()
	// A goroutine to analysis the result, updating how many successful and failed uploads.
	wg.Add(1)
	go func() {
		upload(bqSpec, batches, successful, failed)
		wg.Done()
	}()
	wg.Wait()
	bqSpec.CleanUp()
	bqSpec.Cancel()
}
