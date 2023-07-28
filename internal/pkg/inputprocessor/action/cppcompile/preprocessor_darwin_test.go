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

//go:build darwin

package cppcompile

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/bazelbuild/reclient/internal/pkg/execroot"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/google/go-cmp/cmp"
)

func TestComputeSpecWithXCodeSDK(t *testing.T) {
	ctx := context.Background()
	s := &stubCPPDepScanner{
		res: []string{"include/foo.h"},
		err: nil,
	}
	c := &Preprocessor{CPPDepScanner: s, BasePreprocessor: &inputprocessor.BasePreprocessor{Ctx: ctx}}

	existingFiles := []string{
		filepath.Clean("bin/clang++"),
		filepath.Clean("src/test.cpp"),
		filepath.Clean("include/foo.h"),
		filepath.Clean("out/dummy"),
		filepath.Clean("sdk/SDKSettings.json"),
	}
	er, cleanup := execroot.Setup(t, existingFiles)
	defer cleanup()
	execroot.AddDirs(t, er, []string{
		"include/foo",
		"include/bar",
	})
	opts := inputprocessor.Options{
		Cmd:        []string{"../bin/clang++", "-o", "test.o", "-MF", "test.d", "-I../include/foo", "-I", "../include/bar", "-c", "../src/test.cpp", "-isysroot", "../sdk"},
		WorkingDir: "out",
		ExecRoot:   er,
		Labels:     map[string]string{"type": "compile", "compiler": "clang", "lang": "cpp"},
	}
	got, err := inputprocessor.Compute(c, opts)
	if err != nil {
		t.Errorf("Spec() failed: %v", err)
	}
	want := &command.InputSpec{
		Inputs: []string{
			filepath.Clean("include/foo.h"),
			filepath.Clean("src/test.cpp"),
			filepath.Clean("bin/clang++"),
			filepath.Clean("sdk/SDKSettings.json"),
		},
		VirtualInputs: []*command.VirtualInput{
			{Path: filepath.Clean("include/foo"), IsEmptyDirectory: true},
			{Path: filepath.Clean("include/bar"), IsEmptyDirectory: true},
			{Path: filepath.Clean("sdk"), IsEmptyDirectory: true},
		},
	}
	if diff := cmp.Diff(want, got.InputSpec, strSliceCmp); diff != "" {
		t.Errorf("Compute() returned diff (-want +got): %v", diff)
	}
}
