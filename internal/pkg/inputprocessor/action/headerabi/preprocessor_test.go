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
	"os"
	"path/filepath"
	"testing"

	spb "github.com/bazelbuild/reclient/api/scandeps"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/action/cppcompile"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/google/go-cmp/cmp"
)

const executablePath = "abi-header-dumper"

func TestSpec(t *testing.T) {
	pwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Unable to get current working directory: %v", err)
	}
	s := &stubCPPDepScanner{res: []string{"foo.h"}}
	p := &Preprocessor{
		Preprocessor: &cppcompile.Preprocessor{
			BasePreprocessor: &inputprocessor.BasePreprocessor{Ctx: context.Background()},
			CPPDepScanner:    s,
		},
	}
	p.Options = inputprocessor.Options{
		ExecRoot: pwd,
		Cmd:      []string{executablePath, "--", "-Ifoo", "-I", "bar", "-o", "test.sdump", "test.cpp"},
	}

	if err := p.ParseFlags(); err != nil {
		t.Fatalf("ParseFlags() failed: %v", err)
	}
	p.ComputeSpec()
	got, err := p.Spec()
	if err != nil {
		t.Fatalf("Compute() failed: %v", err)
	}
	want := &command.InputSpec{
		Inputs: []string{
			"test.cpp",
			"foo.h",
		},
		VirtualInputs: []*command.VirtualInput{
			&command.VirtualInput{Path: filepath.Clean("foo"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("bar"), IsEmptyDirectory: true},
		},
	}
	opt := cmp.Transformer("filterOutExecutablePath", func(in []string) []string {
		out := []string{}
		for _, val := range in {
			if val != executablePath {
				out = append(out, val)
			}
		}
		return out
	})
	if diff := cmp.Diff(want, got.InputSpec, opt); diff != "" {
		t.Errorf("Spec() returned diff (-want +got): %v", diff)
	}
	wantCmd := []string{
		"-Ifoo",
		"-I",
		"bar",
		"-Qunused-arguments",
		"-o",
		"test.sdump",
		filepath.Join(pwd, "test.cpp"),
	}
	if diff := cmp.Diff(wantCmd, s.gotCmd[1:]); diff != "" {
		t.Errorf("CPP command from ABI header command %v had diff (-want +got): %s", p.Flags, diff)
	}
}

type stubCPPDepScanner struct {
	gotCmd       []string
	gotFileName  string
	gotDirectory string

	res []string
	err error
}

func (s *stubCPPDepScanner) ProcessInputs(_ context.Context, _ string, command []string, filename, directory string, _ []string) ([]string, bool, error) {
	s.gotCmd = command
	s.gotFileName = filename
	s.gotDirectory = directory

	return s.res, false, s.err
}

func (s *stubCPPDepScanner) ShouldIgnorePlugin(_ string) bool {
	return false
}

func (s *stubCPPDepScanner) Capabilities() *spb.CapabilitiesResponse {
	return nil
}
