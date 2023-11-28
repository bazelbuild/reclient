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
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/bazelbuild/reclient/internal/pkg/cppdependencyscanner"
	"github.com/bazelbuild/reclient/internal/pkg/execroot"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/depscache"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/flags"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/filemetadata"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var (
	strSliceCmp = cmpopts.SortSlices(func(a, b string) bool { return a < b })
)

func TestComputeSpec(t *testing.T) {
	ctx := context.Background()
	fmc := filemetadata.NewSingleFlightCache()
	s := &stubCPPDepScanner{
		res: []string{"include/foo.h"},
		err: nil,
	}
	c := &Preprocessor{CPPDepScanner: s, BasePreprocessor: &inputprocessor.BasePreprocessor{Ctx: ctx, FileMetadataCache: fmc}}

	// Tests that virtual inputs only include existing directories and excludes files.
	existingFiles := []string{
		filepath.Clean("bin/clang++"),
		filepath.Clean("src/test.cpp"),
		filepath.Clean("include/foo/a"),
		filepath.Clean("include/a/b.hmap"),
		filepath.Clean("out/dummy"),
	}
	er, cleanup := execroot.Setup(t, existingFiles)
	defer cleanup()
	inputs := []string{filepath.Clean("include/a/b.hmap")}
	opts := inputprocessor.Options{
		Cmd: []string{"../bin/clang++", "-o", "test.o", "-MF", "test.d", "-I../include/foo", "-I../include/bar", "-I../include/a/b.hmap",
			"-std=c++14", "-Xclang", "-verify", "-c", "../src/test.cpp"},
		WorkingDir: "out",
		ExecRoot:   er,
		Labels:     map[string]string{"type": "compile", "compiler": "clang", "lang": "cpp"},
		Inputs: &command.InputSpec{
			Inputs: inputs,
		},
	}
	got, err := inputprocessor.Compute(c, opts)
	if err != nil {
		t.Errorf("Spec() failed: %v", err)
	}
	want := &command.InputSpec{
		Inputs: []string{
			filepath.Clean("src/test.cpp"),
			filepath.Clean("bin/clang++"),
			filepath.Clean("include/a/b.hmap"),
		},
		VirtualInputs: []*command.VirtualInput{
			{Path: filepath.Clean("include/foo"), IsEmptyDirectory: true},
		},
	}
	if diff := cmp.Diff(want, got.InputSpec, strSliceCmp); diff != "" {
		t.Errorf("Compute() returned diff (-want +got): %v", diff)
	}
	wantCmd := []string{
		filepath.Join(er, "bin/clang++"),
		"-I../include/foo",
		"-I../include/bar",
		"-I../include/a/b.hmap",
		"-std=c++14", "-c", // expect -std=xx to not be normalized
		"-Qunused-arguments", // expect Qunused-arguments to be added, and -Xclang -verify to be removed
		"-o", "test.o",
		filepath.Join(er, "src/test.cpp"),
	}
	// expect command to be adjusted if Clang dependency scanner used
	if cppdependencyscanner.Type() == cppdependencyscanner.ClangScanDeps {
		wantCmd = append(wantCmd, "-o", "/dev/null", "-M", "-MT", "test.o", "-Xclang", "-Eonly", "-Xclang", "-sys-header-deps", "-Wno-error")
	}
	if diff := cmp.Diff(wantCmd, s.gotCmd); diff != "" {
		t.Errorf("Unexpected command passed to the dependency scanner (-want +got): %v", diff)
	}
}

