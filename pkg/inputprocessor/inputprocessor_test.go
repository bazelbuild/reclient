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

package inputprocessor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	lpb "team/foundry-x/re-client/api/log"
	ppb "team/foundry-x/re-client/api/proxy"
	"team/foundry-x/re-client/internal/pkg/cppdependencyscanner"
	"team/foundry-x/re-client/internal/pkg/execroot"
	"team/foundry-x/re-client/internal/pkg/features"
	"team/foundry-x/re-client/internal/pkg/labels"
	"team/foundry-x/re-client/internal/pkg/localresources"
	"team/foundry-x/re-client/internal/pkg/logger"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/filemetadata"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/outerr"
	"github.com/bazelbuild/rules_go/go/tools/bazel"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	fakeExecutionID = "fake-execution-1"
	wd              = "wd"

	androidCommandFilePath = "testdata/sample_android_command.txt"
)

var (
	strSliceCmp = cmpopts.SortSlices(func(a, b string) bool { return a < b })
	dsTimeout   = 10 * time.Second
)

func TestCpp(t *testing.T) {
	ctx := context.Background()
	ds := &stubCPPDependencyScanner{
		processInputsReturnValue: []string{
			"wd/ISoundTriggerClient.cpp",
			"wd/frameworks/av/soundtrigger/ISoundTriggerClient.cpp",
		},
	}
	resMgr := localresources.NewDefaultManager()
	ip := newInputProcessor(ds, dsTimeout, false, nil, resMgr, nil, nil)
	existingFiles := []string{
		filepath.Clean("wd/libc++.so.1"),
		filepath.Clean("wd/ISoundTriggerClient.cpp"),
		filepath.Clean("wd/frameworks/av/soundtrigger/ISoundTriggerClient.cpp"),
	}
	er, cleanup := execroot.Setup(t, append(existingFiles, filepath.Join(wd, "remote_toolchain_inputs")))
	defer cleanup()

	cmd := []string{"clang++", "-c", "-o", "test.o", "-MF", "test.d", "test.cpp"}
	lbls := map[string]string{
		"type":     "compile",
		"compiler": "clang",
		"lang":     "cpp",
		"shallow":  "false",
	}
	i := &command.InputSpec{Inputs: []string{filepath.Clean("wd/libc++.so.1")}}
	opts := &ProcessInputsOptions{
		ExecutionID: fakeExecutionID,
		Cmd:         cmd,
		WorkingDir:  wd,
		ExecRoot:    er,
		Inputs:      i,
		Labels:      lbls,
	}
	gotIO, err := ip.ProcessInputs(ctx, opts, &logger.LogRecord{LogRecord: &lpb.LogRecord{}})
	if err != nil {
		t.Errorf("ProcessInputs(%+v).err = %v, want no error.", opts, err)
	}
	wantIO := &CommandIO{
		InputSpec:             &command.InputSpec{Inputs: existingFiles},
		OutputFiles:           []string{filepath.Clean("wd/test.d"), filepath.Clean("wd/test.o")},
		EmittedDependencyFile: filepath.Clean("wd/test.d"),
	}
	if diff := cmp.Diff(wantIO, gotIO, strSliceCmp); diff != "" {
		t.Errorf("ProcessInputs(%v) returned diff in CommandIO, (-want +got): %s", opts, diff)
	}
}

// TestCppEventTimes tests that the EventTime "ProcessInputs" is properly set when creating an input processor.
// This was based on TestCpp and modified to test for EventTimes.
func TestCppEventTimes(t *testing.T) {
	ctx := context.Background()
	ds := &stubCPPDependencyScanner{
		processInputsReturnValue: []string{
			"wd/ISoundTriggerClient.cpp",
			"wd/frameworks/av/soundtrigger/ISoundTriggerClient.cpp",
		},
	}
	resMgr := localresources.NewDefaultManager()
	ip := newInputProcessor(ds, dsTimeout, false, nil, resMgr, nil, nil)
	existingFiles := []string{
		filepath.Clean("wd/libc++.so.1"),
		filepath.Clean("wd/ISoundTriggerClient.cpp"),
		filepath.Clean("wd/frameworks/av/soundtrigger/ISoundTriggerClient.cpp"),
	}
	er, cleanup := execroot.Setup(t, append(existingFiles, filepath.Join(wd, "remote_toolchain_inputs")))
	defer cleanup()

	cmd := []string{"clang++", "-c", "-o", "test.o", "-MF", "test.d", "test.cpp"}
	lbls := map[string]string{
		"type":     "compile",
		"compiler": "clang",
		"lang":     "cpp",
		"shallow":  "false",
	}
	i := &command.InputSpec{Inputs: []string{filepath.Clean("wd/libc++.so.1")}}
	opts := &ProcessInputsOptions{
		ExecutionID: fakeExecutionID,
		Cmd:         cmd,
		WorkingDir:  wd,
		ExecRoot:    er,
		Inputs:      i,
		Labels:      lbls,
	}
	rec := &logger.LogRecord{LogRecord: &lpb.LogRecord{}}
	ip.ProcessInputs(ctx, opts, rec)
	if _, ok := rec.GetLocalMetadata().GetEventTimes()["ProcessInputs"]; !ok {
		t.Errorf("Event Time for %s was not set correctly", "ProcessInputs")
	}
}

