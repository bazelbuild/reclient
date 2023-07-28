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
	"testing"

	"github.com/bazelbuild/reclient/internal/pkg/execroot"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/flags"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestMetalavaParser(t *testing.T) {
	er, cleanup := execroot.Setup(t, nil)
	defer cleanup()
	test := []struct {
		name          string
		options       inputprocessor.Options
		existingFiles map[string][]byte
		want          *flags.CommandFlags
	}{
		{
			name: "basic",
			options: inputprocessor.Options{
				ExecRoot: er,
				Cmd: []string{
					"metalava",
					"-J-Xmx2048M",
					"@test.rsp",
					"@test2.rsp",
					"-bootclasspath",
					"e:f:g",
					"-classpath",
					"h,i",
					"--api-lint",
					"--api",
					"j",
					"--removed-api",
					"k",
					"--stubs",
					"l",
					"--convert-to-jdiff",
					"m",
					"n",
					"--convert-new-to-jdiff",
					"o",
					"p",
					"q",
					"--android-jar-pattern",
					"r/%/s",
					"--baseline",
					"t",
					"--strict-input-files:warn",
					"v",
					"-sourcepath",
					"u",
				},
			},
			existingFiles: map[string][]byte{
				"test.rsp":  []byte("a b"),
				"test2.rsp": []byte("c d"),
			},
			want: &flags.CommandFlags{
				ExecutablePath: "metalava",
				Flags: []*flags.Flag{
					&flags.Flag{Value: "-J-Xmx2048M"},
					&flags.Flag{Value: "--api-lint"},
				},
				TargetFilePaths:       []string{"test.rsp", "test2.rsp"},
				Dependencies:          []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "m", "o", "p", "r/", "t"},
				ExecRoot:              er,
				OutputFilePaths:       []string{"j", "k", "n", "q", "t", "v"},
				EmittedDependencyFile: "v",
				OutputDirPaths:        []string{"l"},
				VirtualDirectories:    []string{"u"},
			},
		}, {
			name: "strict input warn mode",
			options: inputprocessor.Options{
				ExecRoot: er,
				Cmd: []string{
					"metalava",
					"-J-Xmx2048M",
					"@test.rsp",
					"@test2.rsp",
					"-bootclasspath",
					"e:f:g",
					"-classpath",
					"h,i",
					"--api-lint",
					"--api",
					"j",
					"--removed-api",
					"k",
					"--stubs",
					"l",
					"--convert-to-jdiff",
					"m",
					"n",
					"--convert-new-to-jdiff",
					"o",
					"p",
					"q",
					"--android-jar-pattern",
					"r/%/s",
					"--baseline",
					"t",
					"--strict-input-files:warn",
					"v",
					"-sourcepath",
					"u",
				},
			},
			existingFiles: map[string][]byte{
				"test.rsp":  []byte("a b"),
				"test2.rsp": []byte("c d"),
			},
			want: &flags.CommandFlags{
				ExecutablePath: "metalava",
				Flags: []*flags.Flag{
					&flags.Flag{Value: "-J-Xmx2048M"},
					&flags.Flag{Value: "--api-lint"},
				},
				TargetFilePaths:       []string{"test.rsp", "test2.rsp"},
				Dependencies:          []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "m", "o", "p", "r/", "t"},
				ExecRoot:              er,
				OutputFilePaths:       []string{"j", "k", "n", "q", "t", "v"},
				EmittedDependencyFile: "v",
				OutputDirPaths:        []string{"l"},
				VirtualDirectories:    []string{"u"},
			},
		},
	}
	for _, test := range test {
		t.Run(test.name, func(t *testing.T) {
			execroot.AddFilesWithContent(t, er, test.existingFiles)
			p := &Preprocessor{&inputprocessor.BasePreprocessor{
				Options: test.options,
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
