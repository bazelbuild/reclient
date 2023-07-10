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

// Package collectlogfiles searches various directories and aggregates
// log files into a single .tar.gz package.
package collectlogfiles

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"team/foundry-x/re-client/internal/pkg/logger"
)

var (
	logFileNames = []string{
		"reproxy.INFO",
		"reproxy.WARNING",
		"reproxy.ERROR",
		"reproxy.FATAL",

		"rewrapper.INFO",
		"rewrapper.WARNING",
		"rewrapper.ERROR",
		"rewrapper.FATAL",

		"bootstrap.INFO",
		"bootstrap.WARNING",
		"bootstrap.ERROR",
		"bootstrap.FATAL",

		"rbe_metrics.txt",
		"rbe_metrics.pb",

		"reproxy_outerr.log",

		"build.trace.gz",
	}
)

// CreateLogsArchive creates a .tar.gz file containing all relevant log files
// relevant to an RBE build.
func CreateLogsArchive(fname string, logDirs []string, logPath string) (err error) {
	var logFiles []string
	for _, logDir := range logDirs {
		logFiles = append(logFiles, collectLogFilesFromDir(logDir)...)
	}
	if logPath != "" {
		_, fp, err := logger.ParseFilepath(logPath)
		if err != nil {
			return err
		}
		if _, err := os.Stat(fp); err == nil {
			logFiles = append(logFiles, fp)
		}
	}

	if len(logFiles) < 1 {
		return fmt.Errorf("unable to find log files. Try again by specifying proxy_log_dir")
	}

	logsArchive, err := os.Create(fname)
	if err != nil {
		return err
	}
	defer func() {
		cerr := logsArchive.Close()
		if err == nil {
			err = cerr
		}
	}()
	zw := gzip.NewWriter(logsArchive)
	defer func() {
		cerr := zw.Close()
		if err == nil {
			err = cerr
		}
	}()
	tw := tar.NewWriter(zw)
	defer func() {
		cerr := tw.Close()
		if err == nil {
			err = cerr
		}
	}()

	for _, f := range logFiles {
		if err := addFileToArchive(tw, f); err != nil {
			return err
		}
	}
	return nil
}

func collectLogFilesFromDir(logDir string) []string {
	var logFiles []string
	for _, logFile := range logFileNames {
		fp := filepath.Join(logDir, logFile)
		if _, err := os.Stat(fp); err == nil {
			logFiles = append(logFiles, fp)
		}
	}
	return logFiles
}

func addFileToArchive(tw *tar.Writer, path string) error {
	of, err := os.Open(path)
	if err != nil {
		return err
	}
	defer of.Close()

	stat, err := of.Stat()
	if err != nil {
		return err
	}
	fh := new(tar.Header)
	fh.Name = path
	fh.Size = stat.Size()
	fh.Mode = int64(stat.Mode())
	fh.ModTime = stat.ModTime()
	if err := tw.WriteHeader(fh); err != nil {
		return err
	}
	if _, err := io.Copy(tw, of); err != nil {
		return err
	}
	return nil
}