// TestCppWithDepsCache tests creating an input processor with a deps cache. It does not test the
// correctness of the deps cache as this is already covered by unit tests of the depscache package.
func TestCppWithDepsCache(t *testing.T) {
	ctx := context.Background()
	if cppdependencyscanner.Type() == cppdependencyscanner.Goma {
		// The goma input processor natively supports deps cache so the reproxy
		// deps cache will not be initialized in this case.
		return
	}
	ds := &stubCPPDependencyScanner{
		processInputsReturnValue: []string{
			"wd/ISoundTriggerClient.cpp",
			"wd/frameworks/av/soundtrigger/ISoundTriggerClient.cpp",
		},
		ignoredPluginsMap: map[string]bool{"foo": true},
	}
	existingFiles := []string{
		filepath.Clean("wd/libc++.so.1"),
		filepath.Clean("wd/ISoundTriggerClient.cpp"),
		filepath.Clean("wd/frameworks/av/soundtrigger/ISoundTriggerClient.cpp"),
	}
	er, cleanup := execroot.Setup(t, append(existingFiles, filepath.Join(wd, "remote_toolchain_inputs")))
	defer cleanup()
	resMgr := localresources.NewDefaultManager()
	fmc := filemetadata.NewSingleFlightCache()
	ip := newInputProcessor(ds, dsTimeout, false, nil, resMgr, fmc, nil)
	ip.depsCache, cleanup = newDepsCache(fmc, er, nil)

	cmd := []string{"clang++", "-c", "-o", "test.o",
		"-Xclang", "-add-plugin", "-Xclang", "foo", // should be stripped because it's included in clangDepsScanIgnoredPlugins
		"-Xclang", "-add-plugin", "-Xclang", "bar", // should be present in arguments sent to the dependency scanner
		"-MF", "test.d", "test.cpp"}
	lbls := map[string]string{
		"type":     "compile",
		"compiler": "clang",
		"lang":     "cpp",
		"shallow":  "false",
	}
	i := &command.InputSpec{Inputs: []string{filepath.Clean("wd/libc++.so.1")}}
	opts := &ProcessInputsOptions{
		ExecutionID: fakeExecutionID,
		Cmd:         cmd,
		WorkingDir:  wd,
		ExecRoot:    er,
		Inputs:      i,
		Labels:      lbls,
	}
	gotIO, err := ip.ProcessInputs(ctx, opts, &logger.LogRecord{LogRecord: &lpb.LogRecord{}})
	if err != nil {
		t.Errorf("ProcessInputs(%+v).err = %v, want no error.", opts, err)
	}
	wantIO := &CommandIO{
		InputSpec:             &command.InputSpec{Inputs: existingFiles},
		OutputFiles:           []string{filepath.Clean("wd/test.d"), filepath.Clean("wd/test.o")},
		EmittedDependencyFile: filepath.Clean("wd/test.d"),
	}
	if diff := cmp.Diff(wantIO, gotIO, strSliceCmp); diff != "" {
		t.Errorf("ProcessInputs(%v) returned diff in CommandIO, (-want +got): %s", opts, diff)
	}
	cleanup()
	dcPath := filepath.Join(er, "reproxy.cache")
	if _, err := os.Stat(dcPath); os.IsNotExist(err) {
		t.Errorf("Deps cache file (%v) does not exist", dcPath)
	}
	wantDepScanArgs := []string{"-c", "-Xclang", "-add-plugin", "-Xclang", "bar", "-Qunused-arguments", "-o", "test.o", filepath.Join(er, wd, "test.cpp"),
		"-o", "/dev/null", "-M", "-MT", "test.o", "-Xclang", "-Eonly", "-Xclang", "-sys-header-deps", "-Wno-error"}
	if diff := cmp.Diff(wantDepScanArgs, ds.processInputsArgs[1:]); diff != "" {
		t.Errorf("CPPDepScanner called with unexpected arguments (-want +got): %s", diff)
	}
}

func TestCppShallowFallback(t *testing.T) {
	ctx := context.Background()
	ds := &stubCPPDependencyScanner{
		processInputsError: errors.New("failed to call clang-scan-deps"),
	}
	resMgr := localresources.NewDefaultManager()
	ip := newInputProcessor(ds, dsTimeout, false, nil, resMgr, nil, nil)
	existingFiles := []string{
		filepath.Clean("wd/libc++.so.1"),
		filepath.Clean("wd/test.cpp"),
	}
	er, cleanup := execroot.Setup(t, append(existingFiles, filepath.Join(wd, "remote_toolchain_inputs")))
	defer cleanup()

	cmd := []string{"clang++", "-c", "-o", "test.o", "-MF", "test.d", "test.cpp"}
	lbls := map[string]string{
		"type":     "compile",
		"compiler": "clang",
		"lang":     "cpp",
		"shallow":  "false",
	}
	i := &command.InputSpec{Inputs: []string{filepath.Clean("wd/libc++.so.1")}}
	opts := &ProcessInputsOptions{
		ExecutionID: fakeExecutionID,
		Cmd:         cmd,
		WorkingDir:  wd,
		ExecRoot:    er,
		Inputs:      i,
		Labels:      lbls,
	}
	gotIO, err := ip.ProcessInputs(ctx, opts, &logger.LogRecord{LogRecord: &lpb.LogRecord{}})
	if err != nil {
		t.Errorf("ProcessInputs(%+v).err = %v, want no error.", opts, err)
	}
	wantIO := &CommandIO{
		InputSpec:             &command.InputSpec{Inputs: existingFiles},
		OutputFiles:           []string{filepath.Clean("wd/test.d"), filepath.Clean("wd/test.o")},
		EmittedDependencyFile: filepath.Clean("wd/test.d"),
		UsedShallowMode:       true,
	}
	if diff := cmp.Diff(wantIO, gotIO, strSliceCmp); diff != "" {
		t.Errorf("ProcessInputs(%v) returned diff in CommandIO, (-want +got): %s", opts, diff)
	}
}

func TestNoCppShallowFallbackWithRemote(t *testing.T) {
	ctx := context.Background()
	ds := &stubCPPDependencyScanner{
		processInputsError: errors.New("failed to call clang-scan-deps"),
	}
	resMgr := localresources.NewDefaultManager()
	ip := newInputProcessor(ds, dsTimeout, false, nil, resMgr, nil, nil)
	existingFiles := []string{
		filepath.Clean("wd/libc++.so.1"),
		filepath.Clean("wd/test.cpp"),
	}
	er, cleanup := execroot.Setup(t, append(existingFiles, filepath.Join(wd, "remote_toolchain_inputs")))
	defer cleanup()

	cmd := []string{"clang++", "-c", "-o", "test.o", "-MF", "test.d", "test.cpp"}
	lbls := map[string]string{
		"type":     "compile",
		"compiler": "clang",
		"lang":     "cpp",
	}
	i := &command.InputSpec{Inputs: []string{filepath.Clean("wd/libc++.so.1")}}
	opts := &ProcessInputsOptions{
		ExecutionID:  fakeExecutionID,
		Cmd:          cmd,
		WorkingDir:   wd,
		ExecRoot:     er,
		Inputs:       i,
		Labels:       lbls,
		ExecStrategy: ppb.ExecutionStrategy_REMOTE,
	}
	if _, err := ip.ProcessInputs(ctx, opts, &logger.LogRecord{LogRecord: &lpb.LogRecord{}}); err == nil {
		t.Errorf("ProcessInputs(%+v) did shallow fall back when remote CPP compile specified", opts)
	}
}

