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

package pathtranslator

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// toAbs makes path to absolute path on any platform.
// "/foo/bar" is not absolute path on windows (missing "C:" etc)
func toAbs(t *testing.T, path string) string {
	t.Helper()
	absPath, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("Unable to get absolute path of %q: %v", path, err)
	}
	return absPath
}

func TestRelToExecRoot(t *testing.T) {
	er := toAbs(t, "/foo/bar")
	tests := []struct {
		name       string
		path       string
		workingDir string
		want       string
	}{{
		name: "under exec root",
		path: "baz",
		want: "baz",
	}, {
		name:       "under working directory",
		path:       "baz",
		workingDir: "wd",
		want:       filepath.Clean("wd/baz"),
	}, {
		name: "absolute under execroot",
		path: toAbs(t, "/foo/bar/baz"),
		want: "baz",
	}, {
		name: "absolute outside of execroot",
		path: toAbs(t, "/bar/foo/baz"),
		want: "",
	}, {
		name: "empty",
		path: "",
		want: "",
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := RelToExecRoot(er, test.workingDir, test.path)
			if got != test.want {
				t.Errorf("RelToExecRoot(%q,%q,%q) = %q, want %q", er, test.workingDir, test.path, got, test.want)
			}
		})
	}
}

func TestRelToWorkingDir(t *testing.T) {
	er := toAbs(t, "/foo/bar")
	tests := []struct {
		name       string
		path       string
		workingDir string
		want       string
	}{{
		name:       "under exec root",
		path:       "baz",
		workingDir: "wd",
		want:       filepath.Join("..", "baz"),
	}, {
		name:       "under working directory",
		path:       "wd/baz",
		workingDir: "wd",
		want:       "baz",
	}, {
		name:       "absolute under execroot",
		path:       toAbs(t, "/foo/bar/baz"),
		workingDir: "wd",
		want:       filepath.Join("..", "baz"),
	}, {
		name:       "empty",
		workingDir: "wd",
		want:       filepath.Clean(".."),
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := RelToWorkingDir(er, test.workingDir, test.path)
			if got != test.want {
				t.Errorf("RelToExecRoot(%q,%q,%q) = %q, want %q", er, test.workingDir, test.path, got, test.want)
			}
		})
	}
}

func TestListRelToExecRoot(t *testing.T) {
	er := toAbs(t, "/foo/bar")
	tests := []struct {
		name       string
		paths      []string
		workingDir string
		want       []string
	}{{
		name:  "under exec + absolute",
		paths: []string{"baz", toAbs(t, "/foo/bar/baz2")},
		want:  []string{"baz", "baz2"},
	}, {
		name:       "under working directory + absolute",
		paths:      []string{"baz", toAbs(t, "/foo/bar/wd/baz2")},
		workingDir: "wd",
		want:       []string{filepath.Clean("wd/baz"), filepath.Clean("wd/baz2")},
	}, {
		name:  "some absolute outside of execroot",
		paths: []string{"baz", toAbs(t, "/bar/foo/baz")},
		want:  []string{"baz"},
	}, {
		name:  "empty",
		paths: []string{},
		want:  nil,
	}, {
		name:  "nil",
		paths: nil,
		want:  nil,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ListRelToExecRoot(er, test.workingDir, test.paths)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("ListRelToExecRoot(%q,%q,%v) returned diff (-want +got): %v", er, test.workingDir, test.paths, diff)
			}
		})
	}
}

func TestAbsPathRelativeToExecutable(t *testing.T) {
	execPath, err := os.Executable()
	if err != nil {
		t.Fatalf("Error calling os.Executable(): %v", err)
	}
	execPathNoSym, err := filepath.EvalSymlinks(execPath)
	if err != nil {
		t.Fatalf("Error calling filepath.EvalSymlinks(%v): %v", execPath, err)
	}
	execPathAbs, err := filepath.Abs(execPathNoSym)
	if err != nil {
		t.Fatalf("Error calling filepath.Abs(%v): %v", execPathNoSym, err)
	}
	absPath, err := BinaryRelToAbs("somepath/to/a/file")
	if err != nil {
		t.Errorf("Error calling BinaryRelToAbs(\"somepath/to/a/file\"): %v", err)
	}
	if !strings.HasPrefix(absPath, filepath.Dir(execPathAbs)) {
		t.Errorf("BinaryRelToAbs(\"somepath/to/a/file\") returned a path %v that is not relative to executable %v", absPath, execPathAbs)
	}
}

func copyBinary(t *testing.T, from, to string) {
	t.Helper()
	fromFile, err := os.Open(from)
	if err != nil {
		t.Fatalf("Unable to open source: %v", err)
	}
	defer fromFile.Close()
	toFile, err := os.Create(to)
	if err != nil {
		t.Fatalf("Unable to open destination: %v", err)
	}
	defer toFile.Close()
	_, err = io.Copy(toFile, fromFile)
	if err != nil {
		t.Fatalf("Unable to copy file: %v", err)
	}
	err = toFile.Sync()
	if err != nil {
		t.Fatalf("Unable to copy file: %v", err)
	}
	_, err = os.Stat(from)
	if err != nil {
		t.Fatalf("Unable to get file permission from source: %v", err)
	}
	err = os.Chmod(to, 0777)
	if err != nil {
		t.Fatalf("Unable to apply file permissions to destination: %v", err)
	}
}