func TestComputeSpecWithDepsCache(t *testing.T) {
	ctx := context.Background()
	fmc := filemetadata.NewSingleFlightCache()
	s := &stubCPPDepScanner{
		res: []string{"include/foo.h"},
		err: nil,
	}
	c := &Preprocessor{
		CPPDepScanner:    s,
		BasePreprocessor: &inputprocessor.BasePreprocessor{Ctx: ctx, FileMetadataCache: fmc},
		DepsCache:        depscache.New(filemetadata.NewSingleFlightCache()),
	}

	existingFiles := []string{
		filepath.Clean("bin/clang++"),
		filepath.Clean("src/test.cpp"),
		filepath.Clean("include/foo/a"),
		filepath.Clean("out/dummy"),
	}
	er, cleanup := execroot.Setup(t, existingFiles)
	defer cleanup()
	c.DepsCache.LoadFromDir(er)
	opts := inputprocessor.Options{
		Cmd:        []string{"../bin/clang++", "-o", "test.o", "-MF", "test.d", "-I../include/foo", "-I", "../include/bar", "-c", "../src/test.cpp"},
		WorkingDir: "out",
		ExecRoot:   er,
		Labels:     map[string]string{"type": "compile", "compiler": "clang", "lang": "cpp"},
	}
	var wg sync.WaitGroup
	wg.Add(1)
	c.testOnlySetDone = func() { wg.Done() }
	got, err := inputprocessor.Compute(c, opts)
	if err != nil {
		t.Errorf("Compute() failed: %v", err)
	}
	want := &command.InputSpec{
		Inputs: []string{
			filepath.Clean("src/test.cpp"),
			filepath.Clean("bin/clang++"),
		},
		VirtualInputs: []*command.VirtualInput{
			{Path: filepath.Clean("include/foo"), IsEmptyDirectory: true},
		},
	}
	if diff := cmp.Diff(want, got.InputSpec, strSliceCmp); diff != "" {
		t.Errorf("Compute() returned diff (-want +got): %v", diff)
	}
	wg.Wait()
	c = &Preprocessor{
		CPPDepScanner:    s,
		BasePreprocessor: &inputprocessor.BasePreprocessor{Ctx: ctx, FileMetadataCache: fmc},
		DepsCache:        c.DepsCache,
	}
	got, err = inputprocessor.Compute(c, opts)
	if err != nil {
		t.Errorf("Compute() failed: %v", err)
	}
	if diff := cmp.Diff(want, got.InputSpec, strSliceCmp); diff != "" {
		t.Errorf("Compute() returned diff (-want +got): %v", diff)
	}
	if s.processCalls != 1 {
		t.Errorf("Wrong number of ProcessInputs calls: got %v, want 1", s.processCalls)
	}
}

func TestComputeSpec_SysrootAndProfileSampleUseArgsConvertedToAbsolutePath(t *testing.T) {
	ctx := context.Background()
	fmc := filemetadata.NewSingleFlightCache()
	s := &stubCPPDepScanner{
		res: []string{"include/foo.h"},
		err: nil,
	}
	c := &Preprocessor{CPPDepScanner: s, BasePreprocessor: &inputprocessor.BasePreprocessor{Ctx: ctx, FileMetadataCache: fmc}}

	pwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Unable to get current working directory: %v", err)
	}
	c.Flags = &flags.CommandFlags{
		ExecutablePath:   "../bin/clang++",
		TargetFilePaths:  []string{"../src/test.cpp"},
		OutputFilePaths:  []string{"test.o"},
		ExecRoot:         pwd,
		WorkingDirectory: "out",
		Flags: []*flags.Flag{
			{Key: "-c"},
			{Key: "--sysroot", Value: "a/b", Joined: false},
			{Key: "-isysroot", Value: "../c/d", Joined: true},
			{Key: "--sysroot=", Value: fmt.Sprintf("%s/e/f", pwd), Joined: true},
			{Key: "-fprofile-sample-use=", Value: "../c/d/abc.prof", Joined: true},
			{Key: "-fsanitize-blacklist=", Value: fmt.Sprintf("%s/e/f/ignores.txt", pwd), Joined: true},
			{Key: "-fsanitize-ignorelist=", Value: fmt.Sprintf("%s/e/f/ignores2.txt", pwd), Joined: true},
			{Key: "-fprofile-list=", Value: fmt.Sprintf("%s/e/f/fun.list", pwd), Joined: true},
		},
	}
	if err := c.ComputeSpec(); err != nil {
		t.Fatalf("ComputeSpec() failed: %v", err)
	}

	wantCmd := []string{
		filepath.Join(pwd, "bin/clang++"),
		"-c",
		"--sysroot", filepath.Join(pwd, "out/a/b"),
		"-isysroot" + filepath.Join(pwd, "c/d"),
		"--sysroot=" + filepath.Join(pwd, "e/f"),
		"-fprofile-sample-use=" + filepath.Join(pwd, "c/d/abc.prof"),
		"-fsanitize-blacklist=" + filepath.Join(pwd, "e/f/ignores.txt"),
		"-fsanitize-ignorelist=" + filepath.Join(pwd, "e/f/ignores2.txt"),
		"-fprofile-list=" + filepath.Join(pwd, "e/f/fun.list"),
		"-Qunused-arguments",
		"-o",
		"test.o",
		filepath.Join(pwd, "src/test.cpp"),
	}
	if cppdependencyscanner.Type() == cppdependencyscanner.ClangScanDeps {
		wantCmd = append(wantCmd, []string{
			// adjusted
			"-o",
			"/dev/null",
			"-M",
			"-MT",
			"test.o",
			"-Xclang",
			"-Eonly",
			"-Xclang",
			"-sys-header-deps",
			"-Wno-error",
		}...)
	}
	if diff := cmp.Diff(wantCmd, s.gotCmd); diff != "" {
		t.Errorf("ComputeSpec() called clang-scan-deps incorrectly, diff (-want +got): %v", diff)
	}
}

