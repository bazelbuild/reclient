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

package clanglink

import (
	"os"
	"path/filepath"
	"testing"

	"team/foundry-x/re-client/internal/pkg/execroot"
	"team/foundry-x/re-client/internal/pkg/inputprocessor"
	"team/foundry-x/re-client/internal/pkg/inputprocessor/flags"

	"github.com/bazelbuild/rules_go/go/tools/bazel"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestClangLinkParser(t *testing.T) {
	er, cleanup := execroot.Setup(t, nil)
	defer cleanup()
	test := []struct {
		name          string
		command       []string
		existingFiles map[string][]byte
		want          *flags.CommandFlags
	}{
		{
			name:    "Clang link command with a --sysroot <dir> flag, added as dependency",
			command: []string{"clang++", "-c", "-o", "test.o", "--sysroot", "prebuilts/gcc/linux-x86/bin/", "test.cpp"},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					&flags.Flag{Value: "-c"},
					&flags.Flag{Value: "--sysroot"},
					&flags.Flag{Value: "prebuilts/gcc/linux-x86/bin/"},
					&flags.Flag{Value: "test.cpp"},
				},
				Dependencies: []string{
					"prebuilts/gcc/linux-x86/bin/",
					"test.cpp",
				},
				ExecRoot:        er,
				OutputFilePaths: []string{"test.o"},
			},
		},
		{
			name:    "Clang link command with a --sysroot=<dir> flag, added as dependency",
			command: []string{"clang++", "-c", "-o", "test.o", "--sysroot=prebuilts/gcc/linux-x86/bin/", "test.cpp"},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					&flags.Flag{Value: "-c"},
					&flags.Flag{Value: "--sysroot=prebuilts/gcc/linux-x86/bin/"},
					&flags.Flag{Value: "test.cpp"},
				},
				Dependencies: []string{
					"prebuilts/gcc/linux-x86/bin/",
					"test.cpp",
				},
				ExecRoot:        er,
				OutputFilePaths: []string{"test.o"},
			},
		},
		{
			name:          "Clang link command with an rsp file specified with @ arg, file contents added as dependency",
			command:       []string{"clang++", "-fuse-ld=lld", "-o", "test", "--sysroot", "prebuilts/gcc/linux-x86/bin/", "@test.rsp"},
			existingFiles: map[string][]byte{"test.rsp": []byte("test.o")},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					&flags.Flag{Value: "-fuse-ld=lld"},
					&flags.Flag{Value: "--sysroot"},
					&flags.Flag{Value: "prebuilts/gcc/linux-x86/bin/"},
					&flags.Flag{Value: "@test.rsp"},
				},
				Dependencies: []string{
					"prebuilts/gcc/linux-x86/bin/",
					"test.rsp",
					"test.o",
				},
				ExecRoot:        er,
				OutputFilePaths: []string{"test"},
			},
		},
		{
			name:    "Clang link command with an archive which is scanned and contents added as dependency",
			command: []string{"clang++", "-fuse-ld=lld", "-o", "test", "--sysroot", "prebuilts/gcc/linux-x86/bin/", "testarchive.a"},

			existingFiles: map[string][]byte{
				"testarchive.a": []byte(
					"!<arch>\n" +
						"foo.o           0           0     0     644     4         `\n" +
						"foo\n" +
						"bar.o           0           0     0     644     4         `\n" +
						"bar\n" +
						"baz.o           0           0     0     644     4         `\n" +
						"baz",
				),
			},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					&flags.Flag{Value: "-fuse-ld=lld"},
					&flags.Flag{Value: "--sysroot"},
					&flags.Flag{Value: "prebuilts/gcc/linux-x86/bin/"},
					&flags.Flag{Value: "testarchive.a"},
				},
				Dependencies: []string{
					"prebuilts/gcc/linux-x86/bin/",
					"foo.o",
					"bar.o",
					"baz.o",
					"testarchive.a",
				},
				ExecRoot:        er,
				OutputFilePaths: []string{"test"},
			},
		},
		{
			name:    "Clang link command with an rsp file specified with @ arg, which contains archive which is scanned and contents added as dependency",
			command: []string{"clang++", "-fuse-ld=lld", "-o", "test", "--sysroot", "prebuilts/gcc/linux-x86/bin/", "@test.rsp"},

			existingFiles: map[string][]byte{
				"test.rsp": []byte("testarchive.a"),
				"testarchive.a": []byte(
					"!<arch>\n" +
						"foo.o           0           0     0     644     4         `\n" +
						"foo\n" +
						"bar.o           0           0     0     644     4         `\n" +
						"bar\n" +
						"baz.o           0           0     0     644     4         `\n" +
						"baz",
				),
			},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					&flags.Flag{Value: "-fuse-ld=lld"},
					&flags.Flag{Value: "--sysroot"},
					&flags.Flag{Value: "prebuilts/gcc/linux-x86/bin/"},
					&flags.Flag{Value: "@test.rsp"},
				},
				Dependencies: []string{
					"prebuilts/gcc/linux-x86/bin/",
					"test.rsp",
					"foo.o",
					"bar.o",
					"baz.o",
					"testarchive.a",
				},
				ExecRoot:        er,
				OutputFilePaths: []string{"test"},
			},
		},
		{
			name:          "Clang link command with -L flag",
			command:       []string{"clang++", "-fuse-ld=lld", "-o", "test", "-L", "prebuilts/gcc/linux-x86/lib", "-Lprebuilts/gcc/linux-x86/lib2", "--sysroot", "prebuilts/gcc/linux-x86/bin/", "@test.rsp"},
			existingFiles: map[string][]byte{"test.rsp": []byte("test.o")},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					&flags.Flag{Value: "-fuse-ld=lld"},
					&flags.Flag{Value: "-L"},
					&flags.Flag{Value: "prebuilts/gcc/linux-x86/lib"},
					&flags.Flag{Value: "-Lprebuilts/gcc/linux-x86/lib2"},
					&flags.Flag{Value: "--sysroot"},
					&flags.Flag{Value: "prebuilts/gcc/linux-x86/bin/"},
					&flags.Flag{Value: "@test.rsp"},
				},
				Dependencies: []string{
					"prebuilts/gcc/linux-x86/lib",
					"prebuilts/gcc/linux-x86/lib2",
					"prebuilts/gcc/linux-x86/bin/",
					"test.rsp",
					"test.o",
				},
				ExecRoot:        er,
				OutputFilePaths: []string{"test"},
			},
		},
		{
			name:          "Clang link command with files in invocation - added as dependency",
			command:       []string{"clang++", "-fuse-ld=lld", "-o", "test", "-L", "prebuilts/gcc/linux-x86/lib", "prebuilts/gcc/linux-x86/lib2.so", "--sysroot", "prebuilts/gcc/linux-x86/bin/", "@test.rsp"},
			existingFiles: map[string][]byte{"test.rsp": []byte("test.o")},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					&flags.Flag{Value: "-fuse-ld=lld"},
					&flags.Flag{Value: "-L"},
					&flags.Flag{Value: "prebuilts/gcc/linux-x86/lib"},
					&flags.Flag{Value: "prebuilts/gcc/linux-x86/lib2.so"},
					&flags.Flag{Value: "--sysroot"},
					&flags.Flag{Value: "prebuilts/gcc/linux-x86/bin/"},
					&flags.Flag{Value: "@test.rsp"},
				},
				Dependencies: []string{
					"prebuilts/gcc/linux-x86/lib",
					"prebuilts/gcc/linux-x86/lib2.so",
					"prebuilts/gcc/linux-x86/bin/",
					"test.rsp",
					"test.o",
				},
				ExecRoot:        er,
				OutputFilePaths: []string{"test"},
			},
		},
		{
			name:    "Clang link command with -Wl,--out-implib argument",
			command: []string{"clang++", "-fuse-ld=lld", "-o", "test", "-L", "prebuilts/gcc/linux-x86/lib", "prebuilts/gcc/linux-x86/lib2.so", "-Wl,--out-implib=bar.dll"},
			want: &flags.CommandFlags{
				ExecutablePath: "clang++",
				Flags: []*flags.Flag{
					&flags.Flag{Value: "-fuse-ld=lld"},
					&flags.Flag{Value: "-L"},
					&flags.Flag{Value: "prebuilts/gcc/linux-x86/lib"},
					&flags.Flag{Value: "prebuilts/gcc/linux-x86/lib2.so"},
					&flags.Flag{Value: "-Wl,--out-implib=bar.dll"},
				},
				Dependencies: []string{
					"prebuilts/gcc/linux-x86/lib",
					"prebuilts/gcc/linux-x86/lib2.so",
				},
				ExecRoot:        er,
				OutputFilePaths: []string{"test", "bar.dll"},
			},
		},
	}
	for _, test := range test {
		t.Run(test.name, func(t *testing.T) {
			execroot.AddFilesWithContent(t, er, test.existingFiles)
			defer func() {
				for f := range test.existingFiles {
					if err := os.Remove(filepath.Join(er, f)); err != nil {
						// Error because they can affect other tests.
						t.Errorf("Failed to clean test file: %v", err)
					}
				}
			}()

			p := &Preprocessor{
				&inputprocessor.BasePreprocessor{
					Options: inputprocessor.Options{
						Cmd:      test.command,
						ExecRoot: er,
					},
				},
				true,
			}
			if err := p.ParseFlags(); err != nil {
				t.Errorf("ParseFlags() returned error: %v", err)
			}
			if diff := cmp.Diff(test.want, p.Flags, cmpopts.IgnoreUnexported(flags.Flag{})); diff != "" {
				t.Errorf("ParseFlags() returned diff, (-want +got): %s", diff)
			}
		})
	}
}

// TestArchiveDeep scans the test archive and verifies the contents are returned.
func TestArchiveDeep(t *testing.T) {
	wantContents := []string{
		"foo.o",
		"bar.o",
		"baz.o",
	}

	f, err := bazel.Runfile("testdata/testarchive.a")
	if err != nil {
		t.Fatalf("TestArchiveDeep: Failed to find archive %v", err)
	}

	deps, err := readArchive(f, "")
	if err != nil {
		t.Fatalf("TestArchiveDeep: Failed to read archive %v", err)
	}

	if diff := cmp.Diff(wantContents, deps); diff != "" {
		t.Errorf("TestArchiveDeep: returned diff, (-want +got): %s", diff)
	}
}

// TestArchiveDeepFailure ensures an error is returned if the archive could not be read.
func TestArchiveDeepFailure(t *testing.T) {

	f := "testdata/missingarchive.a"

	_, err := readArchive(f, "")
	if err == nil {
		t.Errorf("TestArchiveDeepFailure: readArchive successful; expected failure")
	}
}
