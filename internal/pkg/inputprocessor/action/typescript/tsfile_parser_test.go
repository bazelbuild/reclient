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

package typescript

import (
	"testing"

	"github.com/bazelbuild/rules_go/go/tools/bazel"
	"github.com/google/go-cmp/cmp"
)

func TestTsfileParser(t *testing.T) {
	test := []struct {
		name string
		path string
		want *Tsfile
	}{
		{
			name: "single tsfile multi imports",
			path: "internal/pkg/inputprocessor/action/typescript/testdata/test_singlefile/test.ts",
			want: &Tsfile{
				Imports: []string{"./a", "./b", "./c", "./aa/bb/c", "b/cc/a", "c/cc", "../dd/ddd"},
			},
		},
	}

	for _, test := range test {
		t.Run(test.name, func(t *testing.T) {
			f, err := bazel.Runfile(test.path)
			if err != nil {
				t.Fatalf("failed to find tsfile: %v", err)
			}
			got, err := parseTsfile(f)
			if err != nil {
				t.Fatalf("failed to parse tsfile: %v", err)
			}
			test.want.TsPath = f
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("parse() returned diff, (-want +got): %s", diff)
			}
		})
	}
}

func TestTsfileInputs(t *testing.T) {
	test := []struct {
		name string
		path string
		want []string
	}{
		{
			name: "tsfile with multiple imports",
			path: "internal/pkg/inputprocessor/action/typescript/testdata/test_inputs/a/t1.ts",
			want: []string{
				"internal/pkg/inputprocessor/action/typescript/testdata/test_inputs/a/b/t2.ts",
				"internal/pkg/inputprocessor/action/typescript/testdata/test_inputs/c/t3.tsx",
			},
		},
	}
	for _, test := range test {
		t.Run(test.name, func(t *testing.T) {
			f, err := bazel.Runfile(test.path)
			if err != nil {
				t.Fatalf("failed to find tsfile: %v", err)
			}
			tsfile, err := parseTsfile(f)
			if err != nil {
				t.Fatalf("failed to parse tsfile: %v", err)
			}
			inputs, err := tsfile.Inputs()
			if err != nil {
				t.Fatalf("Inputs() returned error: %v", err)
			}
			gotPath := []string{}
			for _, inp := range inputs {
				gotPath = append(gotPath, inp.Path())
			}
			for i := range test.want {
				path, err := bazel.Runfile(test.want[i])
				if err != nil {
					t.Fatalf("failed to find tsfile: %v", err)
				}
				test.want[i] = path
			}
			if diff := cmp.Diff(test.want, gotPath); diff != "" {
				t.Errorf("Inputs() returned diff, (-want, +got): %s", diff)
			}
		})
	}
}