func TestNoCppShallowFallbackWithRemoteLocalFallback(t *testing.T) {
	ctx := context.Background()
	ds := &stubCPPDependencyScanner{
		processInputsError: errors.New("failed to call clang-scan-deps"),
	}
	resMgr := localresources.NewDefaultManager()
	ip := newInputProcessor(ds, dsTimeout, false, nil, resMgr, nil, nil)
	existingFiles := []string{
		filepath.Clean("wd/libc++.so.1"),
		filepath.Clean("wd/test.cpp"),
	}
	er, cleanup := execroot.Setup(t, append(existingFiles, filepath.Join(wd, "remote_toolchain_inputs")))
	defer cleanup()

	cmd := []string{"clang++", "-c", "-o", "test.o", "-MF", "test.d", "test.cpp"}
	lbls := map[string]string{
		"type":     "compile",
		"compiler": "clang",
		"lang":     "cpp",
	}
	i := &command.InputSpec{Inputs: []string{filepath.Clean("wd/libc++.so.1")}}
	opts := &ProcessInputsOptions{
		ExecutionID:  fakeExecutionID,
		Cmd:          cmd,
		WorkingDir:   wd,
		ExecRoot:     er,
		Inputs:       i,
		Labels:       lbls,
		ExecStrategy: ppb.ExecutionStrategy_REMOTE_LOCAL_FALLBACK,
	}
	if _, err := ip.ProcessInputs(ctx, opts, &logger.LogRecord{LogRecord: &lpb.LogRecord{}}); err == nil {
		t.Errorf("ProcessInputs(%+v) did shallow fall back when remote CPP compile specified", opts)
	}
}

func TestCppShallowFallbackWithLocal(t *testing.T) {
	ctx := context.Background()
	ds := &stubCPPDependencyScanner{
		processInputsError: errors.New("failed to call clang-scan-deps"),
	}
	resMgr := localresources.NewDefaultManager()
	ip := newInputProcessor(ds, dsTimeout, false, nil, resMgr, nil, nil)
	existingFiles := []string{
		filepath.Clean("wd/libc++.so.1"),
		filepath.Clean("wd/test.cpp"),
	}
	er, cleanup := execroot.Setup(t, append(existingFiles, filepath.Join(wd, "remote_toolchain_inputs")))
	defer cleanup()

	cmd := []string{"clang++", "-c", "-o", "test.o", "-MF", "test.d", "test.cpp"}
	lbls := map[string]string{
		"type":     "compile",
		"compiler": "clang",
		"lang":     "cpp",
	}
	i := &command.InputSpec{Inputs: []string{filepath.Clean("wd/libc++.so.1")}}
	opts := &ProcessInputsOptions{
		ExecutionID:  fakeExecutionID,
		Cmd:          cmd,
		WorkingDir:   wd,
		ExecRoot:     er,
		Inputs:       i,
		Labels:       lbls,
		ExecStrategy: ppb.ExecutionStrategy_LOCAL,
	}
	gotIO, err := ip.ProcessInputs(ctx, opts, &logger.LogRecord{LogRecord: &lpb.LogRecord{}})
	if err != nil {
		t.Errorf("ProcessInputs(%+v).err = %v, want no error.", opts, err)
	}
	wantIO := &CommandIO{
		InputSpec:             &command.InputSpec{Inputs: existingFiles},
		OutputFiles:           []string{filepath.Clean("wd/test.d"), filepath.Clean("wd/test.o")},
		EmittedDependencyFile: filepath.Clean("wd/test.d"),
		UsedShallowMode:       true,
	}
	if diff := cmp.Diff(wantIO, gotIO, strSliceCmp); diff != "" {
		t.Errorf("ProcessInputs(%v) returned diff in CommandIO, (-want +got): %s", opts, diff)
	}
}

func TestHeaderABIDumper(t *testing.T) {
	ctx := context.Background()
	ds := &stubCPPDependencyScanner{
		processInputsReturnValue: []string{
			"wd/ISoundTriggerClient.cpp",
			"wd/frameworks/av/soundtrigger/ISoundTriggerClient.cpp",
		},
	}
	resMgr := localresources.NewDefaultManager()
	ip := newInputProcessor(ds, dsTimeout, false, nil, resMgr, nil, nil)
	existingFiles := []string{
		filepath.Clean("wd/abi-header-dumper"),
		filepath.Clean("wd/libc++.so.1"),
		filepath.Clean("wd/ISoundTriggerClient.cpp"),
		filepath.Clean("wd/frameworks/av/soundtrigger/ISoundTriggerClient.cpp"),
	}
	er, cleanup := execroot.Setup(t, append(existingFiles, filepath.Join(wd, "remote_toolchain_inputs")))
	defer cleanup()

	cmd := []string{"abi-header-dumper", "-o", "test.sdump", "test.cpp", "--", "-Werror"}
	lbls := map[string]string{
		"type": "abi-dump",
		"tool": "header-abi-dumper",
	}
	i := &command.InputSpec{Inputs: []string{filepath.Clean("wd/libc++.so.1")}}
	opts := &ProcessInputsOptions{
		ExecutionID: fakeExecutionID,
		Cmd:         cmd,
		WorkingDir:  wd,
		ExecRoot:    er,
		Inputs:      i,
		Labels:      lbls,
	}
	gotIO, err := ip.ProcessInputs(ctx, opts, &logger.LogRecord{LogRecord: &lpb.LogRecord{}})
	if err != nil {
		t.Errorf("ProcessInputs(%+v).err = %v, want no error.", opts, err)
	}
	wantIO := &CommandIO{
		InputSpec:   &command.InputSpec{Inputs: existingFiles},
		OutputFiles: []string{filepath.Clean("wd/test.sdump")},
	}
	if diff := cmp.Diff(wantIO, gotIO, strSliceCmp); diff != "" {
		t.Errorf("ProcessInputs(%v) returned diff in CommandIO, (-want +got): %s", opts, diff)
	}
}

func TestJavac(t *testing.T) {
	ctx := context.Background()
	resMgr := localresources.NewDefaultManager()
	ip := newInputProcessor(nil, dsTimeout, false, nil, resMgr, nil, nil)
	existingFiles := []string{
		filepath.Clean("wd/foo.java"),
		filepath.Clean("wd/bar/bar.java"),
		filepath.Clean("wd/bar/baz.java"),
	}
	er, cleanup := execroot.Setup(t, append(existingFiles, filepath.Join(wd, "remote_toolchain_inputs")))
	execroot.AddFileWithContent(t, filepath.Join(er, wd, "foo.rsp"), []byte("foo.java"))
	defer cleanup()

	cmd := []string{"javac", "-classpath", "bar", "-s", "out", "@foo.rsp"}
	lbls := map[string]string{
		"type":     "compile",
		"compiler": "javac",
		"lang":     "java",
		"shallow":  "false",
	}
	i := &command.InputSpec{Inputs: []string{filepath.Clean("wd/bar/baz.java")}}
	opts := &ProcessInputsOptions{
		ExecutionID: fakeExecutionID,
		Cmd:         cmd,
		WorkingDir:  wd,
		ExecRoot:    er,
		Inputs:      i,
		Labels:      lbls,
	}
	gotIO, err := ip.ProcessInputs(ctx, opts, &logger.LogRecord{LogRecord: &lpb.LogRecord{}})
	if err != nil {
		t.Errorf("ProcessInputs(%+v).err = %v, want no error.", opts, err)
	}
	wantIO := &CommandIO{
		InputSpec:         &command.InputSpec{Inputs: []string{filepath.Clean("wd/foo.rsp"), filepath.Clean("wd/bar"), filepath.Clean("wd/bar/baz.java"), filepath.Clean("wd/foo.java")}},
		OutputDirectories: []string{filepath.Clean("wd/out")},
	}
	if diff := cmp.Diff(wantIO, gotIO, strSliceCmp); diff != "" {
		t.Errorf("ProcessInputs(%v) returned diff in CommandIO, (-want +got): %s", opts, diff)
	}
}

