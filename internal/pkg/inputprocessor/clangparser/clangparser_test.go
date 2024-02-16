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

package clangparser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bazelbuild/reclient/internal/pkg/execroot"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/args"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/flags"

	"github.com/google/go-cmp/cmp"
)

func TestHandleClangFlags_FlagsWithDeps(t *testing.T) {
	testCases := []string{
		"-B",
		"-fprofile-use=",
		"-fsanitize-blacklist=",
		"-fsanitize-ignorelist=",
		"-fsanitize-coverage-allowlist=",
		"-fprofile-sample-use=",
		"--prefix=",
		"-fprofile-list=",
	}

	s := State{
		objFile:     "x.obj",
		hasCoverage: false,
		hasSplit:    false,
		saveTemps:   "",
	}

	for _, key := range testCases {
		t.Run("FlagsWithDeps"+key, func(t *testing.T) {
			nextRes := &args.FlagResult{
				NormalizedKey: key,
				Values:        []string{"value.file"},
			}
			f := &flags.CommandFlags{}
			err := s.HandleClangFlags(nextRes, f, false)
			if err != nil {
				t.Fatalf("Unexpected error in HandleClangFlags(%v, %v): %v", nextRes, f, err)
			}
			if len(f.Dependencies) != 1 {
				t.Fatalf("Expected 1 dependency, got %d", len(f.Dependencies))
			}
			if f.Dependencies[0] != "value.file" {
				t.Fatalf("Got incorrect dependency, expected 'value.file', got %v", f.Dependencies[0])
			}
		})
	}
}

func TestCollectThinLtoDeps(t *testing.T) {
	testCases := []struct {
		name       string
		workingDir string
		bitcodeDir string
	}{
		{name: "WorkingDirDot", workingDir: ".", bitcodeDir: "."},
		{name: "WorkingDirDistinct", workingDir: "out", bitcodeDir: "lto.foo"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			expectedDeps := []string{
				filepath.Join(tc.bitcodeDir, "foo.o.thinlto.bc"),
				filepath.Join(tc.bitcodeDir, "bar.o"),
				filepath.Join(tc.bitcodeDir, "baz.o"),
			}

			execRoot, cleanup := execroot.Setup(t, nil)
			defer cleanup()

			if err := os.MkdirAll(filepath.Join(execRoot, tc.workingDir, tc.bitcodeDir), 0770); err != nil {
				t.Fatalf("Failed to create dir: %v", err)
			}
			importContent := []byte(strings.Join(expectedDeps[1:], " "))
			if err := os.WriteFile(filepath.Join(execRoot, tc.workingDir, tc.bitcodeDir, "foo.o.imports"), importContent, 0644); err != nil {
				t.Fatalf("Failed to write imports file: %v", err)
			}

			deps, err := collectThinLTODeps(execRoot, tc.workingDir, expectedDeps[0])
			if err != nil {
				t.Errorf("Failed to collect deps: %v", err)
			}

			if diff := cmp.Diff(expectedDeps, deps); diff != "" {
				t.Fatalf("Incorrect thinlto deps: (-want +got)\n%s", diff)
			}
		})
	}
}

// Should ignore non-existing imports file and complete successfully.
func TestCollectThinLtoDeps_NoImportsFile(t *testing.T) {
	bcFile := "foo.o.thinlto.bc"
	deps, err := collectThinLTODeps(".", ".", bcFile)
	if err != nil {
		t.Errorf("Failed to collect deps: %v", err)
	}
	if len(deps) != 1 {
		t.Errorf("Incorrect number of deps, want %d, got %d", 1, len(deps))
	}
}
