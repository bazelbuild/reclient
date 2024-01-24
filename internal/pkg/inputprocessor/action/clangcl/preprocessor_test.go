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

package clangcl

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	spb "github.com/bazelbuild/reclient/api/scandeps"
	"github.com/bazelbuild/reclient/internal/pkg/execroot"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/action/cppcompile"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/google/go-cmp/cmp"
)

const executablePath = "clang-cl"

type spyExecutor struct {
	gotCmd         *command.Command
	stdout, stderr string
	err            error
}

func (e *spyExecutor) Execute(ctx context.Context, cmd *command.Command) (string, string, error) {
	e.gotCmd = cmd
	return e.stdout, e.stderr, e.err
}

func TestWinSdkDir(t *testing.T) {
	tests := []struct {
		name        string
		dirs        []string
		expectedDir string
		expectedErr bool
	}{
		{
			name: "multiple versions",
			dirs: []string{
				filepath.Join("Windows Kits", "10", "Include", "10.0.20348.0"),
				filepath.Join("Windows Kits", "10", "Include", "10.0.20348.1"),
				filepath.Join("Windows Kits", "5", "Include", "10.0.20348.0"),
				filepath.Join("Windows Kits", "5", "Include", "9.0.20348.0"),
			},
			expectedDir: filepath.Join("Windows Kits", "10", "Include", "10.0.20348.1"),
		},
		{
			name:        "missing dir",
			expectedErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			execRoot := t.TempDir()
			for _, dir := range test.dirs {
				os.MkdirAll(filepath.Join(execRoot, dir), 0755)
			}
			got, err := winSDKDir(execRoot)
			if err != nil && !test.expectedErr {
				t.Errorf("Got error = %v, expected none", err)
			}
			if err == nil && test.expectedErr {
				t.Errorf("Expected error, got none")
			}
			got, _ = filepath.Rel(execRoot, got)
			if got != test.expectedDir {
				t.Errorf("Expected dir = %q, got = %q", test.expectedDir, got)
			}
		})
	}
}

