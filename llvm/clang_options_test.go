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

package clangoptions_test

import (
	"testing"
)

func TestClangOptionsFileContents(t *testing.T) {
	var (
		wantFile, gotFile         string
		wantFilePath, gotFilePath string
	)
	// Iterating through the map of filename:filecontents to get the file content
	// to avoid depending on the filename.
	for k, v := range wantClangOptions {
		wantFilePath = k
		wantFile = string(v)
		break
	}
	for k, v := range gotClangOptions {
		gotFilePath = k
		gotFile = string(v)
		break
	}

	if wantFile != gotFile {
		t.Fatalf("Clang options file %q does not match with generated file (%q) from current clang-scan-deps revision. Refer to upgrading_clang_scan_deps playbook to properly update the file.", gotFilePath, wantFilePath)
	}
}
