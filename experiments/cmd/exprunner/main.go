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

// Package main is the main package for the experiment exprunner binary.
//
// Use the exprunner tool to run experiments defined in an experiment textproto
// file, tabulate them into a bigquery table, and upload results to perfgate.
// Example:
// bazelisk run //experiments/cmd/exprunner:exprunner -- --exp_def_path=$PWD/experiments/examples/chrome-build.textproto --perfgate_path="reclient_perfgate" --benchmark_path=$PWD/tests/performance/benchmarks/chrome-perfgate.config
//
// To view logging info as the experiment is running, run with --alsologtostderr.
package main

import (
	"flag"
	"os"
	"path/filepath"
	"text/template"

	"github.com/bazelbuild/reclient/experiments/internal/pkg/perfgate"
	"github.com/bazelbuild/reclient/experiments/internal/pkg/runner"
	"github.com/bazelbuild/reclient/experiments/internal/pkg/tabulator"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/moreflag"

	log "github.com/golang/glog"
)

var (
	gcpProject    = flag.String("gcp_project", "foundry-x-experiments", "Name of the GCP project to run experiments on. Defaults to foundry-x-experiments.")
	resBucket     = flag.String("results_bucket", "foundry-x-experiments-results", "Name of the GCS bucket to store results. Defaults to foundry-x-experiments-results.")
	expDefPath    = flag.String("exp_def_path", "", "Path to the .textproto file containing the experiment definition. If none specified, does not run. If exp_def_vars is provided then this file will be parsed as a text/template.")
	dateSuffix    = flag.String("date_suffix", "", "Date string suffix to use instead of checking the current time. If none specified, the current time will be used.")
	perfgatePath  = flag.String("perfgate_path", "", "Path to the perfgate wrapper binary. If none specified, results will not be uploaded to perfgate.")
	benchmarkFile = flag.String("benchmark_path", "", "Path to the BenchmarkInfo file containing metric definitions.")
	logFrequency  = flag.Int("log_frequency", -1, "Time between polling the log file for updates, in seconds. Default 5 for virtual machine experiments, 0 (disabled) for workstation experiments")
	expDefVars    map[string]string
)

func main() {
	flag.Var((*moreflag.StringMapValue)(&expDefVars), "exp_def_vars", "Comma-separated key value pairs in the form key=value to be used in exp_def_path file in the form {{.key}}.")

	flag.Parse()
	if *expDefPath == "" {
		log.Fatal("exp_def_path cannot be empty.")
	}
	if len(expDefVars) > 0 {
		filledTemplate := generateExpDefFromTemplate(*expDefPath, expDefVars)
		defer os.Remove(filledTemplate)
		*expDefPath = filledTemplate
	}
	log.Infof("Running experiment: %v", *expDefPath)
	expName, err := runner.RunExperiment(*expDefPath, *dateSuffix, *gcpProject, *resBucket, *logFrequency)
	if err != nil {
		log.Fatalf("Error running experiment: %v", err)
	}
	log.Infof("Running tabulator on experiment: %v", expName)
	if err := tabulator.RunExperimentResultCollector(*resBucket, expName, *gcpProject); err != nil {
		log.Fatalf("Error running tabulator: %v", err)
	}
	log.Infof("Done tabulating results for %v", expName)
	if *perfgatePath != "" {
		log.Infof("Uploading results to perfgate using: %v", *perfgatePath)
		if err := perfgate.RunUploader(*expDefPath, *resBucket, expName, *gcpProject, *perfgatePath, *benchmarkFile); err != nil {
			log.Fatalf("Error running perfgate: %v", err)
		}
	}
}

func generateExpDefFromTemplate(tmplPath string, tmplMap map[string]string) string {
	log.Infof("Populating template")
	f, err := os.CreateTemp("", filepath.Base(tmplPath))
	defer f.Close()
	if err != nil {
		log.Fatalf("Error creating tmp file: %w", err)
	}
	tmpl, err := template.ParseFiles(tmplPath)
	if err != nil {
		log.Fatalf("Error parsing template file: %w", err)
	}
	err = tmpl.Execute(f, tmplMap)
	if err != nil {
		log.Fatalf("Error generating experiment definition file from template: %w")
	}
	return f.Name()
}
