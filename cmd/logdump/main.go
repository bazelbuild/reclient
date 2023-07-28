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

// logdump binary reads reproxy log files and dumps them in a single file that is queryable by gqui.
package main

import (
	"flag"
	"os"
	"path/filepath"

	"github.com/bazelbuild/reclient/internal/pkg/logger"
	"github.com/bazelbuild/reclient/internal/pkg/rbeflag"
	"github.com/bazelbuild/reclient/pkg/version"

	"google.golang.org/protobuf/proto"

	lpb "github.com/bazelbuild/reclient/api/log"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/moreflag"
	log "github.com/golang/glog"
)

var (
	proxyLogDir []string
	logFormat   = flag.String("log_format", "text", "Format of proxy log. Currently only text is supported.")
	logPath     = flag.String("log_path", "", "DEPRECATED. Use proxy_log_dir instead. If provided, the path to a log file of all executed records. The format is e.g. text://full/file/path.")
	outputDir   = flag.String("output_dir", "/tmp/", "The location to which stats should be written.")
)

func main() {
	flag.Var((*moreflag.StringListValue)(&proxyLogDir), "proxy_log_dir", "Comma-separated list of directory paths to aggregate proxy logs from.")
	rbeflag.Parse()
	version.PrintAndExitOnVersionFlag(true)

	if *logPath == "" && len(proxyLogDir) == 0 {
		log.Fatal("Must provide proxy log path.")
	}
	if *outputDir == "" {
		log.Fatal("Must provide an output directory.")
	}
	format, err := logger.ParseFormat(*logFormat)
	if err != nil {
		log.Fatalf("Bad log format: %v", err)
	}

	var recs []*lpb.LogRecord
	if len(proxyLogDir) != 0 {
		recs, _, err = logger.ParseFromLogDirs(format, proxyLogDir)
		if err != nil {
			log.Fatalf("Failed to parse log files: %v", err)
		}
	} else {
		recs, err = logger.ParseFromFile(format, *logPath)
		if err != nil {
			log.Fatalf("Failed to parse log file: %v", err)
		}
	}
	dump := &lpb.LogDump{Records: recs}
	out, err := proto.Marshal(dump)
	if err != nil {
		log.Fatalf("Failed to encode log records: %v", err)
	}
	if err := os.WriteFile(filepath.Join(*outputDir, "reproxy_log.pb"), out, 0644); err != nil {
		log.Fatalf("Failed to write log pb file: %v", err)
	}
	log.Infof("Log dumped successfully.")
}
