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

package cppcompile

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
			name:       "Simple clang command",
			workingDir: ".",
			command:    []string{"clang++", "-c", "test.cpp"},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					&flags.Flag{Key: "-c"},
				},
				TargetFilePaths:  []string{"test.cpp"},
				WorkingDirectory: ".",
				ExecRoot:         er,
			},
		},
		{
			name:       "Simple clang command with working directory",
			workingDir: "foo",
			command:    []string{"clang++", "-c", "test.cpp"},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					&flags.Flag{Key: "-c"},
				},
				TargetFilePaths:  []string{"test.cpp"},
				WorkingDirectory: "foo",
				ExecRoot:         er,
			},
		},
		{
			name:       "Simple clang command with outputs",
			workingDir: ".",
			command:    []string{"clang++", "-c", "-o", "test.o", "-MF", "test.d", "test.cpp"},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					&flags.Flag{Key: "-c"},
				},
				TargetFilePaths:       []string{"test.cpp"},
				EmittedDependencyFile: "test.d",
				WorkingDirectory:      ".",
				ExecRoot:              er,
				OutputFilePaths:       []string{"test.o", "test.d"},
			},
		},
		{
			name:       "Simple clang command with rsp file",
			workingDir: ".",
			command:    []string{"clang++", "@rsp", "-c", "-o", "test.o", "-MF", "test.d", "test.cpp"},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					&flags.Flag{Key: "-c"},
				},
				TargetFilePaths:       []string{"test.cpp"},
				EmittedDependencyFile: "test.d",
				WorkingDirectory:      ".",
				ExecRoot:              er,
				Dependencies:          []string{"rsp"},
				OutputFilePaths:       []string{"test.o", "test.d"},
			},
		},
		{
			name:       "Simple clang command with outputs and working directory",
			workingDir: "foo",
			command:    []string{"clang++", "-c", "-o", "test.o", "-MF", "test.d", "test.cpp"},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					&flags.Flag{Key: "-c"},
				},
				TargetFilePaths:       []string{"test.cpp"},
				EmittedDependencyFile: "test.d",
				WorkingDirectory:      "foo",
				ExecRoot:              er,
				OutputFilePaths:       []string{"test.o", "test.d"},
			},
		},
		{
			name:       "Clang command with an fprofile file",
			workingDir: ".",
			command:    []string{"clang++", "-c", "-o", "test.o", "-fprofile-use=prof.txt", "-MF", "test.d", "test.cpp"},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					&flags.Flag{Key: "-c"},
					&flags.Flag{Key: "-fprofile-use=", Value: "prof.txt", Joined: true},
				},
				TargetFilePaths:       []string{"test.cpp"},
				EmittedDependencyFile: "test.d",
				Dependencies:          []string{"prof.txt"},
				WorkingDirectory:      ".",
				ExecRoot:              er,
				OutputFilePaths:       []string{"test.o", "test.d"},
			},
		},
		{
			name:       "Clang command with an fprofile-list file",
			workingDir: ".",
			command:    []string{"clang++", "-c", "-o", "test.o", "-fprofile-list=fun1.list", "-fprofile-list=fun2.list", "-MF", "test.d", "test.cpp"},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					&flags.Flag{Key: "-c"},
					&flags.Flag{Key: "-fprofile-list=", Value: "fun1.list", Joined: true},
					&flags.Flag{Key: "-fprofile-list=", Value: "fun2.list", Joined: true},
				},
				TargetFilePaths:       []string{"test.cpp"},
				EmittedDependencyFile: "test.d",
				Dependencies:          []string{"fun1.list", "fun2.list"},
				WorkingDirectory:      ".",
				ExecRoot:              er,
				OutputFilePaths:       []string{"test.o", "test.d"},
			},
		},
		{
			name:       "Clang command with explicit coverage file",
			workingDir: ".",
			command:    []string{"clang++", "-c", "-o", "test.o", "--coverage-data-file=foo.gcno", "-MF", "test.d", "test.cpp"},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					&flags.Flag{Key: "-c"},
					&flags.Flag{Key: "--coverage-data-file=", Value: "foo.gcno", Joined: true},
				},
				TargetFilePaths:       []string{"test.cpp"},
				EmittedDependencyFile: "test.d",
				WorkingDirectory:      ".",
				ExecRoot:              er,
				OutputFilePaths:       []string{"test.o", "foo.gcno", "test.d"},
			},
		},
		{
			name:       "Clang command with coverage",
			workingDir: ".",
			command:    []string{"clang++", "-c", "-o", "test.o", "--coverage", "-MF", "test.d", "test.cpp"},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					&flags.Flag{Key: "-c"},
					&flags.Flag{Key: "--coverage"},
				},
				TargetFilePaths:       []string{"test.cpp"},
				EmittedDependencyFile: "test.d",
				WorkingDirectory:      ".",
				ExecRoot:              er,
				OutputFilePaths:       []string{"test.o", "test.d", "test.gcno"},
			},
		},
		{
			name:       "Clang command with coverage before .o",
			workingDir: ".",
			command:    []string{"clang++", "-c", "-ftest-coverage", "-o", "test.o", "-MF", "test.d", "test.cpp"},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					&flags.Flag{Key: "-c"},
					&flags.Flag{Key: "-ftest-coverage"},
				},
				TargetFilePaths:       []string{"test.cpp"},
				EmittedDependencyFile: "test.d",
				WorkingDirectory:      ".",
				ExecRoot:              er,
				OutputFilePaths:       []string{"test.o", "test.d", "test.gcno"},
			},
		},
		{
			name:       "Clang command with gsplit-dwarf",
			workingDir: ".",
			command:    []string{"clang++", "-c", "-o", "test.o", "-gsplit-dwarf", "-MF", "test.d", "test.cpp"},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					&flags.Flag{Key: "-c"},
					&flags.Flag{Key: "-gsplit-dwarf"},
				},
				TargetFilePaths:       []string{"test.cpp"},
				EmittedDependencyFile: "test.d",
				WorkingDirectory:      ".",
				ExecRoot:              er,
				OutputFilePaths:       []string{"test.o", "test.d", "test.dwo"},
			},
		},
		{
			name:       "Clang command with an fsanitize-blacklist file",
			workingDir: ".",
			command:    []string{"clang++", "-c", "-o", "test.o", "-fsanitize-blacklist=blacklist.txt", "-MF", "test.d", "test.cpp"},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					&flags.Flag{Key: "-c"},
					&flags.Flag{Key: "-fsanitize-blacklist=", Value: "blacklist.txt", Joined: true},
				},
				TargetFilePaths:       []string{"test.cpp"},
				EmittedDependencyFile: "test.d",
				Dependencies:          []string{"blacklist.txt"},
				WorkingDirectory:      ".",
				ExecRoot:              er,
				OutputFilePaths:       []string{"test.o", "test.d"},
			},
		},
		{
			name:       "Clang command with an fsanitize-ignorelist file",
			workingDir: ".",
			command:    []string{"clang++", "-c", "-o", "test.o", "-fsanitize-ignorelist=ignorelist.txt", "-MF", "test.d", "test.cpp"},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					&flags.Flag{Key: "-c"},
					&flags.Flag{Key: "-fsanitize-ignorelist=", Value: "ignorelist.txt", Joined: true},
				},
				TargetFilePaths:       []string{"test.cpp"},
				EmittedDependencyFile: "test.d",
				Dependencies:          []string{"ignorelist.txt"},
				WorkingDirectory:      ".",
				ExecRoot:              er,
				OutputFilePaths:       []string{"test.o", "test.d"},
			},
		},
		{
			name:       "Clang command with an fprofile-sample-use file",
			workingDir: ".",
			command:    []string{"clang++", "-c", "-o", "test.o", "-fprofile-sample-use=sample.txt", "-MF", "test.d", "test.cpp"},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					&flags.Flag{Key: "-c"},
					&flags.Flag{Key: "-fprofile-sample-use=", Value: "sample.txt", Joined: true},
				},
				TargetFilePaths:       []string{"test.cpp"},
				EmittedDependencyFile: "test.d",
				Dependencies:          []string{"sample.txt"},
				WorkingDirectory:      ".",
				ExecRoot:              er,
				OutputFilePaths:       []string{"test.o", "test.d"},
			},
		},
		{
			name:       "Clang command with a -B<dir> flag",
			workingDir: ".",
			command:    []string{"clang++", "-c", "-o", "test.o", "-Bprebuilts/gcc/linux-x86/bin/", "-MF", "test.d", "test.cpp"},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					&flags.Flag{Key: "-c"},
					&flags.Flag{Key: "-B", Value: "prebuilts/gcc/linux-x86/bin/", Joined: true},
				},
				TargetFilePaths:       []string{"test.cpp"},
				EmittedDependencyFile: "test.d",
				Dependencies:          []string{"prebuilts/gcc/linux-x86/bin/"},
				WorkingDirectory:      ".",
				ExecRoot:              er,
				OutputFilePaths:       []string{"test.o", "test.d"},
			},
		},
		{
			name:       "Clang command with a -B <dir> flag",
			workingDir: ".",
			command:    []string{"clang++", "-c", "-o", "test.o", "-B", "prebuilts/gcc/linux-x86/bin/", "-MF", "test.d", "test.cpp"},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					&flags.Flag{Key: "-c"},
					&flags.Flag{Key: "-B", Value: "prebuilts/gcc/linux-x86/bin/"},
				},
				TargetFilePaths:       []string{"test.cpp"},
				EmittedDependencyFile: "test.d",
				Dependencies:          []string{"prebuilts/gcc/linux-x86/bin/"},
				WorkingDirectory:      ".",
				ExecRoot:              er,
				OutputFilePaths:       []string{"test.o", "test.d"},
			},
		},
		{
			name:       "Clang command with include directories",
			workingDir: ".",
			command: []string{
				"clang++",
				"-Iincludes/bla",
				"-I",
				"moreincludes",
				"-Iincludes/boo",
				"-I",
				"includes/bla",
				"-c",
				"-o",
				"test.o",
				"test.cpp",
			},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					&flags.Flag{Key: "-I", Value: "includes/bla", Joined: true},
					&flags.Flag{Key: "-I", Value: "moreincludes"},
					&flags.Flag{Key: "-I", Value: "includes/boo", Joined: true},
					&flags.Flag{Key: "-I", Value: "includes/bla"},
					&flags.Flag{Key: "-c"},
				},
				TargetFilePaths:  []string{"test.cpp"},
				IncludeDirPaths:  []string{"includes/bla", "moreincludes", "includes/boo", "includes/bla"},
				WorkingDirectory: ".",
				ExecRoot:         er,
				OutputFilePaths:  []string{"test.o"},
			},
		},
		{
			name:       "Clang command with include directories and working directory",
			workingDir: "foo",
			command: []string{
				"clang++",
				"-Iincludes/bla",
				"-I",
				"moreincludes",
				"-Iincludes/boo",
				"-I",
				"includes/bla",
				"-c",
				"-o",
				"test.o",
				"test.cpp",
			},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					{Key: "-I", Value: "includes/bla", Joined: true},
					{Key: "-I", Value: "moreincludes"},
					{Key: "-I", Value: "includes/boo", Joined: true},
					{Key: "-I", Value: "includes/bla"},
					{Key: "-c"},
				},
				TargetFilePaths:  []string{"test.cpp"},
				IncludeDirPaths:  []string{"includes/bla", "moreincludes", "includes/boo", "includes/bla"},
				WorkingDirectory: "foo",
				ExecRoot:         er,
				OutputFilePaths:  []string{"test.o"},
			},
		},
		{
			name:       "Clang command with include directories, working directory and arguments with parameter",
			workingDir: "foo",
			command: []string{
				"clang++",
				"-Iincludes/bla",
				"-I",
				"moreincludes",
				"-Iincludes/boo",
				"-I",
				"includes/bla",
				"-DBAR",
				"-D",
				"BAZ",
				"-fmax-tokens",
				"32",
				"-c",
				"-o",
				"test.o",
				"test.cpp",
			},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					&flags.Flag{Key: "-I", Value: "includes/bla", Joined: true},
					&flags.Flag{Key: "-I", Value: "moreincludes"},
					&flags.Flag{Key: "-I", Value: "includes/boo", Joined: true},
					&flags.Flag{Key: "-I", Value: "includes/bla"},
					&flags.Flag{Key: "-D", Value: "BAR", Joined: true},
					&flags.Flag{Key: "-D", Value: "BAZ"},
					&flags.Flag{Key: "-fmax-tokens", Value: "32"},
					&flags.Flag{Key: "-c"},
				},
				TargetFilePaths:  []string{"test.cpp"},
				IncludeDirPaths:  []string{"includes/bla", "moreincludes", "includes/boo", "includes/bla"},
				WorkingDirectory: "foo",
				ExecRoot:         er,
				OutputFilePaths:  []string{"test.o"},
			},
		},
		{
			name:       "Clang command with include directories, working directory and arguments with parameter, target file not last",
			workingDir: "foo",
			command: []string{
				"clang++",
				"-Iincludes/bla",
				"-I",
				"moreincludes",
				"-Iincludes/boo",
				"-I",
				"includes/bla",
				"-DBAR",
				"-D",
				"BAZ",
				"-fmax-tokens",
				"32",
				"-c",
				"test.cpp",
				"-o",
				"test.o",
			},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					&flags.Flag{Key: "-I", Value: "includes/bla", Joined: true},
					&flags.Flag{Key: "-I", Value: "moreincludes"},
					&flags.Flag{Key: "-I", Value: "includes/boo", Joined: true},
					&flags.Flag{Key: "-I", Value: "includes/bla"},
					&flags.Flag{Key: "-D", Value: "BAR", Joined: true},
					&flags.Flag{Key: "-D", Value: "BAZ"},
					&flags.Flag{Key: "-fmax-tokens", Value: "32"},
					&flags.Flag{Key: "-c"},
				},
				TargetFilePaths:  []string{"test.cpp"},
				IncludeDirPaths:  []string{"includes/bla", "moreincludes", "includes/boo", "includes/bla"},
				WorkingDirectory: "foo",
				ExecRoot:         er,
				OutputFilePaths:  []string{"test.o"},
			},
		},
		{
			name:       "ThinLTO clang command",
			workingDir: ".",
			command:    []string{"clang", "-c", "-fthinlto-index=foo.o.thinlto.bc", "test.cpp"},
			want: &flags.CommandFlags{
				ExecutablePath: "clang",
				Flags: []*flags.Flag{
					&flags.Flag{Key: "-c"},
					&flags.Flag{Key: "-fthinlto-index=", Value: "foo.o.thinlto.bc", Joined: true},
				},
				TargetFilePaths:  []string{"test.cpp"},
				Dependencies:     []string{"foo.o.thinlto.bc"},
				WorkingDirectory: ".",
				ExecRoot:         er,
			},
		}, {
			name:       "ThinLTO clang command with working dir",
			workingDir: "out",
			command:    []string{"clang", "-c", "-fthinlto-index=lto.foo/foo.o.thinlto.bc", "test.cpp"},
			want: &flags.CommandFlags{
				ExecutablePath: "clang",
				Flags: []*flags.Flag{
					&flags.Flag{Key: "-c"},
					&flags.Flag{Key: "-fthinlto-index=", Value: "lto.foo/foo.o.thinlto.bc", Joined: true},
				},
				TargetFilePaths:  []string{"test.cpp"},
				Dependencies:     []string{"lto.foo/foo.o.thinlto.bc"},
				WorkingDirectory: "out",
				ExecRoot:         er,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			execroot.AddFilesWithContent(t, er, test.files)
			got, err := ClangParser{}.ParseFlags(ctx, test.command, test.workingDir, er)
			if err != nil {
				t.Errorf("ParseFlags(%v,%v,%v).err = %v, want no error.", test.command, test.workingDir, er, err)
			}

			if diff := cmp.Diff(test.want, got, cmpopts.IgnoreUnexported(flags.Flag{})); diff != "" {
				t.Errorf("Test %v, ParseFlags(%v,%v,%v) returned diff, (-want +got): %s", test.name, test.command, test.workingDir, er, diff)
			}
		})
	}
}
