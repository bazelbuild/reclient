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

package javac

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

func TestJavacPreprocessor(t *testing.T) {
	er, cleanup := execroot.Setup(t, []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "lib", "javac"})
	defer cleanup()
	execroot.AddFileWithContent(t, filepath.Join(er, "test.rsp"), []byte("h i\nj"))
	execroot.AddFileWithContent(t, filepath.Join(er, "javac_remote_toolchain_inputs"), []byte("lib"))
	cmd := []string{"javac", "-J-Xmx2048M", "-bootclasspath", "a:b:c", "-classpath", "d:e:f", "-processorpath", "g", "--system=j", "-Aroom.schemaLocation=k", "-d", "out1/", "-s", "out2/", "@test.rsp"}
	ctx := context.Background()
	pp := &Preprocessor{
		&inputprocessor.BasePreprocessor{
			Ctx: ctx,
		},
	}
	gotSpec, err := inputprocessor.Compute(pp, inputprocessor.Options{
		ExecRoot:        er,
		ToolchainInputs: []string{"javac"},
		Cmd:             cmd,
	})
	if err != nil {
		t.Errorf("Compute() returned error: %v", err)
	}
	wantSpec := &inputprocessor.ActionSpec{
		InputSpec: &command.InputSpec{
			Inputs: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "test.rsp", "lib", "javac"},
			EnvironmentVariables: map[string]string{
				"PATH": ".:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
			},
		},
		OutputDirectories: []string{"out1", "out2"},
	}
	if diff := cmp.Diff(wantSpec, gotSpec, strSliceCmp); diff != "" {
		t.Errorf("GetActionSpec() returned diff in ActionSpec, (-want +got): %s", diff)
	}
}
