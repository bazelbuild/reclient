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

// Package execroot provides execroot for test.
package execroot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Setup creates a temporary exec root for testing and touches files with given paths under the exec root.
func Setup(t *testing.T, files []string) (string, func()) {
	t.Helper()
	execRoot, err := os.MkdirTemp("", strings.ReplaceAll(t.Name(), "/", "_"))
	if err != nil {
		t.Fatalf("Failed to make temp dir: %v", err)
	}
	AddFiles(t, execRoot, files)
	execRoot, err = filepath.EvalSymlinks(execRoot)
	if err != nil {
		t.Fatalf("Failed to resolve symlink %q: %v", execRoot, err)
	}
	return execRoot, func() { os.RemoveAll(execRoot) }
}

// AddFiles touches the given files under the given exec root.
func AddFiles(t *testing.T, execRoot string, files []string) {
	t.Helper()
	for _, f := range files {
		if err := os.MkdirAll(filepath.Join(execRoot, filepath.Dir(f)), os.ModePerm); err != nil {
			t.Fatalf("Failed to setup working directory %v under temp exec root %v: %v", filepath.Dir(f), execRoot, err)
		}
		err := os.WriteFile(filepath.Join(execRoot, f), nil, 0755)
		if err != nil {
			t.Fatalf("Failed to setup file %v under temp exec root %v: %v", f, execRoot, err)
		}
	}
}

// AddDirs touches the given dirs under the given exec root.
func AddDirs(t *testing.T, execRoot string, dirs []string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(execRoot, d), os.ModePerm); err != nil {
			t.Fatalf("Failed to setup working directory %v under temp exec root %v: %v", d, execRoot, err)
		}
	}
}

// AddFileWithContent creates the given file under the given exec root with its respective content.
func AddFileWithContent(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
		t.Fatalf("Failed to setup working directory %v: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, content, os.ModePerm); err != nil {
		t.Fatalf("Failed to setup file %v: %v", path, err)
	}
}

// AddFilesWithContent creates the given files under the given exec root with their respective content.
func AddFilesWithContent(t *testing.T, execRoot string, files map[string][]byte) {
	t.Helper()
	for f, c := range files {
		AddFileWithContent(t, filepath.Join(execRoot, f), c)
	}
}
