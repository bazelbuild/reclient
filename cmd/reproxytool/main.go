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
// Binary reproxytool has a variety of functionality to usefully parse both
// reproxy.INFO and reproxy.rrpl files.
//
// To see help information:
//
// bazelisk run //cmd/reproxytool:reproxytool -- --help
//
// Example Invocation:
//
// Convert reproxy.INFO log to usage CSV:
//
//	bazelisk run //cmd/reproxytool:reproxytool -- \
//	  --operation=usage_to_csv --log_path=/tmp/reproxy.INFO \
//	  --alsologtostderr
//
// Example Invocation to show an action:
//
//	bazelisk run //cmd/reproxytool:reproxytool -- \
//	  --operation show_action --instance=<instance> \
//	  --service <service> --alsologtostderr --v 1 \
//	  --use_application_default_credentials=true --digest <digest>
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path"

	csv "github.com/bazelbuild/reclient/cmd/reproxytool/usage2csv"
	rflags "github.com/bazelbuild/remote-apis-sdks/go/pkg/flags"
	remotetool "github.com/bazelbuild/remote-apis-sdks/go/pkg/tool"
	log "github.com/golang/glog"
)

const (
	usage2CSV remotetool.OpType = "usage_to_csv"
)

var (
	supportedOps = append(remotetool.SupportedOps, usage2CSV)
)

var (
	operation = flag.String("operation", "", fmt.Sprintf("Specifies the operation to perform. Supported values: %v", supportedOps))
	logPath   = flag.String("log_path", "", "Path to log file. E.g., /tmp/reproxy.INFO")
)

func addOps() {
	remotetool.SupportedOps = supportedOps
	remotetool.RemoteToolOperations[usage2CSV] = func(ctx context.Context, c *remotetool.Client) {
		if err := csv.Usage2CSV(getLogPathFlag()); err != nil {
			log.Exitf("Error parsing usage data from reproxy.INFO, %v,to CSV file %v", logPath, err)
		}
	}
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %v [-flags] -- --operation <op> arguments ...\n", path.Base(os.Args[0]))
		flag.PrintDefaults()
	}
	remotetool.RegisterFlags()
	addOps()
	flag.Parse()
	if *operation == "" {
		log.Exitf("--operation must be specified.")
	}

	ctx := context.Background()
	grpcClient, err := rflags.NewClientFromFlags(ctx)
	if err != nil {
		log.Exitf("error connecting to remote execution client: %v", err)
	}
	defer grpcClient.Close()
	c := &remotetool.Client{GrpcClient: grpcClient}

	fn, ok := remotetool.RemoteToolOperations[remotetool.OpType(*operation)]
	if !ok {
		log.Exitf("unsupported operation %v. Supported operations:\n%v", *operation, remotetool.SupportedOps)
	}
	fn(ctx, c)
}

func getLogPathFlag() string {
	if *logPath == "" {
		log.Exitf("--log_path must be specified.")
	}
	return *logPath
}
