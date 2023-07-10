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

package r8

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

func TestR8Preprocessor(t *testing.T) {
	er, cleanup := execroot.Setup(t, []string{"a", "b", "c", "lib", "r8.jar", "java", "file.jar"})
	defer cleanup()
	execroot.AddFilesWithContent(t, er, map[string][]byte{
		"subdir/proguard.flags":        []byte("flag\n-include sibling.flags"),
		"subdir/sibling.flags":         []byte("flag\n-include ../base.flags"),
		"base.flags":                   []byte("flag"),
		"java_remote_toolchain_inputs": []byte("lib"),
	})
	execroot.AddDirs(t, er, []string{"dex"})
	cmd := []string{"r8-compat-proguard", "-J-Xmx2048M", "-injars", "file.jar", "--output", "dex", "-printmapping", "proguard_dict", "-printconfiguration", "print_conf", "-libraryjars", "a:b:c", "-include", "d", "-include", "e", "--main-dex-list", "f", "-include", "subdir/proguard.flags"}
	pp := &Preprocessor{
		&inputprocessor.BasePreprocessor{
			Ctx: context.Background(),
		},
	}
	gotSpec, err := inputprocessor.Compute(pp, inputprocessor.Options{
		ExecRoot:        er,
		ToolchainInputs: []string{"java"},
		Cmd:             cmd,
		Inputs:          &command.InputSpec{Inputs: []string{"r8.jar"}},
	})
	if err != nil {
		t.Errorf("Compute() returned error: %v", err)
	}

	wantSpec := &inputprocessor.ActionSpec{
		InputSpec: &command.InputSpec{
			Inputs: []string{"a", "b", "c", "file.jar", "lib", "r8.jar", "java",
				filepath.Clean("subdir/proguard.flags"), filepath.Clean("subdir/sibling.flags"), "base.flags"},
			EnvironmentVariables: map[string]string{
				"PATH": ".:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
			},
			VirtualInputs: []*command.VirtualInput{
				&command.VirtualInput{Path: "dex", IsEmptyDirectory: true},
			},
		},
		OutputFiles:       []string{"print_conf", "proguard_dict"},
		OutputDirectories: []string{"dex"},
	}
	if diff := cmp.Diff(wantSpec, gotSpec, strSliceCmp); diff != "" {
		t.Errorf("Compute() returned diff in ActionSpec, (-want +got): %s", diff)
	}
}
