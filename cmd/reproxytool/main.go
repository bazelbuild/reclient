// Copyright 2023 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Binary reproxytool parses reclient log files for common debugging purpose.
//
// To see help information:
//
// bazelisk run //cmd/reproxytool:reproxytool -- --help
//
// Example Invocation:
//
// Convert reproxy.INFO log to usage CSV:
// bazelisk run //cmd/reproxytool:reproxytool -- \
// --operation=usage_to_csv --log_path=/tmp/reproxy.INFO \
// --alsologtostderr
package main

import (
	"flag"
	"fmt"
	"os"
	"path"

	csv "github.com/bazelbuild/reclient/cmd/reproxytool/usage2csv"
	log "github.com/golang/glog"
)

// OpType denotes the type of operation to perform.
type OpType string

const (
	usage2CSV OpType = "usage_to_csv"
)

var supportedOps = []OpType{
	usage2CSV,
}

var (
	operation = flag.String("operation", "", fmt.Sprintf("Specifies the operation to perform. Supported values: %v", supportedOps))
	logPath   = flag.String("log_path", "", "Path to log file. E.g., /tmp/reproxy.INFO")
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %v [-flags] -- --operation <op> arguments ...\n", path.Base(os.Args[0]))
		flag.PrintDefaults()
	}
	flag.Parse()
	if *operation == "" {
		log.Exitf("--operation must be specified.")
	}
	switch OpType(*operation) {
	case usage2CSV:
		if err := csv.Usage2CSV(getLogPathFlag()); err != nil {
			log.Exitf("Error parsing usage data from reproxy.INFO, %v,to CSV file %v", logPath, err)
		}
	default:
		log.Exitf("Unsupported operation %v. Supported operations:\n%v", *operation, supportedOps)
	}
}

func getLogPathFlag() string {
	if *logPath == "" {
		log.Exitf("--log_path must be specified.")
	}
	return *logPath
}
