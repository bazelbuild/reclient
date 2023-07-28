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

// Package main is the main package for the dumpstats binary which produces RE client stats.
//
// This tool currently provides a workaround for dumping stats in an Android build dist_dir, which
// is later added to a database queryable by dremel. It relies on a parsing of reproxy record logs.
package main

import (
	"flag"
	"os"

	"github.com/bazelbuild/reclient/internal/pkg/bootstrap"
	"github.com/bazelbuild/reclient/internal/pkg/rbeflag"
	"github.com/bazelbuild/reclient/internal/pkg/stats"
	"github.com/bazelbuild/reclient/pkg/version"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/moreflag"
	log "github.com/golang/glog"
)

var (
	proxyLogDir     []string
	shutdownProxy   = flag.Bool("shutdown_proxy", false, "Whether to shut down the proxy before reading the log file.")
	shutdownSeconds = flag.Int("shutdown_seconds", 5, "Number of seconds to wait for reproxy to shut down")
	serverAddr      = flag.String("server_address", "127.0.0.1:8000", "The server address in the format of host:port for network, or unix:///file for unix domain sockets.")
	logFormat       = flag.String("log_format", "text", "Format of proxy log. Currently only text is supported.")
	logPath         = flag.String("log_path", "", "DEPRECATED. Use proxy_log_dir instead. If provided, the path to a log file of all executed records. The format is e.g. text://full/file/path.")
	outputDir       = flag.String("output_dir", os.TempDir(), "The location to which stats should be written.")
)

// TODO(b/277909914): remove this binary, it is now superseded by bootstrap --shutdown.
func main() {
	defer log.Flush()
	flag.Var((*moreflag.StringListValue)(&proxyLogDir), "proxy_log_dir", "Comma-separated list of directory paths to aggregate proxy logs from.")
	rbeflag.Parse()
	version.PrintAndExitOnVersionFlag(true)

	if *logPath == "" && len(proxyLogDir) == 0 {
		log.Fatal("Must provide proxy log path.")
	}
	if *outputDir == "" {
		log.Fatal("Must provide an output directory.")
	}

	if *shutdownProxy {
		bootstrap.ShutDownProxy(*serverAddr, *shutdownSeconds)
	}

	if len(proxyLogDir) > 0 {
		log.Infof("Aggregating stats from %v...", proxyLogDir)
		if err := stats.AggregateLogDirsToFiles(*logFormat, proxyLogDir, *outputDir); err != nil {
			log.Fatalf("AggregateLogDirsToFiles failed: %v", err)
		}
		log.Infof("Stats dumped successfully.")
		return
	}
	log.Infof("Aggregating stats from %v...", *logPath)
	if err := stats.AggregateLogToFiles(*logPath, *outputDir); err != nil {
		log.Fatalf("AggregateLogToFiles(%s) failed: %v", *logPath, err)
	}
	log.Infof("Stats dumped successfully.")
}
