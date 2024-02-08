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

func TestJavacArgumentFilesPreprocessor(t *testing.T) {
	er, cleanup := execroot.Setup(t, []string{"MyClass1.java", "MyClass2.java", "MyClass3.java", "MyClass4.java", "MyClass5.java", "MyClass6.java", "a", "b", "c", "d", "e", "f", "g", "j", "k", "lib", "javac"})
	defer cleanup()
	// See Example here: https://docs.oracle.com/en/java/javase/17/docs/specs/man/javac.html#examples-of-using-javac-filename
	// 1) Use of the @<filename> to recursively interpret files is not supported.
	// 2) The arguments within a file can be separated by spaces or new line characters.
	// TODO(b/324585908): Support flag value with embedded spaces enclosed by double quotation marks.
	// 3) If a file name contains embedded spaces, then put the whole file name in double quotation marks.
	execroot.AddFileWithContent(t, filepath.Join(er, "options"), []byte("-bootclasspath a:b:c\n-classpath d:e:f\n-processorpath g\n--system=j\n-Aroom.schemaLocation=k\n-d out1/\n-s out2/"))
	execroot.AddFileWithContent(t, filepath.Join(er, "foo/bar/baz/classes"), []byte("MyClass1.java\nMyClass2.java\nMyClass3.java"))
	execroot.AddFileWithContent(t, filepath.Join(er, "classes_seperated_by_space"), []byte("MyClass4.java MyClass5.java\n\"MyClass6.java\""))
	execroot.AddFileWithContent(t, filepath.Join(er, "javac_remote_toolchain_inputs"), []byte("lib"))
	cmd := []string{"javac", "@options", "@foo/bar/baz/classes", "@classes_seperated_by_space"}
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
			Inputs: []string{
				"options",
				filepath.Join("foo", "bar", "baz", "classes"),
				"MyClass1.java",
				"MyClass2.java",
				"MyClass3.java",
				"classes_seperated_by_space",
				"MyClass4.java",
				"MyClass5.java",
				"MyClass6.java",
				"a", "b", "c", "d", "e", "f", "g", "j", "k",
				"lib",
				"javac"},
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