func TestComputeSpecAbsolutePaths(t *testing.T) {
	ctx := context.Background()
	fmc := filemetadata.NewSingleFlightCache()
	s := &stubCPPDepScanner{
		res: []string{"include/foo.h"},
		err: nil,
	}
	c := &Preprocessor{CPPDepScanner: s, BasePreprocessor: &inputprocessor.BasePreprocessor{Ctx: ctx, FileMetadataCache: fmc}}

	pwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Unable to get current working directory: %v", err)
	}
	wd := "out"
	c.Flags = &flags.CommandFlags{
		ExecutablePath:   filepath.Join(pwd, "bin/clang++"),
		TargetFilePaths:  []string{filepath.Join(pwd, "src/test.cpp")},
		OutputFilePaths:  []string{filepath.Join(pwd, wd, "test.o")},
		ExecRoot:         pwd,
		WorkingDirectory: wd,
		Flags: []*flags.Flag{
			{Key: "-c"},
			{Key: "--sysroot", Value: filepath.Join(pwd, wd, "a/b"), Joined: false},
			{Key: "-isysroot", Value: filepath.Join(pwd, "c/d"), Joined: true},
			{Key: "--sysroot=", Value: filepath.Join(pwd, "e/f"), Joined: true},
			{Key: "-fprofile-sample-use=", Value: filepath.Join(pwd, "c/d/abc.prof"), Joined: true},
			{Key: "-fsanitize-blacklist=", Value: filepath.Join(pwd, "e/f/ignores.txt"), Joined: true},
			{Key: "-fsanitize-ignorelist=", Value: filepath.Join(pwd, "e/f/ignores2.txt"), Joined: true},
			{Key: "-fprofile-list=", Value: filepath.Join(pwd, "e/f/fun.list"), Joined: true},
		},
	}
	if err := c.ComputeSpec(); err != nil {
		t.Fatalf("ComputeSpec() failed: %v", err)
	}

	wantCmd := []string{
		filepath.Join(pwd, "bin/clang++"),
		"-c",
		"--sysroot", filepath.Join(pwd, "out/a/b"),
		"-isysroot" + filepath.Join(pwd, "c/d"),
		"--sysroot=" + filepath.Join(pwd, "e/f"),
		"-fprofile-sample-use=" + filepath.Join(pwd, "c/d/abc.prof"),
		"-fsanitize-blacklist=" + filepath.Join(pwd, "e/f/ignores.txt"),
		"-fsanitize-ignorelist=" + filepath.Join(pwd, "e/f/ignores2.txt"),
		"-fprofile-list=" + filepath.Join(pwd, "e/f/fun.list"),
		"-Qunused-arguments",
		"-o",
		filepath.Join(pwd, wd, "test.o"),
		filepath.Join(pwd, "src/test.cpp"),
	}
	if cppdependencyscanner.Type() == cppdependencyscanner.ClangScanDeps {
		wantCmd = append(wantCmd, []string{
			// adjusted
			"-o",
			"/dev/null",
			"-M",
			"-MT",
			filepath.Join(pwd, wd, "test.o"),
			"-Xclang",
			"-Eonly",
			"-Xclang",
			"-sys-header-deps",
			"-Wno-error",
		}...)
	}
	if diff := cmp.Diff(wantCmd, s.gotCmd); diff != "" {
		t.Errorf("ComputeSpec() called clang-scan-deps incorrectly, diff (-want +got): %v", diff)
	}
	if s.gotFileName != filepath.Join(pwd, "src/test.cpp") {
		t.Errorf("ComputeSpec() called the input processor incorrectly, want filename=%q, got %q", filepath.Join(pwd, "src/test.cpp"), s.gotFileName)
	}
}

