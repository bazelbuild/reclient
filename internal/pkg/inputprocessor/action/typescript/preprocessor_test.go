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
	"context"
	"path/filepath"
	"testing"

	"team/foundry-x/re-client/internal/pkg/execroot"
	"team/foundry-x/re-client/internal/pkg/inputprocessor"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var (
	strSliceCmp = cmpopts.SortSlices(func(a, b string) bool { return a < b })
)

func TestTscPreprocessor(t *testing.T) {
	er, cleanup := execroot.Setup(t, []string{"t1.ts", "tsconfig.json", "tsc"})
	defer cleanup()
	execroot.AddFileWithContent(t, filepath.Join(er, "tsconfig.json"), []byte(`{"files":["t1.ts"]}`))
	execroot.AddFileWithContent(t, filepath.Join(er, "t1.ts"), []byte(`console.log(t1)`))
	cmd := []string{"tsc", "tsconfig.json"}
	ctx := context.Background()
	pp := &Preprocessor{
		&inputprocessor.BasePreprocessor{
			Ctx: ctx,
		},
	}

	gotSpec, err := inputprocessor.Compute(pp, inputprocessor.Options{
		ExecRoot:        er,
		ToolchainInputs: []string{"tsc"},
		Cmd:             cmd,
	})
	if err != nil {
		t.Errorf("Compute() returned error: %v", err)
	}
	wantSpec := &inputprocessor.ActionSpec{
		InputSpec: &command.InputSpec{
			Inputs: []string{
				"t1.ts",
				"tsc",
				"tsconfig.json",
			},
			EnvironmentVariables: map[string]string{
				"PATH": ".:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
			},
		},
		OutputFiles: []string{
			"t1.js",
		},
	}
	if diff := cmp.Diff(wantSpec, gotSpec, strSliceCmp); diff != "" {
		t.Errorf("GetActionSpec() returned diff in ActionSpec, (-want +got): %s", diff)
	}
}
