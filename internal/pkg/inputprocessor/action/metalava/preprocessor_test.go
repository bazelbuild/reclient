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

package metalava

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

const (
	wd = "wd"
)

var (
	strSliceCmp = cmpopts.SortSlices(func(a, b string) bool { return a < b })
)

func TestMetalavaPreprocessor(t *testing.T) {
	existingFiles := []string{
		filepath.Clean("wd/foo.java"),
		filepath.Clean("wd/bar/bar.java"),
		filepath.Clean("metalava.jar"),
	}
	er, cleanup := execroot.Setup(t, append(existingFiles, filepath.Join(wd, "remote_toolchain_inputs")))
	defer cleanup()
	execroot.AddFileWithContent(t, filepath.Join(er, wd, "foo.rsp"), []byte("foo.java"))
	execroot.AddDirs(t, er, []string{"wd/src"})
	cmd := []string{"metalava", "-classpath", "bar", "--api", "api.txt", "@foo.rsp", "-sourcepath", "src"}
	ctx := context.Background()
	i := &command.InputSpec{
		Inputs:               []string{filepath.Clean("metalava.jar")},
		EnvironmentVariables: map[string]string{"FOO": filepath.Join(er, "foo")},
	}
	pp := &Preprocessor{
		&inputprocessor.BasePreprocessor{
			Ctx:      ctx,
			Executor: &execStub{stdout: "Metalava: 1.3.0"},
		},
	}
	gotSpec, err := inputprocessor.Compute(pp, inputprocessor.Options{
		Cmd:        cmd,
		WorkingDir: wd,
		ExecRoot:   er,
		Inputs:     i,
	})
	if err != nil {
		t.Errorf("Compute() returned error: %v", err)
	}

	wantSpec := &inputprocessor.ActionSpec{
		InputSpec: &command.InputSpec{
			Inputs: []string{filepath.Clean("wd/foo.rsp"), filepath.Clean("wd/bar"), filepath.Clean("wd/foo.java"), filepath.Clean("metalava.jar")},
			VirtualInputs: []*command.VirtualInput{
				&command.VirtualInput{Path: filepath.Clean("wd/src"), IsEmptyDirectory: true},
			},
			EnvironmentVariables: map[string]string{
				"FOO": filepath.Join("..", "foo"),
			},
		},
		OutputFiles: []string{filepath.Clean("wd/api.txt")},
	}
	if diff := cmp.Diff(wantSpec, gotSpec, strSliceCmp); diff != "" {
		t.Errorf("Compute() returned diff in ActionSpec, (-want +got): %s", diff)
	}
}

type execStub struct {
	stdout string
	stderr string
	err    error
}

func (e *execStub) Execute(ctx context.Context, cmd *command.Command) (string, string, error) {
	return e.stdout, e.stderr, e.err
}