func TestComputeSpec_RemovesUnsupportedFlags(t *testing.T) {
	ctx := context.Background()
	fmc := filemetadata.NewSingleFlightCache()
	s := &stubCPPDepScanner{
		res: []string{"include/foo.h"},
		err: nil,
	}
	c := &Preprocessor{CPPDepScanner: s, BasePreprocessor: &inputprocessor.BasePreprocessor{Ctx: ctx, FileMetadataCache: fmc}}

	// Tests that virtual inputs only include existing directories and excludes files.
	existingFiles := []string{
		filepath.Clean("bin/clang++"),
		filepath.Clean("src/test.cpp"),
		filepath.Clean("include/foo/a"),
		filepath.Clean("include/a/b.hmap"),
		filepath.Clean("out/dummy"),
	}
	er, cleanup := execroot.Setup(t, existingFiles)
	defer cleanup()
	inputs := []string{filepath.Clean("include/a/b.hmap")}
	opts := inputprocessor.Options{
		Cmd: []string{"../bin/clang++", "-o", "test.o", "-MF", "test.d", "-I../include/foo", "-I../include/bar", "-I../include/a/b.hmap",
			"-fno-experimental-new-pass-manager", "-fexperimental-new-pass-manager", "-std=c++14", "-Xclang", "-verify", "-c", "../src/test.cpp"},
		WorkingDir: "out",
		ExecRoot:   er,
		Labels:     map[string]string{"type": "compile", "compiler": "clang", "lang": "cpp"},
		Inputs: &command.InputSpec{
			Inputs: inputs,
		},
	}
	if _, err := inputprocessor.Compute(c, opts); err != nil {
		t.Errorf("Spec() failed: %v", err)
	}
	wantCmd := []string{
		filepath.Join(er, "bin/clang++"),
		"-I../include/foo",
		"-I../include/bar",
		"-I../include/a/b.hmap",
		"-std=c++14", "-c", // expect -std=xx to not be normalized
		"-Qunused-arguments", // expect Qunused-arguments to be added, and -Xclang -verify to be removed
		// -fno-experimental-new-pass-manager and -fexperimental-new-pass-manager are removed since
		// they're not supported in newer clang versions.
		"-o", "test.o",
		filepath.Join(er, "src/test.cpp"),
	}
	// expect command to be adjusted if Clang dependency scanner used
	if cppdependencyscanner.Type() == cppdependencyscanner.ClangScanDeps {
		wantCmd = append(wantCmd, "-o", "/dev/null", "-M", "-MT", "test.o", "-Xclang", "-Eonly", "-Xclang", "-sys-header-deps", "-Wno-error")
	}
	if diff := cmp.Diff(wantCmd, s.gotCmd); diff != "" {
		t.Errorf("Unexpected command passed to the dependency scanner (-want +got): %v", diff)
	}
}