func TestMetalava(t *testing.T) {
	ctx := context.Background()
	resMgr := localresources.NewDefaultManager()
	ip := newInputProcessor(nil, dsTimeout, false, &execStub{stdout: "Metalava: 1.3.0"}, resMgr, nil, nil)
	existingFiles := []string{
		filepath.Clean("wd/foo.java"),
		filepath.Clean("wd/bar/bar.java"),
		filepath.Clean("metalava.jar"),
	}
	er, cleanup := execroot.Setup(t, append(existingFiles, filepath.Join(wd, "remote_toolchain_inputs")))
	execroot.AddFileWithContent(t, filepath.Join(er, wd, "foo.rsp"), []byte("foo.java"))
	execroot.AddDirs(t, er, []string{"wd/src"})
	defer cleanup()

	cmd := []string{"metalava", "-classpath", "bar", "--api", "api.txt", "@foo.rsp", "-sourcepath", "src"}
	lbls := map[string]string{
		"type":     "compile",
		"compiler": "metalava",
		"lang":     "java",
		"shallow":  "false",
	}
	i := &command.InputSpec{
		Inputs:               []string{filepath.Clean("metalava.jar")},
		EnvironmentVariables: map[string]string{"FOO": filepath.Join(er, "foo")},
	}
	opts := &ProcessInputsOptions{
		ExecutionID: fakeExecutionID,
		Cmd:         cmd,
		WorkingDir:  wd,
		ExecRoot:    er,
		Inputs:      i,
		Labels:      lbls,
	}
	gotIO, err := ip.ProcessInputs(ctx, opts, &logger.LogRecord{LogRecord: &lpb.LogRecord{}})
	if err != nil {
		t.Errorf("ProcessInputs(%+v).err = %v, want no error.", opts, err)
	}
	wantIO := &CommandIO{
		InputSpec: &command.InputSpec{
			Inputs: []string{filepath.Clean("wd/foo.rsp"), filepath.Clean("wd/bar"), filepath.Clean("wd/foo.java"), filepath.Clean("metalava.jar")},
			VirtualInputs: []*command.VirtualInput{
				&command.VirtualInput{Path: filepath.Clean("wd/src"), IsEmptyDirectory: true},
			},
			EnvironmentVariables: map[string]string{
				"FOO": filepath.Join("..", "foo"),
			},
		},
		OutputFiles: []string{filepath.Clean("wd/api.txt")},
	}
	if diff := cmp.Diff(wantIO, gotIO, strSliceCmp); diff != "" {
		t.Errorf("ProcessInputs(%v) returned diff in CommandIO, (-want +got): %s", opts, diff)
	}
}

func TestMetalavaInputsUnderSourcePath(t *testing.T) {
	ctx := context.Background()
	resMgr := localresources.NewDefaultManager()
	ip := newInputProcessor(nil, dsTimeout, false, &execStub{stdout: "Metalava: 1.3.0"}, resMgr, nil, nil)
	existingFiles := []string{
		filepath.Clean("wd/foo.java"),
		filepath.Clean("wd/src/bar/bar.java"),
		filepath.Clean("metalava.jar"),
	}
	er, cleanup := execroot.Setup(t, append(existingFiles, filepath.Join(wd, "remote_toolchain_inputs")))
	execroot.AddFileWithContent(t, filepath.Join(er, wd, "foo.rsp"), []byte("foo.java"))
	defer cleanup()

	cmd := []string{"metalava", "-classpath", "src/bar", "--api", "api.txt", "@foo.rsp", "-sourcepath", "src"}
	lbls := map[string]string{
		"type":     "compile",
		"compiler": "metalava",
		"lang":     "java",
		"shallow":  "false",
	}

	i := &command.InputSpec{Inputs: []string{filepath.Clean("metalava.jar")}}
	opts := &ProcessInputsOptions{
		ExecutionID: fakeExecutionID,
		Cmd:         cmd,
		WorkingDir:  wd,
		ExecRoot:    er,
		Inputs:      i,
		Labels:      lbls,
	}
	gotIO, err := ip.ProcessInputs(ctx, opts, &logger.LogRecord{LogRecord: &lpb.LogRecord{}})
	if err != nil {
		t.Errorf("ProcessInputs(%+v).err = %v, want no error.", opts, err)
	}
	wantIO := &CommandIO{
		InputSpec: &command.InputSpec{
			Inputs:        []string{filepath.Clean("wd/foo.rsp"), filepath.Clean("wd/src/bar"), filepath.Clean("wd/foo.java"), filepath.Clean("metalava.jar")},
			VirtualInputs: []*command.VirtualInput{{Path: filepath.Clean("wd/src"), IsEmptyDirectory: true}},
		},
		OutputFiles: []string{filepath.Clean("wd/api.txt")},
	}
	if diff := cmp.Diff(wantIO, gotIO, strSliceCmp); diff != "" {
		t.Errorf("ProcessInputs(%v) returned diff in CommandIO, (-want +got): %s", opts, diff)
	}
}

