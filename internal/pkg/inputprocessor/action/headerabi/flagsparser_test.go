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

package headerabi

import (
	"context"
	"testing"

	"team/foundry-x/re-client/internal/pkg/execroot"
	"team/foundry-x/re-client/internal/pkg/inputprocessor/flags"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

const (
	fakeExecRoot = "fake"
)

func TestParseFlags(t *testing.T) {
	er, cleanup := execroot.Setup(t, nil)
	defer cleanup()
	tests := []struct {
		name       string
		command    []string
		workingDir string
		files      map[string][]byte
		want       *flags.CommandFlags
	}{
		{
			name:       "abi-dumper command",
			workingDir: ".",
			command: []string{
				"prebuilts/clang-tools/linux-x86/bin/header-abi-dumper",
				"--root-dir",
				".",
				"-o",
				"outfile",
				"bionic/libm/upstream-freebsd/lib/msun/src/s_fmax.c",
				"--",
				"-Werror=non-virtual-dtor",
				"-Werror=address",
				"-Werror=sequence-point",
				"-fPIC",
				"-Ibionic/libc/system_properties/include",
				"-isystem",
				"bionic/libc/include",
				"-include",
				"fenv-access.h",
				"-Isystem/core/include",
			},
			want: &flags.CommandFlags{
				ExecutablePath: "prebuilts/clang-tools/linux-x86/bin/header-abi-dumper",
				Flags: []*flags.Flag{
					&flags.Flag{Key: "--root-dir", Value: "."},
					&flags.Flag{Key: "-W", Value: "error=non-virtual-dtor", Joined: true},
					&flags.Flag{Key: "-W", Value: "error=address", Joined: true},
					&flags.Flag{Key: "-W", Value: "error=sequence-point", Joined: true},
					&flags.Flag{Key: "-fPIC"},
					&flags.Flag{
						Key:    "-I",
						Value:  "bionic/libc/system_properties/include",
						Joined: true,
					},
					&flags.Flag{Key: "-isystem", Value: "bionic/libc/include"},
					&flags.Flag{Key: "--include", Value: "fenv-access.h"},
					&flags.Flag{Key: "-I", Value: "system/core/include", Joined: true},
				},
				IncludeDirPaths: []string{"bionic/libc/system_properties/include", "system/core/include"},
				TargetFilePaths: []string{"bionic/libm/upstream-freebsd/lib/msun/src/s_fmax.c"},
				OutputFilePaths: []string{
					"outfile",
				},
				WorkingDirectory: ".",
				ExecRoot:         er,
			},
		},
		{
			name:       "abi-dumper command with multiple root dirs",
			workingDir: ".",
			command: []string{
				"prebuilts/clang-tools/linux-x86/bin/header-abi-dumper",
				"--root-dir",
				".",
				"--root-dir",
				"$OUT_DIR:out",
				"-o",
				"outfile",
				"bionic/libm/upstream-freebsd/lib/msun/src/s_fmax.c",
				"--",
				"-Werror=non-virtual-dtor",
				"-Werror=address",
				"-Werror=sequence-point",
				"-fPIC",
				"-Ibionic/libc/system_properties/include",
				"-isystem",
				"bionic/libc/include",
				"-include",
				"fenv-access.h",
				"-Isystem/core/include",
			},
			want: &flags.CommandFlags{
				ExecutablePath: "prebuilts/clang-tools/linux-x86/bin/header-abi-dumper",
				Flags: []*flags.Flag{
					&flags.Flag{Key: "--root-dir", Value: "."},
					&flags.Flag{Key: "--root-dir", Value: "$OUT_DIR:out"},
					&flags.Flag{Key: "-W", Value: "error=non-virtual-dtor", Joined: true},
					&flags.Flag{Key: "-W", Value: "error=address", Joined: true},
					&flags.Flag{Key: "-W", Value: "error=sequence-point", Joined: true},
					&flags.Flag{Key: "-fPIC"},
					&flags.Flag{
						Key:    "-I",
						Value:  "bionic/libc/system_properties/include",
						Joined: true,
					},
					&flags.Flag{Key: "-isystem", Value: "bionic/libc/include"},
					&flags.Flag{Key: "--include", Value: "fenv-access.h"},
					&flags.Flag{Key: "-I", Value: "system/core/include", Joined: true},
				},
				IncludeDirPaths: []string{"bionic/libc/system_properties/include", "system/core/include"},
				TargetFilePaths: []string{"bionic/libm/upstream-freebsd/lib/msun/src/s_fmax.c"},
				OutputFilePaths: []string{
					"outfile",
				},
				WorkingDirectory: ".",
				ExecRoot:         er,
			},
		},
		{
			name:       "abi-dumper command with no root-dir",
			workingDir: ".",
			command: []string{
				"prebuilts/clang-tools/linux-x86/bin/header-abi-dumper",
				"-o",
				"outfile",
				"bionic/libm/upstream-freebsd/lib/msun/src/s_fmax.c",
				"--",
				"-Werror=non-virtual-dtor",
				"-Werror=address",
				"-Werror=sequence-point",
				"-fPIC",
				"-Ibionic/libc/system_properties/include",
				"-isystem",
				"bionic/libc/include",
				"-include",
				"fenv-access.h",
				"-Isystem/core/include",
			},
			want: &flags.CommandFlags{
				ExecutablePath: "prebuilts/clang-tools/linux-x86/bin/header-abi-dumper",
				Flags: []*flags.Flag{
					&flags.Flag{Key: "-W", Value: "error=non-virtual-dtor", Joined: true},
					&flags.Flag{Key: "-W", Value: "error=address", Joined: true},
					&flags.Flag{Key: "-W", Value: "error=sequence-point", Joined: true},
					&flags.Flag{Key: "-fPIC"},
					&flags.Flag{
						Key:    "-I",
						Value:  "bionic/libc/system_properties/include",
						Joined: true,
					},
					&flags.Flag{Key: "-isystem", Value: "bionic/libc/include"},
					&flags.Flag{Key: "--include", Value: "fenv-access.h"},
					&flags.Flag{Key: "-I", Value: "system/core/include", Joined: true},
				},
				IncludeDirPaths: []string{"bionic/libc/system_properties/include", "system/core/include"},
				TargetFilePaths: []string{"bionic/libm/upstream-freebsd/lib/msun/src/s_fmax.c"},
				OutputFilePaths: []string{
					"outfile",
				},
				WorkingDirectory: ".",
				ExecRoot:         er,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			execroot.AddFilesWithContent(t, er, test.files)
			got, err := Parser{}.ParseFlags(ctx, test.command, test.workingDir, er)
			if err != nil {
				t.Errorf("ParseFlags(%v,%v,%v).err = %v, want no error.", test.command, test.workingDir, er, err)
			}

			if diff := cmp.Diff(test.want, got, cmpopts.IgnoreUnexported(flags.Flag{})); diff != "" {
				t.Errorf("Test %v, ParseFlags(%v,%v,%v) returned diff, (-want +got): %s", test.name, test.command, test.workingDir, er, diff)
			}
		})
	}
}
