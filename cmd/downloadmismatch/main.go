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

// The tool to download compare build mismatches given the rbe stats file.
// After a compare build that contains mismatches, we can run the tool like this:
// ```
// downloadmismatch --diff --proxy_log_dir=/tmp --mismatch_output_dir=/tmp
// ```
// downloadmismatch binary will download the mismatch outputs according to
// /tmp/rbe_metrics.pb, extract the readable strings, and diff the strings. The
// outputs will be dumped to /tmp(the `mismatch_output_dir`) structured as follow:
// /tmp
//     |reclient_mismatches
//		|#ActionHash
//			|output_file_path_of_one_mismatch
//				|local
//					|#LocalOutputRetry1
//					|#LocalOutputRetry1.strings
//				|remote
//					|#RemoteOutputRetry1
//					|#RemoteOutputRetry1.strings
//					|#RemoteOutputRetry2
//					|#RemoteOutputRetry2.strings
//				|compare_action.diff
//		...

package main

import (
	"context"
	"flag"
	"log"
	"path/filepath"

	"github.com/bazelbuild/reclient/internal/pkg/downloadmismatch"
	"github.com/bazelbuild/reclient/internal/pkg/rbeflag"

	rflags "github.com/bazelbuild/remote-apis-sdks/go/pkg/flags"
)

const (
	defaultLogDir = "/tmp"
)

var (
	proxyLogDir = flag.String("proxy_log_dir", defaultLogDir, "The directory that stores the reproxy mismatch stats file(such as rbe_metrics.pb), the default is /tmp")
	outputDir   = flag.String("mismatch_output_dir", defaultLogDir, "The directory to store the downloaded mismatch outputs, the default is /tmp")
	doDiff      = flag.Bool("diff", true, "extract readable string content from local/remote output and generate #mismatch_output_dir/#actionHash/compare_build.diff")
)

func main() {
	rbeflag.Parse()
	rbeflag.LogAllFlags(0)

	grpcClient, err := rflags.NewClientFromFlags(context.Background())
	if err != nil {
		log.Fatalf("error connecting to remote execution client: %v", err)
	}
	if err := downloadmismatch.DownloadMismatches(*proxyLogDir, *outputDir, grpcClient); err != nil {
		log.Fatalf("DownloadMismatches encountered fatal error: %v", err)
	}
	if *doDiff {
		err := downloadmismatch.DiffOutputDir(filepath.Join(*outputDir, downloadmismatch.DownloadDir))
		if err != nil {
			log.Fatal(err)
		}
	}
}