func TestBuildCommandLine(t *testing.T) {
	tests := []struct {
		name           string
		flags          []*flags.Flag
		want           []string
		ignoredPlugins map[string]bool
	}{
		{
			name:           "verify, fallow-half-arguments-and-returns removed, and sysroot converted to absolute",
			flags:          []*flags.Flag{{Key: "-c"}, {Key: "-Xclang", Value: "-verify"}, {Key: "-Xclang", Value: "-fallow-half-arguments-and-returns"}, {Key: "--sysroot=", Value: "a/b", Joined: true}},
			want:           []string{filepath.Clean("/exec/root/foo/clang++"), "-c", "--sysroot=" + filepath.Clean("/exec/root/foo/a/b"), "-Qunused-arguments", "-o", "test.o", filepath.Clean("/exec/root/foo/test.cpp")},
			ignoredPlugins: map[string]bool{},
		},
		{
			name:           "plugin NOT in ingored list",
			flags:          []*flags.Flag{{Key: "-Xclang", Value: "-add-plugin"}, {Key: "-Xclang", Value: "blinkg-gc-plugin"}, {Key: "-c"}, {Key: "-Xclang", Value: "-verify"}},
			want:           []string{filepath.Clean("/exec/root/foo/clang++"), "-Xclang", "-add-plugin", "-Xclang", "blinkg-gc-plugin", "-c", "-Qunused-arguments", "-o", "test.o", filepath.Clean("/exec/root/foo/test.cpp")},
			ignoredPlugins: map[string]bool{"find-bad-constructs": true},
		},
		{
			name: "one of plugins in ingored list",
			flags: []*flags.Flag{{Key: "-Xclang", Value: "-add-plugin"}, {Key: "-Xclang", Value: "find-bad-constructs"},
				{Key: "-Xclang", Value: "-add-plugin"}, {Key: "-Xclang", Value: "blinkg-gc-plugin"},
				{Key: "-c"}, {Key: "-Xclang", Value: "-verify"}},
			want:           []string{filepath.Clean("/exec/root/foo/clang++"), "-Xclang", "-add-plugin", "-Xclang", "blinkg-gc-plugin", "-c", "-Qunused-arguments", "-o", "test.o", filepath.Clean("/exec/root/foo/test.cpp")},
			ignoredPlugins: map[string]bool{"find-bad-constructs": true},
		},
		{
			name:           "both plugins in ingored list",
			flags:          []*flags.Flag{{Key: "-Xclang", Value: "-add-plugin"}, {Key: "-Xclang", Value: "find-bad-constructs"}, {Key: "-c"}, {Key: "-Xclang", Value: "-verify"}},
			want:           []string{filepath.Clean("/exec/root/foo/clang++"), "-c", "-Qunused-arguments", "-o", "test.o", filepath.Clean("/exec/root/foo/test.cpp")},
			ignoredPlugins: map[string]bool{"find-bad-constructs": true, "blinkg-gc-plugin": true},
		},
		{
			name:           "plugin in ingored list, but preceded by incorrect flag",
			flags:          []*flags.Flag{{Key: "-Xclang", Value: "-unrecognized-flag"}, {Key: "-Xclang", Value: "find-bad-constructs"}, {Key: "-c"}, {Key: "-Xclang", Value: "-verify"}},
			want:           []string{filepath.Clean("/exec/root/foo/clang++"), "-Xclang", "-unrecognized-flag", "-Xclang", "find-bad-constructs", "-c", "-Qunused-arguments", "-o", "test.o", filepath.Clean("/exec/root/foo/test.cpp")},
			ignoredPlugins: map[string]bool{"find-bad-constructs": true},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &flags.CommandFlags{
				ExecutablePath:        "clang++",
				Flags:                 tc.flags,
				TargetFilePaths:       []string{"test.cpp"},
				OutputFilePaths:       []string{"test.o", "test.d"},
				EmittedDependencyFile: "test.d",
				WorkingDirectory:      "foo",
				ExecRoot:              "/exec/root",
			}
			p := &Preprocessor{
				BasePreprocessor: &inputprocessor.BasePreprocessor{Flags: f},
				CPPDepScanner: &stubCPPDepScanner{
					ignoredPluginsMap: tc.ignoredPlugins,
				},
			}
			got := p.BuildCommandLine("-o", false, map[string]bool{"--sysroot=": true})
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("BuildCommandLine returned diff, (-want +got): %s", diff)
			}
		})
	}
}

