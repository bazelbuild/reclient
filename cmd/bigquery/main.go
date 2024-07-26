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
// Note that due to b/279056853, we only support TextFormat and ReducedTextFormat
// log files. Valid log_path could be (see: http://shortn/_3fVE7Zeuey):
// 1. --log_path text:///tmp/reproxy_log.rpl
// 2. --log_path reducedtext:///tmp/reproxy_log.rrpl

/*
Example invocation (assuming the bigquery table already exists):

	bazelisk run //cmd/bigquery:bigquery -- \
	  --log_path text:///tmp/reproxy_log.rpl \
	  --alsologtostderr=true \
	  --table <bigquery-dataset-id>.<bigquery-table-id> \
	  --project_id <gcp-project-id> # (ex:"foundry-x-experiments")

If you don't have a bigquery table yet, you can create it using the following steps:

 1. Run bazelisk build //api/log:log_bq_schema_proto

 2. Run the following command using "bq" tool to create table:

    bq mk --table \
    `# expiration time in seconds. This argument is optional and the table doesn't expire if you don't set it.` \
    --expiration 600 \
    `# Format: <project-id>:<dataset-id>.<table-id>` \
    foundry-x-experiments:reproxylogs.reproxy_log_1 \
    bazel-bin/api/log/log.schema

Note: It can take upto 5mins for the bigquery table to become active
after it is created.
*/
package main

import (
	"context"
	"flag"

	lpb "github.com/bazelbuild/reclient/api/log"
	"github.com/bazelbuild/reclient/internal/pkg/bigquery"
	"github.com/bazelbuild/reclient/internal/pkg/bigquerytranslator"
	"github.com/bazelbuild/reclient/internal/pkg/logger"
	"github.com/bazelbuild/reclient/internal/pkg/rbeflag"
	log "github.com/golang/glog"
)

var (
	// TODO: support --proxy_log_dir.
	bqProjectID = flag.String("bq_project", "foundry-x-experiments", "Project where log records are stored.")
	bqTableSpec = flag.String("bq_table", "reproxy_log_test.test_1", "Table where log records are stored.")
	// 8M byte limit for marshaled JSON data is empirically derived. BigQuery's REST API has a 10M upload payload limit, but JSON encoding adds overhead.
	bqBatchMB = flag.Int("bq_batch_mb", 8, "Batch size in MB for bigquery uploading, defaults to 8MB and should not be larger than 8 MB")
	logPath   = flag.String("log_path", "", "If provided, the path to a log file of all executed records. The format is e.g. text:///full/file/path.rpl.")
)

func main() {
	defer log.Flush()
	rbeflag.Parse()
	if *logPath == "" {
		log.Exit("Must provide proxy log path.")
	}

	log.Infof("Loading stats from %v...", *logPath)
	logRecords, err := logger.ParseFromFormatFile(*logPath)
	if err != nil {
		log.Exitf("Failed reading proxy log: %v", err)
	}
	log.Infof("Loaded %v log records from %v. to cache.", len(logRecords), *logPath)

	ctx, cancel := context.WithCancel(context.Background())
	client, cleanUp, err := bigquery.NewInserter(ctx, *bqTableSpec, *bqProjectID, nil)
	if err != nil {
		log.Exitf("Failed creating bigquery client: %v", err)
	}
	bqSpec := &bigquery.BQSpec{
		ProjectID:   *bqProjectID,
		TableSpec:   *bqTableSpec,
		BatchSizeMB: *bqBatchMB,
		Client:      client,
		CleanUp:     cleanUp,
		Ctx:         ctx,
		Cancel:      cancel,
	}

	items := make(chan *bigquerytranslator.Item, 1000)
	var successful int32
	var failed int32
	go marshalLogRecords(logRecords, items)
	bigquery.LogRecordsToBigQuery(bqSpec, items, &successful, &failed)
	log.Infof("Totally %v log records in file, %v successful, %v failed", len(logRecords), successful, failed)
}

func marshalLogRecords(logRecords []*lpb.LogRecord, items chan<- *bigquerytranslator.Item) {
	for _, r := range logRecords {
		items <- &bigquerytranslator.Item{LogRecord: r}
	}
	close(items)
}
