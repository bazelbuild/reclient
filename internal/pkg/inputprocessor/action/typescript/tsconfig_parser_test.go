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

func TestTsconfigParser(t *testing.T) {
	test := []struct {
		name string
		path string
		want *Tsconfig
	}{
		{
			name: "tsconfig without extends field",
			path: "internal/pkg/inputprocessor/action/typescript/testdata/test_noextends/tsconfig_no_ext.json",
			want: &Tsconfig{
				Include: []string{"a/", "b/", "c/"},
				Files:   []string{"testfile/a.ts", "testfile/b.ts", "testfile/c.ts"},
				Exclude: []string{"a/a", "a/b"},
				Extends: "",
			},
		},
		{
			name: "tsconfig with extends field",
			path: "internal/pkg/inputprocessor/action/typescript/testdata/test_extends/tsconfig_ext.json",
			want: &Tsconfig{
				Include: []string{"a/", "b/", "c/", "d/"},
				Files:   []string{"testfile/a.ts", "testfile/b.ts", "testfile/c.ts"},
				Exclude: []string{"a/a", "a/b"},
				Extends: "../test_noextends/tsconfig_no_ext.json",
				References: []Reference{
					{
						Path: "../r1",
					},
					{
						Path: "../r2/r3",
					},
				},
			},
		},
	}

	for _, test := range test {
		t.Run(test.name, func(t *testing.T) {
			f, err := bazel.Runfile(test.path)
			if err != nil {
				t.Fatalf("failed to find tsconfig: %v", err)
			}
			got, err := parseTsconfig(f)
			if err != nil {
				t.Fatalf("parseTsconfig() returned error: %v", err)
			}
			test.want.TsPath = f
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("parseTsconfig() returned diff, (-want +got): %s", diff)
			}
		})
	}
}

func TestTsconfigInputs(t *testing.T) {
	test := []struct {
		name string
		path string
		want []string
	}{
		{
			name: "tsconfig include multiple files",
			path: "internal/pkg/inputprocessor/action/typescript/testdata/test_inputs/tsconfig_files.json",
			want: []string{
				"internal/pkg/inputprocessor/action/typescript/testdata/test_inputs/a/b/t2.ts",
				"internal/pkg/inputprocessor/action/typescript/testdata/test_inputs/c/t3.tsx",
			},
		},
		{
			name: "tsconfig include multiple directories",
			path: "internal/pkg/inputprocessor/action/typescript/testdata/test_inputs/tsconfig_include.json",
			want: []string{
				"internal/pkg/inputprocessor/action/typescript/testdata/test_inputs/a/b/t2.ts",
				"internal/pkg/inputprocessor/action/typescript/testdata/test_inputs/a/t1.ts",
				"internal/pkg/inputprocessor/action/typescript/testdata/test_inputs/c/t3.tsx",
			},
		},
	}
	for _, test := range test {
		t.Run(test.name, func(t *testing.T) {
			f, err := bazel.Runfile(test.path)
			if err != nil {
				t.Fatalf("failed to find tsconfig: %v", err)
			}
			tsconfig, err := parseTsconfig(f)
			if err != nil {
				t.Fatalf("parseTsconfig() returned error: %v", err)
			}
			inputs, err := tsconfig.Inputs()
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
				t.Errorf("Inputs() returned diff, (-want, +got): %v", diff)
			}
		})
	}
}