func TestIncludeDirectoriesWithNoInputs_VirtualInputsAdded(t *testing.T) {
	tests := []struct {
		cmd   []string
		name  string
		files []string
		dirs  []string
		want  *command.InputSpec
	}{
		{
			name: "Single non-existent directory",
			cmd:  []string{"clang++", "-c", "-o", "test.o", "-Ia", "-MF", "test.d", "test.cpp"},
			dirs: []string{"wd/a"},
			want: &command.InputSpec{
				Inputs: []string{filepath.Clean("wd/test.cpp")},
				VirtualInputs: []*command.VirtualInput{
					&command.VirtualInput{Path: filepath.Clean("wd/a"), IsEmptyDirectory: true},
				},
			},
		},
		{
			name: "-I<path> follows -I <path>",
			cmd:  []string{"clang++", "-c", "-o", "test.o", "-Ia", "-I", "b/c", "-MF", "test.d", "test.cpp"},
			dirs: []string{"wd/a", "wd/b/c"},
			want: &command.InputSpec{
				Inputs: []string{filepath.Clean("wd/test.cpp")},
				VirtualInputs: []*command.VirtualInput{
					&command.VirtualInput{Path: filepath.Clean("wd/a"), IsEmptyDirectory: true},
					&command.VirtualInput{Path: filepath.Clean("wd/b/c"), IsEmptyDirectory: true},
				},
			},
		},
		{
			name: "-I <path> follows -I<path>",
			cmd:  []string{"clang++", "-c", "-o", "test.o", "-I", "a/b", "-Ic/d", "-MF", "test.d", "test.cpp"},
			dirs: []string{"wd/a/b", "wd/c/d"},
			want: &command.InputSpec{
				Inputs: []string{filepath.Clean("wd/test.cpp")},
				VirtualInputs: []*command.VirtualInput{
					&command.VirtualInput{Path: filepath.Clean("wd/a/b"), IsEmptyDirectory: true},
					&command.VirtualInput{Path: filepath.Clean("wd/c/d"), IsEmptyDirectory: true},
				},
			},
		},
		{
			name: "-isystem<path> follows -I<path>",
			cmd:  []string{"clang++", "-c", "-o", "test.o", "-isystema/b", "-Ic/d", "-MF", "test.d", "test.cpp"},
			dirs: []string{"wd/a/b", "wd/c/d"},
			want: &command.InputSpec{
				Inputs: []string{filepath.Clean("wd/test.cpp")},
				VirtualInputs: []*command.VirtualInput{
					&command.VirtualInput{Path: filepath.Clean("wd/a/b"), IsEmptyDirectory: true},
					&command.VirtualInput{Path: filepath.Clean("wd/c/d"), IsEmptyDirectory: true},
				},
			},
		},
		{
			name: "-isystem <path> follows -I<path>",
			cmd:  []string{"clang++", "-c", "-o", "test.o", "-isystem", "a/b", "-Ic/d", "-MF", "test.d", "test.cpp"},
			dirs: []string{"wd/a/b", "wd/c/d"},
			want: &command.InputSpec{
				Inputs: []string{filepath.Clean("wd/test.cpp")},
				VirtualInputs: []*command.VirtualInput{
					&command.VirtualInput{Path: filepath.Clean("wd/a/b"), IsEmptyDirectory: true},
					&command.VirtualInput{Path: filepath.Clean("wd/c/d"), IsEmptyDirectory: true},
				},
			},
		},
		{
			name: "-isystem <path> follows -isystem <path>/<subpath>",
			cmd:  []string{"clang++", "-c", "-o", "test.o", "-isystem", "a/b", "-isystem", "a/b/c", "-MF", "test.d", "test.cpp"},
			dirs: []string{"wd/a/b/c"},
			want: &command.InputSpec{
				Inputs: []string{filepath.Clean("wd/test.cpp")},
				VirtualInputs: []*command.VirtualInput{
					&command.VirtualInput{Path: filepath.Clean("wd/a/b"), IsEmptyDirectory: true},
					&command.VirtualInput{Path: filepath.Clean("wd/a/b/c"), IsEmptyDirectory: true},
				},
			},
		},
	}

	for _, test := range tests {
		ctx := context.Background()
		ds := &stubCPPDependencyScanner{
			processInputsReturnValue: []string{"wd/test.cpp"},
		}
		resMgr := localresources.NewDefaultManager()
		ip := newInputProcessor(ds, dsTimeout, false, nil, resMgr, nil, nil)
		existingFiles := []string{filepath.Clean("wd/test.cpp")}
		er, cleanup := execroot.Setup(t, append(existingFiles, filepath.Join(wd, "remote_toolchain_inputs")))
		execroot.AddFiles(t, er, test.files)
		execroot.AddDirs(t, er, test.dirs)
		defer cleanup()

		lbls := map[string]string{
			"type":     "compile",
			"compiler": "clang",
			"lang":     "cpp",
			"shallow":  "false",
		}
		shallowFallbackConfig[labels.FromMap(lbls)] = map[ppb.ExecutionStrategy_Value]bool{ppb.ExecutionStrategy_UNSPECIFIED: false}
		defer func() {
			shallowFallbackConfig[labels.FromMap(lbls)] = map[ppb.ExecutionStrategy_Value]bool{ppb.ExecutionStrategy_UNSPECIFIED: true}
		}()
		i := &command.InputSpec{Inputs: []string{filepath.Clean("wd/libc++.so.1")}}
		opts := &ProcessInputsOptions{
			ExecutionID: fakeExecutionID,
			Cmd:         test.cmd,
			WorkingDir:  wd,
			ExecRoot:    er,
			Inputs:      i,
			Labels:      lbls,
		}
		gotIO, err := ip.ProcessInputs(ctx, opts, &logger.LogRecord{LogRecord: &lpb.LogRecord{}})
		if err != nil {
			t.Errorf("ProcessInputs(%+v).err = %v, want no error.", opts, err)
		}

		if diff := cmp.Diff(test.want, gotIO.InputSpec); diff != "" {
			t.Errorf("ProcessInputs(%+v) returned diff in inputs, (-want +got): %s", opts, diff)
		}
	}
}

