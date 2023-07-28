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

// Binary biqeury is used to stream an reproxy_log generated from a build
// using re-client to bigquery so that it can be further queried upon.
//
// Example invocation (assuming the bigquery table already exists):
//
//	bazelisk run //cmd/bigquery:bigquery -- \
//	  --log_path text:///tmp/reproxy_log.txt \
//	  --alsologtostderr=true \
//	  --table_id <bigquery-table-id> \
//	  --dataset_id <bigquery-dataset-id> \
//	  --project_id <gcp-project-id> # (ex:"foundry-x-experiments")
//
// If you don't have a bigquery table yet, you can create it using the following steps:
//  1. Run scripts/gen_reproxy_log_big_query_schema.sh
//  2. Run the following command using "bq" tool to create table:
//     bq mk --table \
//     --expiration 600 \ # in seconds. This argument is optional and the table doesn't expire if you don't set it.
//     foundry-x-experiments:reproxylogs.reproxy_log_1 \ # Format: <project-id>:<dataset-id>.<table-id>
//     `pwd`/reproxy_log_bigquery_schema/proxy/reproxy_log.schema
//
// Note: It can take upto 5mins for the bigquery table to become active
// after it is created.
package main

import (
	"context"
	"flag"
	"fmt"
	"sync"
	"time"

	"github.com/bazelbuild/reclient/internal/pkg/bigquery"
	"github.com/bazelbuild/reclient/internal/pkg/bigquerytranslator"
	"github.com/bazelbuild/reclient/internal/pkg/logger"
	"github.com/bazelbuild/reclient/internal/pkg/rbeflag"

	lpb "github.com/bazelbuild/reclient/api/log"

	log "github.com/golang/glog"
	"golang.org/x/sync/errgroup"
)

var (
	// TODO: support --proxy_log_dir.
	logPath          = flag.String("log_path", "", "If provided, the path to a log file of all executed records. The format is e.g. text:///full/file/path.")
	projectID        = flag.String("project_id", "foundry-x-experiments", "The project containing the big query table to which log records should be streamed to.")
	tableSpec        = flag.String("table", "reproxy_log_test.test_1", "Resource specifier of the BigQuery to which log records should be streamed to. If the project is not provided in the specifier project_id will be used.")
	numConcurrentOps = flag.Int("num_concurrent_uploads", 100, "Number of concurrent upload operations to perform.")
)

func insertRows(logs []*lpb.LogRecord) error {
	ctx := context.Background()
	inserter, cleanup, err := bigquery.NewInserter(ctx, *tableSpec, *projectID, nil)
	defer cleanup()
	if err != nil {
		return fmt.Errorf("bigquery.NewInserter: %v", err)
	}

	items := make(chan *bigquerytranslator.Item, *numConcurrentOps)

	g, _ := errgroup.WithContext(context.Background())

	var processed int32
	var processedMu sync.Mutex

	for i := 0; i < *numConcurrentOps; i++ {
		g.Go(func() error {
			for item := range items {
				if err := inserter.Put(ctx, item); err != nil {
					return err
				}
				processedMu.Lock()
				processed++
				processedMu.Unlock()
			}
			return nil
		})
	}
	go func() {
		for range time.Tick(5 * time.Second) {
			processedMu.Lock()
			log.Infof("Finished %v/%v items...", int(processed), len(logs))
			processedMu.Unlock()
		}
	}()
	if len(logs) < 1 {
		log.Infof("No items to load to bigquery.")
		return nil
	}
	log.Infof("Total number of items: %v", len(logs))
	for _, r := range logs {
		items <- &bigquerytranslator.Item{r}
	}
	close(items)

	if err := g.Wait(); err != nil {
		log.Errorf("Error while uploading to bigquery: %v", err)
	}
	return nil
}

func main() {
	defer log.Flush()
	rbeflag.Parse()
	if *logPath == "" {
		log.Fatal("Must provide proxy log path.")
	}

	log.Infof("Loading stats from %v...", *logPath)
	logRecords, err := logger.ParseFromFormatFile(*logPath)
	if err != nil {
		log.Fatalf("Failed reading proxy log: %v", err)
	}

	log.Infof("Inserting stats into bigquery table...")
	if err := insertRows(logRecords); err != nil {
		log.Fatalf("Unable to insert records into bigquery table: %+v", err)
	}
}
