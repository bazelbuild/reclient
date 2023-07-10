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
	"sort"
	"testing"

	"github.com/bazelbuild/rules_go/go/tools/bazel"
	"github.com/google/go-cmp/cmp"
)

func TestTsInputs(t *testing.T) {
	test := []struct {
		name string
		path string
		want []string
	}{
		{
			name: "tsconfig with transitive imports",
			path: "internal/pkg/inputprocessor/action/typescript/testdata/test_transitive/tsconfig.json",
			want: []string{
				"internal/pkg/inputprocessor/action/typescript/testdata/test_transitive/tsconfig.json",
				"internal/pkg/inputprocessor/action/typescript/testdata/test_transitive/a/t1.ts",
				"internal/pkg/inputprocessor/action/typescript/testdata/test_transitive/a/t2.ts",
				"internal/pkg/inputprocessor/action/typescript/testdata/test_transitive/a/t3.tsx",
				"internal/pkg/inputprocessor/action/typescript/testdata/test_transitive/c/t1.ts",
				"internal/pkg/inputprocessor/action/typescript/testdata/test_transitive/c/t2.ts",
				"internal/pkg/inputprocessor/action/typescript/testdata/test_transitive/c/t3.ts",
				"internal/pkg/inputprocessor/action/typescript/testdata/test_transitive/b/bb/t1.ts",
				"internal/pkg/inputprocessor/action/typescript/testdata/test_inputs/c/t3.tsx",
				"internal/pkg/inputprocessor/action/typescript/testdata/test_inputs/a/t1.ts",
				"internal/pkg/inputprocessor/action/typescript/testdata/test_inputs/a/b/t2.ts",
				"internal/pkg/inputprocessor/action/typescript/testdata/test_inputs/tsconfig_include.json",
				"internal/pkg/inputprocessor/action/typescript/testdata/test_transitive/d/t1.tsx",
				"internal/pkg/inputprocessor/action/typescript/testdata/test_transitive/d/t2.ts",
				"internal/pkg/inputprocessor/action/typescript/testdata/test_transitive/d/t3.ts",
				"internal/pkg/inputprocessor/action/typescript/testdata/test_transitive/e/t1.ts",
				"internal/pkg/inputprocessor/action/typescript/testdata/test_transitive/e/t2.ts",
				"internal/pkg/inputprocessor/action/typescript/testdata/test_transitive/e/t3.d.ts",
			},
		},
	}
	for _, test := range test {
		t.Run(test.name, func(t *testing.T) {
			f, err := bazel.Runfile(test.path)
			if err != nil {
				t.Fatalf("failed to find tsfile: %v", err)
			}
			tsprocessor, err := ProcessInputs(f)
			if err != nil {
				t.Fatalf("Inputs(f) returned error: %v", err)
			}
			inputs, err := tsprocessor.Inputs()
			if err != nil {
				t.Fatalf("Inputs() returned error: %v", err)
			}
			gotPath := []string{}
			for _, inp := range inputs {
				gotPath = append(gotPath, inp)
			}
			for i := range test.want {
				path, err := bazel.Runfile(test.want[i])
				if err != nil {
					t.Fatalf("failed to find tsfile: %v", err)
				}
				test.want[i] = path
			}
			sort.Strings(test.want)
			sort.Strings(gotPath)

			if diff := cmp.Diff(test.want, gotPath); diff != "" {
				t.Errorf("Inputs() returned diff, (-want, +got): %s", diff)
			}
		})
	}
}