func TestFromAndroidCompileCommand(t *testing.T) {
	ctx := context.Background()
	ds := &stubCPPDependencyScanner{
		processInputsReturnValue: []string{
			"ISoundTriggerClient.cpp",
			"frameworks/av/soundtrigger/ISoundTriggerClient.cpp",
			"external/libcxx/include/stdint.h",
		},
	}
	resMgr := localresources.NewDefaultManager()
	ip := newInputProcessor(ds, dsTimeout, false, nil, resMgr, nil, nil)
	existingFiles := []string{
		filepath.Clean("ISoundTriggerClient.cpp"),
		filepath.Clean("frameworks/av/soundtrigger/ISoundTriggerClient.cpp"),
		filepath.Clean("external/libcxx/include/stdint.h"),
	}
	er, cleanup := execroot.Setup(t, append(existingFiles, "remote_toolchain_inputs"))
	defer cleanup()

	execroot.AddDirs(t, er, []string{
		"system/core/libcutils/include",
		"system/core/libutils/include",
		"system/core/libbacktrace/include",
		"system/core/liblog/include",
		"system/core/libsystem/include",
		"out/soong/.intermediates/frameworks/native/libs/binder/libbinder/android_arm_armv7-a-neon_core_static/gen/aidl",
		"frameworks/native/libs/binder/include",
		"system/core/base/include",
		"external/libcxxabi/include",
		"bionic/libc/system_properties/include",
		"system/core/property_service/libpropertyinfoparser/include",
		"system/core/include",
		"system/media/audio/include",
		"hardware/libhardware/include",
		"hardware/libhardware_legacy/include",
		"hardware/ril/include",
		"libnativehelper/include",
		"frameworks/native/include",
		"frameworks/native/opengl/include",
		"frameworks/av/include",
		"bionic/libc/include",
		"bionic/libc/kernel/uapi/asm-arm",
		"bionic/libc/kernel/android/scsi",
		"bionic/libc/kernel/android/uapi",
		"libnativehelper/include_jni",
	})

	cmd := fromFile(t, androidCommandFilePath)
	lbls := map[string]string{
		"type":     "compile",
		"compiler": "clang",
		"lang":     "cpp",
		"shallow":  "false",
	}
	opts := &ProcessInputsOptions{
		ExecutionID: fakeExecutionID,
		Cmd:         cmd,
		ExecRoot:    er,
		Labels:      lbls,
	}
	gotIO, err := ip.ProcessInputs(ctx, opts, &logger.LogRecord{LogRecord: &lpb.LogRecord{}})
	if err != nil {
		t.Errorf("ProcessInputs(%+v).err = %v, want no error.", opts, err)
	}

	wantInp := &command.InputSpec{
		Inputs: existingFiles,
		VirtualInputs: []*command.VirtualInput{
			&command.VirtualInput{Path: filepath.Clean("frameworks/av/soundtrigger"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("system/core/libcutils/include"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("system/core/libutils/include"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("system/core/libbacktrace/include"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("system/core/liblog/include"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("system/core/libsystem/include"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("out/soong/.intermediates/frameworks/native/libs/binder/libbinder/android_arm_armv7-a-neon_core_static/gen/aidl"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("frameworks/native/libs/binder/include"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("system/core/base/include"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("external/libcxx/include"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("external/libcxxabi/include"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("bionic/libc/system_properties/include"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("system/core/property_service/libpropertyinfoparser/include"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("system/core/include"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("system/media/audio/include"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("hardware/libhardware/include"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("hardware/libhardware_legacy/include"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("hardware/ril/include"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("libnativehelper/include"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("frameworks/native/include"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("frameworks/native/opengl/include"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("frameworks/av/include"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("bionic/libc/include"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("bionic/libc/kernel/uapi"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("bionic/libc/kernel/uapi/asm-arm"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("bionic/libc/kernel/android/scsi"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("bionic/libc/kernel/android/uapi"), IsEmptyDirectory: true},
			&command.VirtualInput{Path: filepath.Clean("libnativehelper/include_jni"), IsEmptyDirectory: true},
		},
	}
	wantOut := []string{
		filepath.Clean("out/soong/.intermediates/frameworks/av/soundtrigger/libsoundtrigger/android_arm_armv7-a-neon_core_shared/obj/frameworks/av/soundtrigger/ISoundTriggerClient.o.d"),
		filepath.Clean("out/soong/.intermediates/frameworks/av/soundtrigger/libsoundtrigger/android_arm_armv7-a-neon_core_shared/obj/frameworks/av/soundtrigger/ISoundTriggerClient.o"),
	}
	wantIO := &CommandIO{
		InputSpec:             wantInp,
		OutputFiles:           wantOut,
		EmittedDependencyFile: filepath.Clean("out/soong/.intermediates/frameworks/av/soundtrigger/libsoundtrigger/android_arm_armv7-a-neon_core_shared/obj/frameworks/av/soundtrigger/ISoundTriggerClient.o.d"),
	}
	if diff := cmp.Diff(wantIO, gotIO, strSliceCmp); diff != "" {
		t.Errorf("ProcessInputs(%v) returned diff in CommandIO, (-want +got): %s", opts, diff)
	}
}

func TestError(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Unable to get working directory: %v", err)
	}

	tests := []struct {
		name            string
		executionID     string
		workingDir      string
		execRoot        string
		inputSpec       *command.InputSpec
		lbls            map[string]string
		command         []string
		shallowFallback bool
		depsScanErr     error
		wantErr         error
	}{
		{
			name:        "ProcessInputs fails when subprocess returns error",
			executionID: "test1",
			workingDir:  ".",
			execRoot:    cwd,
			inputSpec:   &command.InputSpec{},
			lbls: map[string]string{
				"type":     "compile",
				"compiler": "clang",
				"lang":     "cpp",
				"shallow":  "false",
			},
			command: []string{"clang++", "-c", "test.cpp"},
			// expect deps scanner DeadlineExceeded to be wrapped as ErrIPTimeout
			depsScanErr: context.DeadlineExceeded,
			wantErr:     ErrIPTimeout,
		},
		{
			name:        "ProcessInputs succeeds when subprocess returns error but fallback is on",
			executionID: "test1",
			workingDir:  ".",
			execRoot:    cwd,
			inputSpec:   &command.InputSpec{},
			lbls: map[string]string{
				"type":     "compile",
				"compiler": "clang",
				"lang":     "cpp",
			},
			command:         []string{"clang++", "-c", "test.cpp"},
			shallowFallback: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			_, cleanup := execroot.Setup(t, []string{filepath.Join(test.workingDir, "remote_toolchain_inputs")})
			defer cleanup()
			ds := &stubCPPDependencyScanner{processInputsError: fmt.Errorf("fail: %w", test.depsScanErr)}
			resMgr := localresources.NewDefaultManager()
			ip := newInputProcessor(ds, dsTimeout, false, nil, resMgr, nil, nil)
			shallowFallbackConfig[labels.FromMap(test.lbls)] = map[ppb.ExecutionStrategy_Value]bool{ppb.ExecutionStrategy_UNSPECIFIED: test.shallowFallback}
			defer func() {
				shallowFallbackConfig[labels.FromMap(test.lbls)] = map[ppb.ExecutionStrategy_Value]bool{ppb.ExecutionStrategy_UNSPECIFIED: !test.shallowFallback}
			}()

			opts := &ProcessInputsOptions{
				ExecutionID: test.executionID,
				Cmd:         test.command,
				WorkingDir:  test.workingDir,
				ExecRoot:    test.execRoot,
				Inputs:      test.inputSpec,
				Labels:      test.lbls,
			}
			// verify that ProcessInputs wraps correctly dependency scanner's error
			if _, err := ip.ProcessInputs(ctx, opts, &logger.LogRecord{LogRecord: &lpb.LogRecord{}}); !errors.Is(err, test.wantErr) {
				t.Errorf("ProcessInputs(%+v)=%q, want: %q", opts, err, test.depsScanErr)
			}
		})
	}
}

func TestTool(t *testing.T) {
	ctx := context.Background()
	ip := &InputProcessor{}
	existingFiles := []string{
		filepath.Join(wd, "my/bin"),
		filepath.Join(wd, "input"),
	}
	er, cleanup := execroot.Setup(t, existingFiles)
	defer cleanup()

	cmd := []string{"my/bin", "input"}
	lbls := map[string]string{
		"type": "tool",
	}
	opts := &ProcessInputsOptions{
		ExecutionID: fakeExecutionID,
		Cmd:         cmd,
		WorkingDir:  wd,
		ExecRoot:    er,
		Labels:      lbls,
		Inputs: &command.InputSpec{
			Inputs: []string{filepath.Join(wd, "input")},
		},
	}
	gotIO, err := ip.ProcessInputs(ctx, opts, &logger.LogRecord{LogRecord: &lpb.LogRecord{}})
	if err != nil {
		t.Errorf("ProcessInputs(%+v).err = %v, want no error.", opts, err)
	}
	wantIO := &CommandIO{
		InputSpec: &command.InputSpec{Inputs: existingFiles},
	}
	if diff := cmp.Diff(wantIO, gotIO, strSliceCmp); diff != "" {
		t.Errorf("ProcessInputs(%v) returned diff in CommandIO, (-want +got): %s", opts, diff)
	}
}

func TestSystemTool(t *testing.T) {
	ctx := context.Background()
	ip := &InputProcessor{}
	existingFiles := []string{
		filepath.Join(wd, "input"),
	}
	er, cleanup := execroot.Setup(t, existingFiles)
	defer cleanup()

	cmd := []string{"cat", "input"}
	lbls := map[string]string{
		"type": "tool",
	}
	opts := &ProcessInputsOptions{
		ExecutionID: fakeExecutionID,
		Cmd:         cmd,
		WorkingDir:  wd,
		ExecRoot:    er,
		Labels:      lbls,
		Inputs: &command.InputSpec{
			Inputs: []string{filepath.Join(wd, "input")},
		},
	}
	gotIO, err := ip.ProcessInputs(ctx, opts, &logger.LogRecord{LogRecord: &lpb.LogRecord{}})
	if err != nil {
		t.Errorf("ProcessInputs(%+v).err = %v, want no error.", opts, err)
	}
	wantIO := &CommandIO{
		InputSpec: &command.InputSpec{Inputs: existingFiles},
	}
	if diff := cmp.Diff(wantIO, gotIO, strSliceCmp); diff != "" {
		t.Errorf("ProcessInputs(%v) returned diff in CommandIO, (-want +got): %s", opts, diff)
	}
}

func TestShallow(t *testing.T) {
	ctx := context.Background()
	resMgr := localresources.NewDefaultManager()
	ip := newInputProcessor(nil, dsTimeout, false, nil, resMgr, nil, nil)
	existingFiles := []string{
		filepath.Clean("wd/test.cpp"),
		filepath.Clean("clang++"),
	}
	er, cleanup := execroot.Setup(t, append(existingFiles, filepath.Join(wd, "remote_toolchain_inputs")))
	defer cleanup()

	cmd := []string{"../clang++", "-Ifoo", "-I", "bar", "-c", "-o", "test.o", "-MF", "test.d", "test.cpp"}
	lbls := map[string]string{
		"type":     "compile",
		"compiler": "clang",
		"lang":     "cpp",
		"shallow":  "true",
	}
	opts := &ProcessInputsOptions{
		ExecutionID: fakeExecutionID,
		Cmd:         cmd,
		WorkingDir:  wd,
		ExecRoot:    er,
		Labels:      lbls,
	}
	gotIO, err := ip.ProcessInputs(ctx, opts, &logger.LogRecord{LogRecord: &lpb.LogRecord{}})
	if err != nil {
		t.Errorf("ProcessInputs(%+v).err = %v, want no error.", opts, err)
	}
	wantIO := &CommandIO{
		InputSpec:             &command.InputSpec{Inputs: existingFiles},
		OutputFiles:           []string{filepath.Clean("wd/test.o"), filepath.Clean("wd/test.d")},
		EmittedDependencyFile: filepath.Clean("wd/test.d"),
		UsedShallowMode:       true,
	}
	if diff := cmp.Diff(wantIO, gotIO, strSliceCmp); diff != "" {
		t.Errorf("ProcessInputs(%v) returned diff in CommandIO, (-want +got): %s", opts, diff)
	}
}

func TestError_InvalidLabels(t *testing.T) {
	ctx := context.Background()
	ip := &InputProcessor{}
	er, cleanup := execroot.Setup(t, []string{filepath.Join(wd, "remote_toolchain_inputs")})
	defer cleanup()

	cmd := []string{"clang++", "-c", "-o", "test.o", "-MF", "test.d", "test.cpp"}
	lbls := map[string]string{
		"compiler": "clang",
		"lang":     "cpp",
	}
	opts := &ProcessInputsOptions{
		ExecutionID: fakeExecutionID,
		Cmd:         cmd,
		WorkingDir:  wd,
		ExecRoot:    er,
		Labels:      lbls,
	}
	if _, err := ip.ProcessInputs(ctx, opts, &logger.LogRecord{LogRecord: &lpb.LogRecord{}}); status.Code(err) != codes.Unimplemented {
		t.Errorf("ProcessInputs(%+v).err.Code() = %v, want %v.", opts, status.Code(err), codes.Unimplemented)
	}
}

// Tests that the cache returns the expected result by deleting the file between first and second call
func TestFileCache(t *testing.T) {
	ctx := context.Background()
	resMgr := localresources.NewDefaultManager()
	ip := newInputProcessor(nil, dsTimeout, false, nil, resMgr, nil, nil)
	existingFiles := []string{
		filepath.Clean("wd/test.cpp"),
		filepath.Clean("clang++"),
	}
	er, cleanup := execroot.Setup(t, append(existingFiles, filepath.Join(wd, "remote_toolchain_inputs")))
	defer cleanup()

	cmd := []string{"../clang++", "-Ifoo", "-I", "bar", "-c", "-o", "test.o", "-MF", "test.d", "test.cpp"}
	lbls := map[string]string{
		"type":     "compile",
		"compiler": "clang",
		"lang":     "cpp",
		"shallow":  "true",
	}
	opts := &ProcessInputsOptions{
		ExecutionID: fakeExecutionID,
		Cmd:         cmd,
		WorkingDir:  wd,
		ExecRoot:    er,
		Labels:      lbls,
	}
	// Process the inputs
	gotIOA, err := ip.ProcessInputs(ctx, opts, &logger.LogRecord{LogRecord: &lpb.LogRecord{}})
	if err != nil {
		t.Errorf("First ProcessInputs(%+v).err = %v, want no error.", opts, err)
	}
	// Cleanup the generated files
	cleanup()
	// Re-process the inputs
	gotIOB, err := ip.ProcessInputs(ctx, opts, &logger.LogRecord{LogRecord: &lpb.LogRecord{}})
	if err != nil {
		t.Errorf("Second ProcessInputs(%+v).err = %v, want no error.", opts, err)
	}

	if diff := cmp.Diff(gotIOA, gotIOB, strSliceCmp); diff != "" {
		// If both cached, expect the results to be the same
		t.Errorf("ProcessInputs(%v) returned different results on both executions, expected identical (-first +cached): %s", opts, diff)
	}
}

// Tests that missing files are not cached on the first call, and are returned by the second call
// after they are created.
func TestNoFileCache(t *testing.T) {
	ctx := context.Background()
	resMgr := localresources.NewDefaultManager()
	ip := newInputProcessor(nil, dsTimeout, false, nil, resMgr, nil, nil)
	existingFiles := []string{
		filepath.Clean("wd/test.cpp"),
		filepath.Clean("clang++"),
	}
	er, cleanup := execroot.Setup(t, []string{filepath.Join(wd, "remote_toolchain_inputs")})
	defer cleanup()

	cmd := []string{"../clang++", "-Ifoo", "-I", "bar", "-c", "-o", "test.o", "-MF", "test.d", "test.cpp"}
	lbls := map[string]string{
		"type":     "compile",
		"compiler": "clang",
		"lang":     "cpp",
		"shallow":  "true",
	}
	opts := &ProcessInputsOptions{
		ExecutionID: fakeExecutionID,
		Cmd:         cmd,
		WorkingDir:  wd,
		ExecRoot:    er,
		Labels:      lbls,
	}
	// Process the inputs; expect failure because "existingFiles" do not exist
	gotIOA, err := ip.ProcessInputs(ctx, opts, &logger.LogRecord{LogRecord: &lpb.LogRecord{}})
	if err != nil {
		t.Errorf("First ProcessInputs(%+v).err = %v, want no error.", opts, err)
	}
	// Cleanup then recreate files, with the "existingFiles" this time
	cleanup()
	er, cleanup = execroot.Setup(t, append(existingFiles, filepath.Join(wd, "remote_toolchain_inputs")))
	defer cleanup() // cleanup() is a no-op if the execroot has already been cleaned up
	opts.ExecRoot = er
	// Re-process the inputs
	gotIOB, err := ip.ProcessInputs(ctx, opts, &logger.LogRecord{LogRecord: &lpb.LogRecord{}})
	cleanup()
	if err != nil {
		t.Errorf("Second ProcessInputs(%+v).err = %v, want no error.", opts, err)
	}

	wantIO := &CommandIO{
		InputSpec:             &command.InputSpec{Inputs: existingFiles},
		OutputFiles:           []string{filepath.Clean("wd/test.o"), filepath.Clean("wd/test.d")},
		EmittedDependencyFile: filepath.Clean("wd/test.d"),
		UsedShallowMode:       true,
	}
	// A and B should be different
	if diff := cmp.Diff(gotIOA, gotIOB, strSliceCmp); diff == "" {
		// If both cached, expect the results to be the same
		t.Errorf("ProcessInputs(%v) returned identical results on both executions, expected different: %s", opts, gotIOA)
	}
	// But B should still have everything we want
	if diff := cmp.Diff(wantIO, gotIOB, strSliceCmp); diff != "" {
		t.Errorf("ProcessInputs(%v) returned diff in CommandIO, (-want +got): %s", opts, diff)
	}
}

func TestGetDepsCacheMode(t *testing.T) {
	tests := []struct {
		name                      string
		cacheDir                  string
		enableDepsCache           bool
		experimentalGomaDepsCache bool
		want                      depsCacheMode
	}{
		{
			name:            "NoCacheDir",
			cacheDir:        "",
			enableDepsCache: false,
			want:            noDepsCache,
		},
		{
			name:            "CacheDir",
			cacheDir:        "/test/abc",
			enableDepsCache: true,
			want: func() depsCacheMode {
				if cppdependencyscanner.Type() == cppdependencyscanner.Goma {
					return gomaDepsCache
				}
				return reproxyDepsCache
			}(),
		},
		{
			name:                      "ExperimentalGomaCacheDir",
			cacheDir:                  "/test/abc",
			enableDepsCache:           true,
			experimentalGomaDepsCache: true,
			want:                      reproxyDepsCache,
		},
		{
			name:                      "ExperimentalGomaNoCacheDir",
			cacheDir:                  "",
			experimentalGomaDepsCache: true,
			enableDepsCache:           true,
			want:                      noDepsCache,
		},
		{
			name:            "CacheDirEnableDepsCacheFalse",
			cacheDir:        "/test/abc",
			enableDepsCache: false,
			want:            noDepsCache,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			old := features.GetConfig().ExperimentalGomaDepsCache
			defer func() {
				features.GetConfig().ExperimentalGomaDepsCache = old
			}()
			features.GetConfig().ExperimentalGomaDepsCache = tc.experimentalGomaDepsCache
			if got := getDepsCacheMode(tc.cacheDir, tc.enableDepsCache); got != tc.want {
				t.Errorf("getDepsCacheMode(%v, %v) with ExperimentalGomaNoCacheDir=%v returned %v, expected %v", tc.cacheDir, tc.enableDepsCache, tc.experimentalGomaDepsCache, got, tc.want)
			}
		})
	}
}

type stubCPPDependencyScanner struct {
	processInputsReturnValue []string
	processInputsError       error
	calls                    int
	processInputsArgs        []string
	ignoredPluginsMap        map[string]bool
}

func (s *stubCPPDependencyScanner) ProcessInputs(_ context.Context, _ string, args []string, _ string, _ string, _ []string) ([]string, bool, error) {
	s.processInputsArgs = args
	s.calls++
	return s.processInputsReturnValue, false, s.processInputsError
}

func (s *stubCPPDependencyScanner) ShouldIgnorePlugin(plugin string) bool {
	_, present := s.ignoredPluginsMap[plugin]
	return present
}

type execStub struct {
	stdout string
	stderr string
	err    error
}

func (e *execStub) Execute(ctx context.Context, cmd *command.Command) (string, string, error) {
	return e.stdout, e.stderr, e.err
}

// This is unused in inputprocessor tests but required due to depsscannerservice
func (e *execStub) ExecuteInBackground(_ context.Context, _ *command.Command, _ outerr.OutErr, _ chan *command.Result) error {
	return nil
}

func fromFile(t *testing.T, name string) []string {
	t.Helper()

	runfile, err := bazel.Runfile(path.Join("pkg/inputprocessor", name))
	if err == nil {
		name = runfile
	}
	f, err := os.Open(name)
	if err != nil {
		t.Fatalf("Unable to read file %v: %v", name, err)
	}
	defer f.Close()

	var res []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		res = append(res, scanner.Text())
	}
	err = scanner.Err()
	if err != nil {
		t.Fatalf("Failed to scan %s: %v", name, err)
	}
	return res
}
