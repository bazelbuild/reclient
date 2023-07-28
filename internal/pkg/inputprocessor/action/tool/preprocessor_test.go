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

package tool

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/bazelbuild/reclient/internal/pkg/execroot"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var (
	strSliceCmp = cmpopts.SortSlices(func(a, b string) bool { return a < b })
)

func TestToolPreprocessor(t *testing.T) {
	er, cleanup := execroot.Setup(t, nil)
	defer cleanup()
	tests := []struct {
		name          string
		options       inputprocessor.Options
		existingFiles map[string][]byte
		wantSpec      *inputprocessor.ActionSpec
	}{{
		name: "basic",
		options: inputprocessor.Options{
			WorkingDir: "wd",
			Cmd:        []string{filepath.Join("..", "unzip"), "foo.zip", "-o", "foo"},
		},
		existingFiles: map[string][]byte{"unzip": nil, filepath.Join("wd", "foo.zip"): nil},
		wantSpec: &inputprocessor.ActionSpec{
			InputSpec: &command.InputSpec{
				Inputs: []string{"unzip"},
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			execroot.AddFilesWithContent(t, er, test.existingFiles)
			test.options.ExecRoot = er
			pp := &Preprocessor{
				&inputprocessor.BasePreprocessor{Ctx: ctx},
			}
			gotSpec, err := inputprocessor.Compute(pp, test.options)
			if err != nil {
				t.Errorf("Compute() returned error: %v", err)
			}

			if diff := cmp.Diff(test.wantSpec, gotSpec, strSliceCmp); diff != "" {
				t.Errorf("Compute() returned diff in ActionSpec, (-want +got): %s", diff)
			}
		})
	}
}
