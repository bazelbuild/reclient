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

// Package gcs contains logic relevant to GCS storage.
//
// TODO(b/170271950): use gcs API instead.
package gcs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	log "github.com/golang/glog"
)

// Copy copies a file from/to GCS.
func Copy(ctx context.Context, src, dest string) error {
	log.Infof("Copying %v", src)
	args := []string{"gsutil", "cp", src, dest}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdin = os.Stdin // Connect stdin to allow gcloud to use gsutil reauth flow
	if oe, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to copy files: %v, outerr: %v", err, string(oe))
	}
	log.Infof("Copied %v to %v", src, dest)
	return nil
}

// List lists the contents of a directory in GCS.
func List(ctx context.Context, dir string) ([]string, error) {
	args := []string{"gsutil", "ls", dir}
	oe, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to list dir: %v, outerr: %v", err, string(oe))
	}
	return deleteEmpty(strings.Split(string(oe), "\n")), nil
}

func deleteEmpty(s []string) []string {
	var r []string
	for _, str := range s {
		if str != "" {
			r = append(r, str)
		}
	}
	return r
}