func TestVirtualInputs(t *testing.T) {
	pwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Unable to get current working directory: %v", err)
	}
	f := &flags.CommandFlags{
		ExecutablePath:   "../bin/clang++",
		TargetFilePaths:  []string{"../src/test.cpp"},
		OutputFilePaths:  []string{"test.o"},
		ExecRoot:         pwd,
		WorkingDirectory: "out",
		Flags: []*flags.Flag{
			{Value: "-c"},
			// These flags should result in virtual inputs.
			{Key: "--sysroot", Value: "a/b"},
			{Key: "-isysroot", Value: "../c/d", Joined: true},
			{Key: "--sysroot=", Value: fmt.Sprintf("%s/e/f", pwd), Joined: true},
			{Key: "-I", Value: "g/h", Joined: true},
			{Key: "-I", Value: "i/j", Joined: true},
			{Key: "-isysroot", Value: "../foo", Joined: true},
			{Key: "-isystem", Value: "../goo", Joined: true},
			// These flags should not result in virtual inputs.
			{Key: "-sysroot", Value: "../bar"},
			{Key: "-fprofile-sample-use=", Value: "../c/d/abc.prof", Joined: true},
			{Key: "-fsanitize-blacklist=", Value: fmt.Sprintf("%s/e/f/ignores.txt", pwd), Joined: true},
			{Key: "-fsanitize-ignorelist=", Value: fmt.Sprintf("%s/e/f/ignores.txt", pwd), Joined: true},
		},
	}
	vi := VirtualInputs(f, &Preprocessor{})

	want := []*command.VirtualInput{
		{Path: filepath.Clean("out/a/b"), IsEmptyDirectory: true},
		{Path: filepath.Clean("c/d"), IsEmptyDirectory: true},
		{Path: filepath.Clean("e/f"), IsEmptyDirectory: true},
		{Path: filepath.Clean("out/g/h"), IsEmptyDirectory: true},
		{Path: filepath.Clean("out/i/j"), IsEmptyDirectory: true},
		{Path: filepath.Clean("foo"), IsEmptyDirectory: true},
		{Path: filepath.Clean("goo"), IsEmptyDirectory: true},
	}
	if diff := cmp.Diff(want, vi); diff != "" {
		t.Errorf("virtualInputs(%+v) returned incorrect result diff (-want +got): %v", f, diff)
	}
}

