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

// Package pathtranslator provides path translation functions.
package pathtranslator

import (
	"os"
	"path/filepath"
	"strings"
)

type mapperType func(string, string, string) string

// ListRelToExecRoot converts a list of paths that are either relative to workingDir or
// absolute, to paths relative to execRoot. workingDir is relative to execRoot.
// A path will be ignored if path is not relative to execRoot.
// Output path is operating system defined file path.
func ListRelToExecRoot(execRoot, workingDir string, paths []string) []string {
	return mapPaths(execRoot, workingDir, paths, RelToExecRoot)
}

// ListRelToWorkingDir converts a list of paths that are either relative to the execroot or
// absolute, to paths relative to the workingDir.
// Output path is operating system defined file path.
func ListRelToWorkingDir(execRoot, workingDir string, paths []string) []string {
	return mapPaths(execRoot, workingDir, paths, RelToWorkingDir)
}

// RelToExecRoot converts a path that is either relative to workingDir or absolute, to a
// path relative to execRoot. workingDir is relative to execRoot.
// It returns empty string if path is not relative to execRoot.
// Output path is operating system defined file path.
func RelToExecRoot(execRoot, workingDir, p string) string {
	if p == "" {
		return ""
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(execRoot, workingDir, p)
	}
	rp, err := filepath.Rel(execRoot, p)
	if err != nil {
		return ""
	}
	if strings.HasPrefix(rp, "..") {
		return ""
	}
	return rp
}

// RelToWorkingDir converts a path that is either relative to the execroot or absolute,
// to a path relative to the workingDir.
func RelToWorkingDir(execRoot, workingDir, p string) string {
	if !filepath.IsAbs(p) {
		p = filepath.Join(execRoot, p)
	}
	rp, err := filepath.Rel(filepath.Join(execRoot, workingDir), p)
	if err != nil {
		return ""
	}
	return rp
}

func mapPaths(execRoot, workingDir string, paths []string, mapper mapperType) []string {
	var res []string
	for _, p := range paths {
		if rel := mapper(execRoot, workingDir, p); rel != "" {
			res = append(res, rel)
		}
	}
	return res
}

// BinaryRelToAbs converts a path that is relative to the current executable
// to an absolute path. If the executable is a symlink then the symlink is
// resolved before generating the path. If the input path is already an absolute
// path, just return it.
func BinaryRelToAbs(relPath string) (string, error) {
	if filepath.IsAbs(relPath) {
		return relPath, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return "", err
	}
	uploader := filepath.Join(filepath.Dir(executable), relPath)
	return uploader, nil
}
