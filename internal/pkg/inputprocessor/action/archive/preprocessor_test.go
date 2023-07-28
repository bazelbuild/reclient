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

package archive

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bazelbuild/reclient/internal/pkg/execroot"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/flags"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestArchiveParser(t *testing.T) {
	root, cleanup := execroot.Setup(t, nil)
	defer cleanup()
	test := []struct {
		name          string
		command       []string
		existingFiles map[string][]byte
		want          *flags.CommandFlags
	}{
		{
			name:    "regular",
			command: []string{"ar", "-T", "-r", "-c", "-s", "-D", "foo.a", "bar.so"},
			want: &flags.CommandFlags{
				ExecutablePath: "ar",
				Flags: []*flags.Flag{
					&flags.Flag{Value: "-T"},
					&flags.Flag{Value: "-r"},
					&flags.Flag{Value: "-c"},
					&flags.Flag{Value: "-s"},
					&flags.Flag{Value: "-D"},
				},
				Dependencies: []string{
					"bar.so",
				},
				ExecRoot:        root,
				OutputFilePaths: []string{"foo.a"},
			},
		},
		{
			name:          "using rsp",
			command:       []string{"ar", "-T", "-r", "-c", "-s", "-D", "foo.a", "bar.so", "@foo.a.rsp"},
			existingFiles: map[string][]byte{"foo.a.rsp": []byte("baz.so bas.so")},
			want: &flags.CommandFlags{
				ExecutablePath: "ar",
				Flags: []*flags.Flag{
					&flags.Flag{Value: "-T"},
					&flags.Flag{Value: "-r"},
					&flags.Flag{Value: "-c"},
					&flags.Flag{Value: "-s"},
					&flags.Flag{Value: "-D"},
				},
				Dependencies: []string{
					"bar.so",
					"foo.a.rsp",
					"baz.so",
					"bas.so",
				},
				ExecRoot:        root,
				OutputFilePaths: []string{"foo.a"},
			},
		},
		{
			name:    "print",
			command: []string{"ar", "-t", "foo.a"},
			want: &flags.CommandFlags{
				ExecutablePath: "ar",
				Flags: []*flags.Flag{
					&flags.Flag{Value: "-t"},
				},
				Dependencies: []string{"foo.a"},
				ExecRoot:     root,
			},
		},
		{
			name:    "delete",
			command: []string{"ar", "-d", "foo.a", "foo.so"},
			want: &flags.CommandFlags{
				ExecutablePath: "ar",
				Flags: []*flags.Flag{
					&flags.Flag{Value: "-d"},
				},
				Dependencies:    []string{"foo.a"},
				OutputFilePaths: []string{"foo.a"},
				ExecRoot:        root,
			},
		},
	}

	for _, test := range test {
		t.Run(test.name, func(t *testing.T) {
			execroot.AddFilesWithContent(t, root, test.existingFiles)
			defer func() {
				for f := range test.existingFiles {
					if err := os.Remove(filepath.Join(root, f)); err != nil {
						// Fatal because they can affect other tests.
						t.Fatalf("Failed to clean test file: %v", err)
					}
				}
			}()

			p := &Preprocessor{&inputprocessor.BasePreprocessor{
				Options: inputprocessor.Options{
					Cmd:      test.command,
					ExecRoot: root,
				},
			}}

			if err := p.ParseFlags(); err != nil {
				t.Errorf("ParseFlags() returned error: %v", err)
			}
			if diff := cmp.Diff(test.want, p.Flags, cmpopts.IgnoreUnexported(flags.Flag{})); diff != "" {
				t.Errorf("ParseFlags() returned diff, (-want +got): %s", diff)
			}
		})
	}
}
