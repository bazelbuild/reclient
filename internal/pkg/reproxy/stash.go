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

package reproxy

import (
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// StashFiles copies the given files to a temporary location. It returns a function that
// restores the original files.
func StashFiles(paths []string) func() {
	parentDir := os.TempDir()
	newFiles := make(map[string]string)
	for _, srcPath := range paths {
		source, err := os.Open(srcPath)
		if err != nil {
			continue
		}
		defer source.Close()
		destPath := filepath.Join(parentDir, uuid.New().String())
		destination, err := os.Create(destPath)
		if err != nil {
			continue
		}
		defer destination.Close()
		_, err = io.Copy(destination, source)
		if err != nil {
			continue
		}
		err = destination.Sync()
		if err != nil {
			continue
		}
		si, err := os.Stat(srcPath)
		if err != nil {
			continue
		}
		err = os.Chmod(destPath, si.Mode())
		if err != nil {
			continue
		}
		newFiles[destPath] = srcPath
	}
	return func() {
		for dup, orig := range newFiles {
			os.Rename(dup, orig)
		}
	}
}