// TestComputeSpecEventTimes tests that the Event Times "InputProcessorCacheLookup",
// "CPPInputProcessor", and "InputProcessorWait" are properly setup when ComputeSpec() is called.
// This was based on TestComputeSpecWithDepsCache and modified to check for Event Times.
func TestComputeSpecEventTimes(t *testing.T) {
	ctx := context.Background()
	fmc := filemetadata.NewSingleFlightCache()
	s := &stubCPPDepScanner{
		res:        []string{"include/foo.h"},
		err:        nil,
		cacheInput: false,
	}
	c := &Preprocessor{
		CPPDepScanner:    s,
		BasePreprocessor: &inputprocessor.BasePreprocessor{Ctx: ctx, FileMetadataCache: fmc},
		DepsCache:        depscache.New(filemetadata.NewSingleFlightCache()),
	}

	existingFiles := []string{
		filepath.Clean("bin/clang++"),
		filepath.Clean("src/test.cpp"),
		filepath.Clean("include/foo/a"),
		filepath.Clean("out/dummy"),
	}
	er, cleanup := execroot.Setup(t, existingFiles)
	defer cleanup()
	c.DepsCache.LoadFromDir(er)
	opts := inputprocessor.Options{
		Cmd:        []string{"../bin/clang++", "-o", "test.o", "-MF", "test.d", "-I../include/foo", "-I", "../include/bar", "-c", "../src/test.cpp"},
		WorkingDir: "out",
		ExecRoot:   er,
		Labels:     map[string]string{"type": "compile", "compiler": "clang", "lang": "cpp"},
	}

	// No deps cache hit
	var wg sync.WaitGroup
	wg.Add(1)
	c.testOnlySetDone = func() { wg.Done() }
	inputprocessor.Compute(c, opts)
	wg.Wait()
	if _, okIPCL := c.Rec.GetLocalMetadata().GetEventTimes()["InputProcessorCacheLookup"]; okIPCL {
		t.Errorf("Event Time %s was set but DepsCache was not used", "InputProcessorCacheLookup")
	}
	if _, okIPW := c.Rec.GetLocalMetadata().GetEventTimes()["InputProcessorWait"]; !okIPW {
		t.Errorf("Event Time %s was not set correctly", "InputProcessorWait")
	}
	if _, okCIP := c.Rec.GetLocalMetadata().GetEventTimes()["CPPInputProcessor"]; !okCIP {
		t.Errorf("Event Time %s was not set but DepsCache was not used", "CPPInputProcessor")
	}
	// Cache used but no early deps cache hit
	s.cacheInput = true
	c = &Preprocessor{
		CPPDepScanner:    s,
		BasePreprocessor: &inputprocessor.BasePreprocessor{Ctx: ctx, FileMetadataCache: fmc},
		DepsCache:        depscache.New(filemetadata.NewSingleFlightCache()),
	}
	c.DepsCache.LoadFromDir(er)
	wg.Add(1)
	c.testOnlySetDone = func() { wg.Done() }
	inputprocessor.Compute(c, opts)
	wg.Wait()
	if _, okIPCL := c.Rec.GetLocalMetadata().GetEventTimes()["InputProcessorCacheLookup"]; !okIPCL {
		t.Errorf("Event Time %s was not set but DepsCache was used", "InputProcessorCacheLookup")
	}
	if _, okIPW := c.Rec.GetLocalMetadata().GetEventTimes()["InputProcessorWait"]; !okIPW {
		t.Errorf("Event Time %s was not set correctly", "InputProcessorWait")
	}
	if _, okCIP := c.Rec.GetLocalMetadata().GetEventTimes()["CPPInputProcessor"]; okCIP {
		t.Errorf("Event Time %s was set but DepsCache was used", "CPPInputProcessor")
	}
	// Early deps cache hit
	s.cacheInput = false
	c = &Preprocessor{
		CPPDepScanner:    s,
		BasePreprocessor: &inputprocessor.BasePreprocessor{Ctx: ctx, FileMetadataCache: fmc},
		DepsCache:        c.DepsCache,
	}
	inputprocessor.Compute(c, opts)
	if _, okIPCL := c.Rec.GetLocalMetadata().GetEventTimes()["InputProcessorCacheLookup"]; !okIPCL {
		t.Errorf("Event Time for %s was not set but DepsCache was used", "InputProcessorCacheLookup")
	}
	if _, okIPW := c.Rec.GetLocalMetadata().GetEventTimes()["InputProcessorWait"]; okIPW {
		t.Errorf("Event Time %s was set but an early Deps Cache hit was found", "InputProcessorWait")
	}
	if _, okCIP := c.Rec.GetLocalMetadata().GetEventTimes()["CPPInputProcessor"]; okCIP {
		t.Errorf("Event Time %s was set but an early Deps Cache hit was found", "CPPInputProcessor")
	}
}

type stubCPPDepScanner struct {
	gotCmd       []string
	gotFileName  string
	gotDirectory string

	res []string
	err error

	processCalls  int
	minimizeCalls int

	ignoredPluginsMap map[string]bool

	cacheInput bool
}

func (s *stubCPPDepScanner) ProcessInputs(_ context.Context, _ string, command []string, filename, directory string, _ []string) ([]string, bool, error) {
	s.gotCmd = command
	s.gotFileName = filename
	s.gotDirectory = directory
	s.processCalls++

	return s.res, s.cacheInput, s.err
}

func (s *stubCPPDepScanner) ShouldIgnorePlugin(plugin string) bool {
	_, present := s.ignoredPluginsMap[plugin]
	return present
}