func TestResourceDir(t *testing.T) {
	ctx := context.Background()
	execRoot := t.TempDir()

	var clangCL, want, newline string
	if runtime.GOOS == "windows" {
		clangCL = filepath.Join(execRoot, `third_party\llvm-build\Release+Asserts\bin\clang-cl.exe`)
		want = filepath.Join(execRoot, `third_party\llvm-build\Release+Asserts\lib\clang\12.0.0`)
		newline = "\r\n"
	} else {
		clangCL = filepath.Join(execRoot, "third_party/llvm-build/Release+Asserts/bin/clang-cl")
		want = filepath.Join(execRoot, "third_party/llvm-build/Release+Asserts/lib/clang/12.0.0")
		newline = "\n"
	}
	err := os.MkdirAll(filepath.Dir(clangCL), 0755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(clangCL, nil, 0755)
	if err != nil {
		t.Fatal(err)
	}

	e := &spyExecutor{
		stdout: `clang version 12.0.0 (https://github.com/llvm/llvm-project/ f086e85eea94a51eb42115496ac5d24f07bc8791)` + newline +
			`Target: x86_64-pc-windows-msvc` + newline +
			`Thread model: posix` + newline +
			`InstalledDir: ` + filepath.Dir(clangCL) + newline,
	}
	p := &Preprocessor{
		Preprocessor: &cppcompile.Preprocessor{
			BasePreprocessor: &inputprocessor.BasePreprocessor{
				Ctx:      ctx,
				Executor: e,
			},
		},
	}

	got := p.resourceDir([]string{clangCL, "/showIncludes:user", "/c", "../../base/foo.cc", "/Foobj/base/base/foo.obj"})
	if got != want {
		t.Errorf("p.resourceDir([]string{%q, ..})=%q; want=%q", clangCL, got, want)
	}
	wantCmd := &command.Command{
		Args:       []string{clangCL, "--version"},
		WorkingDir: "/",
	}
	if !cmp.Equal(e.gotCmd, wantCmd) {
		t.Errorf("executor got=%v; want=%v", e.gotCmd, wantCmd)
	}
}

func TestComputeSpec(t *testing.T) {
	tests := []struct {
		name                 string
		scandepsCapabilities *spb.CapabilitiesResponse
		versionOutput        string
		inputExtraCmd        []string
		wantResDir           string
		wantResDirRelToEr    bool
	}{
		{
			name:                 "ResourceDirProvided/ExpectsResourceDir",
			scandepsCapabilities: &spb.CapabilitiesResponse{ExpectsResourceDir: true},
			versionOutput:        "clang version 16.0.6 (16)",
			inputExtraCmd:        []string{"-resource-dir", "/some/dir"},
			wantResDir:           "/some/dir",
		},
		{
			name:                 "ResourceDirProvided/DoesntExpectResourceDir",
			scandepsCapabilities: &spb.CapabilitiesResponse{ExpectsResourceDir: false},
			versionOutput:        "clang version 16.0.6 (16)",
			inputExtraCmd:        []string{"-resource-dir", "/some/dir"},
			wantResDir:           "/some/dir",
		},
		{
			name:                 "ResourceDirMissing/ExpectsResourceDir",
			scandepsCapabilities: &spb.CapabilitiesResponse{ExpectsResourceDir: true},
			versionOutput:        "clang version 16.0.6 (16)",
			wantResDir:           "lib/clang/16.0.6",
			wantResDirRelToEr:    true,
		},
		{
			name:                 "ResourceDirMissing/DoesntExpectResourceDir",
			scandepsCapabilities: &spb.CapabilitiesResponse{ExpectsResourceDir: false},
			versionOutput:        "clang version 16.0.6 (16)",
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s := &stubCPPDepScanner{
				res:          []string{"foo.h"},
				err:          nil,
				capabilities: tc.scandepsCapabilities,
			}

			// Tests that virtual inputs only include existing directories and excludes files.
			existingFiles := []string{
				filepath.Clean("bin/clang-cl"),
				filepath.Clean("out/dummy"),
			}
			er, cleanup := execroot.Setup(t, existingFiles)
			t.Cleanup(cleanup)
			os.MkdirAll(filepath.Join(er, "Windows Kits", "10", "Include", "10.0.20348.0"), 0755)
			os.MkdirAll(filepath.Join(er, "VC", "Tools", "MSVC", "14.29.30133"), 0755)
			p := Preprocessor{
				Preprocessor: &cppcompile.Preprocessor{
					BasePreprocessor: &inputprocessor.BasePreprocessor{
						Ctx:      ctx,
						Executor: &stubExecutor{outStr: tc.versionOutput},
					},
					CPPDepScanner: s,
				},
			}
			p.Options = inputprocessor.Options{
				ExecRoot:   er,
				WorkingDir: "out",
				Cmd: append(
					[]string{filepath.Join(er, "bin/clang-cl"),
						"-header-filter=\"(packages)\"",
						"-extra-arg-before=-Xclang",
						"test.cpp",
						"--",
						// These flags should result in virtual inputs.
						"/I", "a/b",
						"/I../c/d",
						"-I", "g/h",
						"-Ii/j",
						"-imsvc", "../foo",
						"/winsysroot", er,
						// These flags should not result in virtual inputs.
						"-sysroot../bar",
						"--sysroot../baz",
						"-fprofile-sample-use=../c/d/abc.prof",
						// -Xclang -verify should be removed from output
						"-Xclang",
						"-verify",
						"-c",
						"/Fotest.o",
						"-o", "test.d",
					}, tc.inputExtraCmd...),
			}
			if err := p.ParseFlags(); err != nil {
				t.Fatalf("ParseFlags() failed: %v", err)
			}
			if err := p.ComputeSpec(); err != nil {
				t.Fatalf("ComputeSpec() failed: %v", err)
			}
			spec, _ := p.Spec()
			// expect files specified both by -o and /Fo
			if diff := cmp.Diff(spec.OutputFiles, []string{filepath.Join("out", "test.o"), filepath.Join("out", "test.d")}); diff != "" {
				t.Errorf("OutputFiles has diff (-want +got): %s", diff)
			}

			wantVirtualOutputs := []*command.VirtualInput{
				{Path: filepath.Clean(filepath.Join("out", "a/b")), IsEmptyDirectory: true},
				{Path: filepath.Clean("c/d"), IsEmptyDirectory: true},
				{Path: filepath.Clean(filepath.Join("out", "g/h")), IsEmptyDirectory: true},
				{Path: filepath.Clean(filepath.Join("out", "i/j")), IsEmptyDirectory: true},
				{Path: filepath.Clean("foo"), IsEmptyDirectory: true},
				{Path: filepath.Clean(filepath.Join("Windows Kits", "10", "Include", "10.0.20348.0")), IsEmptyDirectory: true},
				{Path: filepath.Clean(filepath.Join("VC", "Tools", "MSVC", "14.29.30133")), IsEmptyDirectory: true},
			}
			if diff := cmp.Diff(wantVirtualOutputs, spec.InputSpec.VirtualInputs); diff != "" {
				t.Errorf("InputSpec.VirtualInputs had diff (-want +got): %v", diff)
			}

			wantCmd := []string{
				filepath.Join(er, "bin/clang-cl"),
				"-header-filter=\"(packages)\"",
				"-extra-arg-before=-Xclang",
				"--",
				"/I", "a/b",
				"/I../c/d",
				"-I", "g/h",
				"-Ii/j",
				"-imsvc", "../foo",
				"/winsysroot", er,
				"-sysroot../bar",
				"--sysroot../baz",
				"-fprofile-sample-use=../c/d/abc.prof",
				"-c",
				"-Qunused-arguments",
				"/Fotest.o",
				"/Fotest.d", // -o normalized to /Fo
				filepath.Join(er, "out", "test.cpp"),
			}
			gotCmdNoResDir := make([]string, 0, len(s.gotCmd))
			i := 0
			gotResDir := ""
			for i < len(s.gotCmd) {
				if s.gotCmd[i] == "-resource-dir" {
					i++
					gotResDir = s.gotCmd[i]
				} else {
					gotCmdNoResDir = append(gotCmdNoResDir, s.gotCmd[i])
				}
				i++
			}
			if diff := cmp.Diff(wantCmd, gotCmdNoResDir); diff != "" {
				t.Errorf("CPP command from %v command %v had diff (-want +got): %s", executablePath, p.Flags, diff)
			}
			wantResDir := tc.wantResDir
			if wantResDir != "" && tc.wantResDirRelToEr {
				wantResDir = filepath.Join(er, wantResDir)
			}
			if wantResDir != gotResDir {
				t.Errorf("CPP command had incorrect resource dir, wanted %v, got %v", wantResDir, gotResDir)
			}
		})
	}
}

type stubCPPDepScanner struct {
	gotCmd       []string
	gotFileName  string
	gotDirectory string

	res []string
	err error

	capabilities *spb.CapabilitiesResponse

	processCalls  int
	minimizeCalls int
}

func (s *stubCPPDepScanner) ProcessInputs(_ context.Context, _ string, command []string, filename, directory string, _ []string) ([]string, bool, error) {
	s.gotCmd = command
	s.gotFileName = filename
	s.gotDirectory = directory
	s.processCalls++

	return s.res, false, s.err
}

func (s *stubCPPDepScanner) ShouldIgnorePlugin(_ string) bool {
	return false
}

func (s *stubCPPDepScanner) Capabilities() *spb.CapabilitiesResponse {
	return s.capabilities
}

type stubExecutor struct {
	gotCmd *command.Command

	outStr string
	errStr string
	err    error
}

func (s *stubExecutor) Execute(ctx context.Context, cmd *command.Command) (string, string, error) {
	s.gotCmd = cmd
	return s.outStr, s.errStr, s.err
}
