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

package inputprocessor

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/bazelbuild/reclient/internal/pkg/execroot"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var (
	strSliceCmp = cmpopts.SortSlices(func(a, b string) bool { return a < b })
)

func TestBasePreprocessor(t *testing.T) {
	er, cleanup := execroot.Setup(t, nil)
	defer cleanup()
	tests := []struct {
		name          string
		options       Options
		existingFiles map[string][]byte
		wantSpec      *ActionSpec
	}{{
		name: "basic",
		options: Options{
			WorkingDir: "wd",
			Cmd:        []string{filepath.Join("..", "unzip_basic"), "foo.zip", "-o", "foo"},
		},
		existingFiles: map[string][]byte{"unzip_basic": nil, filepath.Join("wd", "foo.zip"): nil},
		wantSpec: &ActionSpec{
			InputSpec: &command.InputSpec{
				Inputs: []string{"unzip_basic"},
			},
		},
	}, {
		name: "with remote toolchain inputs",
		options: Options{
			WorkingDir: "wd",
			Cmd:        []string{filepath.Join("..", "unzip_wrti"), "foo.zip", "-o", "foo"},
		},
		existingFiles: map[string][]byte{
			"unzip_wrti":                         nil,
			"unzip_wrti_remote_toolchain_inputs": []byte("lib"),
			"lib":                                nil,
			filepath.Join("wd", "foo.zip"):       nil,
		},
		wantSpec: &ActionSpec{
			InputSpec: &command.InputSpec{
				Inputs: []string{"unzip_wrti", "lib"},
			},
		},
	}, {
		name: "environment variable rewrite",
		options: Options{
			WorkingDir: "wd",
			Cmd:        []string{filepath.Join("..", "unzip_evr"), "foo.zip", "-o", "foo"},
			Inputs: &command.InputSpec{
				EnvironmentVariables: map[string]string{"FOO": filepath.Join(er, "lib")},
			},
		},
		existingFiles: map[string][]byte{
			"unzip_evr":                    nil,
			filepath.Join("wd", "foo.zip"): nil,
		},
		wantSpec: &ActionSpec{
			InputSpec: &command.InputSpec{
				Inputs:               []string{"unzip_evr"},
				EnvironmentVariables: map[string]string{"FOO": filepath.Join("..", "lib")},
			},
		},
	}, {
		name: "filter under exec root",
		options: Options{
			WorkingDir: "wd",
			Cmd:        []string{filepath.Join("..", "unzip_fuer"), "foo.zip", "-o", "foo"},
			Inputs: &command.InputSpec{
				Inputs: []string{"badfile"},
			},
		},
		existingFiles: map[string][]byte{
			"unzip_fuer":                   nil,
			filepath.Join("wd", "foo.zip"): nil,
		},
		wantSpec: &ActionSpec{
			InputSpec: &command.InputSpec{
				Inputs: []string{"unzip_fuer"},
			},
		},
	}, {
		name: "with remote toolchain inputs having missing files",
		options: Options{
			WorkingDir:      "wd",
			Cmd:             []string{filepath.Join("..", "unzip_wrtihmf"), "foo.zip", "-o", "foo"},
			ShallowFallback: true,
		},
		existingFiles: map[string][]byte{
			"unzip_wrtihmf":                         nil,
			"unzip_wrtihmf_remote_toolchain_inputs": []byte("bar"),
			"lib":                                   nil,
			filepath.Join("wd", "foo.zip"):          nil,
		},
		wantSpec: &ActionSpec{
			InputSpec: &command.InputSpec{
				Inputs: []string{"unzip_wrtihmf"},
			},
			UsedShallowMode: false,
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			execroot.AddFilesWithContent(t, er, test.existingFiles)
			test.options.ExecRoot = er
			bp := &BasePreprocessor{Ctx: ctx, Options: test.options}
			gotSpec, err := Compute(bp, test.options)
			if err != nil {
				t.Errorf("Compute() returned error: %v", err)
			}

			if diff := cmp.Diff(test.wantSpec, gotSpec, strSliceCmp); diff != "" {
				t.Errorf("Compute() returned diff in ActionSpec, (-want +got): %s", diff)
			}
		})
	}
}
