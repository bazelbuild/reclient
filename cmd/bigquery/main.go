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

/*
Example invocation (assuming the bigquery table already exists):

	bazelisk run //cmd/bigquery:bigquery -- \
	  --log_path text:///tmp/reproxy_log.txt \
	  --alsologtostderr=true \
	  --table <bigquery-dataset-id>.<bigquery-table-id> \
	  --project_id <gcp-project-id> # (ex:"foundry-x-experiments")

If you don't have a bigquery table yet, you can create it using the following steps:

 1. Run scripts/gen_reproxy_log_big_query_schema.sh

 2. Run the following command using "bq" tool to create table:

    bq mk --table \
    `# expiration time in seconds. This argument is optional and the table doesn't expire if you don't set it.` \
    --expiration 600 \
    `# Format: <project-id>:<dataset-id>.<table-id>` \
    foundry-x-experiments:reproxylogs.reproxy_log_1 \
    `pwd`/reproxy_log_bigquery_schema/log/log.schema

Note: It can take upto 5mins for the bigquery table to become active
after it is created.
*/
package main

import (
	"context"
	"flag"

	"github.com/bazelbuild/reclient/internal/pkg/bigquery"
	"github.com/bazelbuild/reclient/internal/pkg/logger"
	"github.com/bazelbuild/reclient/internal/pkg/rbeflag"
	log "github.com/golang/glog"
)

var (
	// TODO: support --proxy_log_dir.
	logPath   = flag.String("log_path", "", "If provided, the path to a log file of all executed records. The format is e.g. text:///full/file/path.")
	projectID = flag.String("project_id", "foundry-x-experiments", "The project containing the big query table to which log records should be streamed to.")
	tableSpec = flag.String("table", "reproxy_log_test.test_1", "Resource specifier of the BigQuery to which log records should be streamed to. If the project is not provided in the specifier project_id will be used.")
	// The default value of 2k for num_concurrent_uploads/batch_size was chosen empirically.
	numConcurrentOps = flag.Int("num_concurrent_uploads", 2000, "Number of concurrent upload operations to perform.")
	batchSize        = flag.Int("batch_size", 2000, "Number of LogRecords to share one BigQuery Client.")
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

	log.Infof("Inserting stats into bigquery table...")
	bqSpecs := bigquery.BQSpec{
		ProjectID:  *projectID,
		TableSpec:  *tableSpec,
		BatchSize:  *batchSize,
		Concurrent: *numConcurrentOps,
	}
	ctx := context.Background()
	if failed, err := bigquery.InsertRows(ctx, logRecords, bqSpecs, true); err != nil {
		log.Exitf("%v records out of %v failed to be inserted into bigquery table: %+v", failed, len(logRecords), err)
	}
}
