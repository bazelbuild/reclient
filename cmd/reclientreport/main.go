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

// Binary reclientreport is used to package up re-client related
// log files into a .tar.gz file so that the user can upload it
// when filing bug reports.
//
//	$ RBE_log_dir=<path-to-build-output-dir> reclientreport
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"team/foundry-x/re-client/internal/pkg/collectlogfiles"
	"team/foundry-x/re-client/internal/pkg/rbeflag"
	"team/foundry-x/re-client/internal/pkg/reproxypid"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/moreflag"
	log "github.com/golang/glog"
)

var (
	proxyLogDir     []string
	serverAddr      = flag.String("server_address", "", "The server address of reproxy in the format of host:port for network, or unix:///file for unix domain sockets. If provided reclientreport will wait for this reproxy instance to exit before collecting logs")
	shutdownTimeout = flag.Duration("shutdown_timeout", 60*time.Second, "Number of seconds to wait for reproxy to shutdown")
	outputDir       = flag.String("output_dir", os.TempDir(), "The location to which stats are written.")
	logPath         = flag.String("log_path", "", "DEPRECATED. Use proxy_log_dir instead. If provided, the path to a log file of all executed records. The format is e.g. text://full/file/path.")
)

func main() {
	flag.Var((*moreflag.StringListValue)(&proxyLogDir), "proxy_log_dir", "If provided, the directory path to a proxy log file of executed records.")
	rbeflag.Parse()

	if *serverAddr != "" {
		if err := waitForReproxy(); err != nil {
			log.Errorf("Error while waiting for reproxy to exit: %v", err)
		}
	}

	fmt.Println("Collecting log files...")
	logFile, err := os.CreateTemp("", "reclient-log-*.tar.gz")
	if err != nil {
		log.Exitf("Failed to create temp log file: %v", err)
	}
	logFileName := logFile.Name()
	logFile.Close()
	logDirs := append(proxyLogDir, getLogDir())
	logDirs = append(logDirs, *outputDir)
	logDirs = append(logDirs, os.TempDir())
	if err := collectlogfiles.CreateLogsArchive(logFileName, uniqueDirs(logDirs), *logPath); err != nil {
		os.Remove(logFileName)
		log.Exitf("Failed to collect re-client logs: %v", err)
	}
	fmt.Printf("Created log file at %v. Please attach this to your bug report!\n", logFileName)
}

func waitForReproxy() error {
	ctx, cancel := context.WithTimeout(context.Background(), *shutdownTimeout)
	defer cancel()
	pf, err := reproxypid.ReadFile(*serverAddr)
	if err != nil {
		return err
	}
	deadCh := make(chan error)
	defer close(deadCh)
	go pf.PollForDeath(ctx, 50*time.Millisecond, deadCh)
	select {
	case err := <-deadCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// getLogDir retrieves the glog log directory
func getLogDir() string {
	if f := flag.Lookup("log_dir"); f != nil {
		return f.Value.String()
	}
	return ""
}

func uniqueDirs(dirs []string) []string {
	m := make(map[string]bool, len(dirs))
	out := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if m[dir] || dir == "" {
			continue
		}
		out = append(out, dir)
		m[dir] = true
	}
	return out
}
