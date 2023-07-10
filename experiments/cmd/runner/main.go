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

// Package main is the main package for the experiment runner binary.
//
// Use the runner tool to run experiments defined in an experiment textproto
// file. Example:
//
// bazelisk run //experiments/cmd/runner:runner -- --exp_def_path=$PWD/experiments/examples/multi-region.textproto
//
// To view logging info as the experiment is running, run with --alsologtostderr.
package main

import (
	"flag"
	"fmt"

	"team/foundry-x/re-client/experiments/internal/pkg/runner"

	log "github.com/golang/glog"
)

var (
	gcpProject   = flag.String("gcp_project", "foundry-x-experiments", "Name of the GCP project to run experiments on")
	resBucket    = flag.String("results_bucket", "foundry-x-experiments-results", "Name of the GCS bucket to store results")
	expDefPath   = flag.String("exp_def_path", "", "Path to the .textproto file containing the experiment definition")
	dateSuffix   = flag.String("date_suffix", "", "Date string suffix to use instead of checking the current time")
	logFrequency = flag.Int("log_frequency", -1, "Time between polling the log file for updates, in seconds. Default 5 for virtual machine experiments, 0 (disabled) for workstation experiments")
)

func main() {
	flag.Parse()
	expName, err := runner.RunExperiment(*expDefPath, *dateSuffix, *gcpProject, *resBucket, *logFrequency)
	if err != nil {
		log.Fatalf("Experiment did not run succesfully: %v", err)
	}
	fmt.Println("experiment run successfully.")
	fmt.Printf("you can store experiment results in bigquery for querying. "+
		"see experiments/cmd/tabulator for more info. example: \n"+
		"bazelisk run //experiments/cmd/tabulator:tabulator -- --experiment_name=%v\n", expName)
}
