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

// Package main is the main package for the experiment tabulator binary.
//
// This binary stores an experiment's results into a BigQuery table for
// easy querying.
//
// Example:
// bazelisk run //experiments/cmd/tabulator:tabulator -- --experiment_name=BouncyRacing-20201005-1940
package main

import (
	"flag"

	"team/foundry-x/re-client/experiments/internal/pkg/tabulator"

	log "github.com/golang/glog"
)

const (
	dataset = "results"
	table   = "results"
)

var (
	gcpProject = flag.String("gcp_project", "foundry-x-experiments", "Name of the GCP project to store bigquery data in")
	resBucket  = flag.String("results_bucket", "foundry-x-experiments-results", "Name of the GCS bucket containing experiment results")
	expName    = flag.String("experiment_name", "", "Name of the experiment folder in the results bucket")
)

func main() {
	flag.Parse()
	if err := tabulator.RunExperimentResultCollector(*resBucket, *expName, *gcpProject); err != nil {
		log.Fatalf("Error running tabulator: %v", err)
	}
}
