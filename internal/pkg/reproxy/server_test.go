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

package reproxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bazelbuild/reclient/internal/pkg/deps"
	"github.com/bazelbuild/reclient/internal/pkg/execroot"
	"github.com/bazelbuild/reclient/internal/pkg/localresources"
	"github.com/bazelbuild/reclient/internal/pkg/logger"
	"github.com/bazelbuild/reclient/internal/pkg/logger/event"
	"github.com/bazelbuild/reclient/internal/pkg/stats"
	"github.com/bazelbuild/reclient/internal/pkg/subprocess"
	"github.com/bazelbuild/reclient/pkg/inputprocessor"
	"github.com/bazelbuild/reclient/pkg/version"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/digest"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/fakes"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/filemetadata"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/outerr"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/testing/protocmp"

	lpb "github.com/bazelbuild/reclient/api/log"
	ppb "github.com/bazelbuild/reclient/api/proxy"
	spb "github.com/bazelbuild/reclient/api/scandeps"

	cpb "github.com/bazelbuild/remote-apis-sdks/go/api/command"
)

var cmpLogRecordsOpts = []cmp.Option{
	protocmp.IgnoreFields(&cpb.Command{}, "identifiers"),
	protocmp.IgnoreFields(&cpb.CommandResult{}, "msg"),
	protocmp.IgnoreFields(&lpb.RemoteMetadata{}, "action_digest", "command_digest", "total_input_bytes", "logical_bytes_uploaded", "real_bytes_uploaded", "logical_bytes_downloaded", "real_bytes_downloaded", "event_times", "stderr_digest", "stdout_digest"),
	protocmp.IgnoreFields(&lpb.LocalMetadata{}, "event_times"),
	protocmp.IgnoreFields(&lpb.RerunMetadata{}, "event_times"),
	protocmp.IgnoreFields(&lpb.Verification_Mismatch{}, "action_digest"),
	protocmp.SortRepeated(func(a, b string) bool { return a < b }),
	protocmp.Transform(),
}

const (
	executablePath = "fake-exec"
)

var (
	aDir      = "a"
	abPath    = filepath.Join(aDir, "b")
	abOutPath = filepath.Join(abPath, "out")
)

func TestRemoteStrategyWithLocalError(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	t.Cleanup(cleanup)

	commandText := "the-command-text"

	// should trigger javac.Preprocessor that would fail in ProcessInputs due to too few arguments
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     []string{commandText},
			ExecRoot: env.ExecRoot,
		},
		Labels:           map[string]string{"type": "compile", "compiler": "javac", "lang": "java"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_REMOTE, ReclientTimeout: 3600},
	}
	server := &Server{
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
		FileMetadataStore: filemetadata.NewSingleFlightCache(),
	}
	server.Init()
	server.SetInputProcessor(&inputprocessor.InputProcessor{}, func() {})
	server.SetREClient(env.Client, func() {})
	resp, err := server.RunCommand(context.Background(), req)
	if err != nil {
		t.Errorf("Expected err to be nil and local error to be within RunResponse, got err = %v", err)
	}
	if resp == nil {
		t.Fatal("Got nil response, expected non-nil")
	}
	if resp.Result.ExitCode != command.LocalErrorExitCode {
		t.Errorf("Expected ExitCode = %v, got = %v", command.LocalErrorExitCode, resp.Result.ExitCode)
	}
	if !strings.Contains(resp.Result.Msg, commandText) {
		t.Errorf("Expected error message that contains %q. Got %q", commandText, resp.Result.Msg)
	}

	if !strings.Contains(resp.Result.Msg, resp.ExecutionId) {
		t.Errorf("Expected error message that contains %q. Got %q", resp.ExecutionId, resp.Result.Msg)
	}
}

func TestRemote(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	files := []string{"foo.h", "bar.h", executablePath}
	execroot.AddFiles(t, env.ExecRoot, files)
	ds := &stubCPPDependencyScanner{
		processInputsReturnValue: []string{
			"foo.h",
			"bar.h",
		},
	}
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
		FileMetadataStore: fmc,
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(ds, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	ctx := context.Background()
	st := time.Now()
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     []string{executablePath, "-c", "c"},
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels: map[string]string{"type": "compile", "lang": "cpp", "compiler": "clang"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{
			ExecutionStrategy:     ppb.ExecutionStrategy_REMOTE,
			LogEnvironment:        true,
			ReclientTimeout:       3600,
			EnableAtomicDownloads: true,
		},
		Metadata: &ppb.Metadata{
			EventTimes:  map[string]*cpb.TimeInterval{"Event": {From: command.TimeToProto(st)}},
			Environment: []string{"FOO=1", "BAR=2"},
		},
	}
	wantCmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        []string{executablePath, "-c", "c"},
		ExecRoot:    env.ExecRoot,
		InputSpec: &command.InputSpec{
			Inputs: []string{"foo.h", "bar.h", executablePath},
		},
		OutputFiles: []string{abOutPath},
	}
	setPlatformOSFamily(wantCmd)
	res := &command.Result{Status: command.CacheHitResultStatus}
	wantStdErr := []byte("stderr")
	env.Set(wantCmd, command.DefaultExecutionOptions(), res, fakes.StdErr(wantStdErr), &fakes.OutputFile{abOutPath, "output"})
	for i := 0; i < 2; i++ {
		got, err := server.RunCommand(ctx, req)
		if err != nil {
			t.Errorf("RunCommand() returned error: %v", err)
		}
		want := &ppb.RunResponse{
			Result: &cpb.CommandResult{
				Status:   cpb.CommandResultStatus_CACHE_HIT,
				ExitCode: 0,
			},
			Stderr: wantStdErr,
		}
		if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
			t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
		}
		path := filepath.Join(env.ExecRoot, abOutPath)
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("Error reading from %s: %v", path, err)
		}
		if !bytes.Equal(contents, []byte("output")) {
			t.Errorf("Expected %s to contain \"output\", got %v", path, contents)
		}
	}
	if fmc.GetCacheHits() != 8 || fmc.GetCacheMisses() != 3 {
		t.Errorf("Cache Hits/Misses = %v, %v. Want 8, 3", fmc.GetCacheHits(), fmc.GetCacheMisses())
	}
	gotSummary, _ := lg.GetStatusSummary(ctx, &ppb.GetStatusSummaryRequest{})
	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	rec := &lpb.LogRecord{
		Command:          command.ToProto(wantCmd),
		Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
		CompletionStatus: lpb.CompletionStatus_STATUS_CACHE_HIT,
		RemoteMetadata: &lpb.RemoteMetadata{
			Result:              &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
			NumInputFiles:       3,
			NumInputDirectories: 1,
			NumOutputFiles:      1,
			TotalOutputBytes:    12, // "output" + "stderr"
			OutputFileDigests: map[string]string{
				abOutPath: digest.NewFromBlob([]byte("output")).String(),
			},
		},
		LocalMetadata: &lpb.LocalMetadata{
			Environment: map[string]string{"FOO": "1", "BAR": "2"},
			Labels:      map[string]string{"type": "compile", "lang": "cpp", "compiler": "clang"},
		},
	}
	wantRecs := []*lpb.LogRecord{rec, rec}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Errorf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
	wantSummary := &ppb.GetStatusSummaryResponse{
		CompletedActionStats: map[string]int32{
			lpb.CompletionStatus_STATUS_CACHE_HIT.String(): 2,
		},
		RunningActions: 0,
	}
	if diff := cmp.Diff(wantSummary, gotSummary, protocmp.Transform()); diff != "" {
		t.Errorf("Status summary returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestRemote_CanonicalWorkingDir(t *testing.T) {
	t.Parallel()
	// Setup exec root and working directory
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	wd := filepath.Join("some", "wd")
	absWD := filepath.Join(env.ExecRoot, wd)
	if err := os.MkdirAll(absWD, 0744); err != nil {
		t.Errorf("Unable to create working dir %v: %v", absWD, err)
	}
	wdABOutPath := filepath.Join(wd, abOutPath)
	files := []string{"foo.h", "bar.h", executablePath}
	execroot.AddFiles(t, env.ExecRoot, files)

	// Initialize deps scanner with stub values
	ds := &stubCPPDependencyScanner{
		processInputsReturnValue: []string{
			"foo.h",
			"bar.h",
		},
	}

	// Start reproxy server
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
		FileMetadataStore: fmc,
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(ds, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg

	// Construct run request with remote exec strategy and CanonicalizeWorkingDir=true
	ctx := context.Background()
	st := time.Now()
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:             []string{filepath.Join("..", "..", executablePath), "-c", "c"},
			ExecRoot:         env.ExecRoot,
			WorkingDirectory: wd,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{wdABOutPath},
			},
		},
		Labels: map[string]string{"type": "compile", "lang": "cpp", "compiler": "clang"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{
			ExecutionStrategy:     ppb.ExecutionStrategy_REMOTE,
			LogEnvironment:        true,
			ReclientTimeout:       3600,
			EnableAtomicDownloads: true,
			RemoteExecutionOptions: &ppb.RemoteExecutionOptions{
				AcceptCached:                 true,
				DoNotCache:                   false,
				DownloadOutputs:              true,
				PreserveUnchangedOutputMtime: false,
				CanonicalizeWorkingDir:       true,
			},
		},
		Metadata: &ppb.Metadata{
			EventTimes:  map[string]*cpb.TimeInterval{"Event": {From: command.TimeToProto(st)}},
			Environment: []string{"FOO=1", "BAR=2"},
		},
	}

	// Preload desired command and result into the fake rexec cache
	wantCmd := &command.Command{
		Identifiers:      &command.Identifiers{},
		Args:             []string{filepath.Join("..", "..", executablePath), "-c", "c"},
		ExecRoot:         env.ExecRoot,
		WorkingDir:       wd,
		RemoteWorkingDir: toRemoteWorkingDir(wd),
		InputSpec: &command.InputSpec{
			Inputs: []string{"foo.h", "bar.h", executablePath},
		},
		OutputFiles: []string{abOutPath},
	}
	setPlatformOSFamily(wantCmd)
	res := &command.Result{Status: command.CacheHitResultStatus}
	wantStdErr := []byte("stderr")
	env.Set(wantCmd, command.DefaultExecutionOptions(), res, fakes.StdErr(wantStdErr), &fakes.OutputFile{abOutPath, "output"})

	// Run requests
	for i := 0; i < 2; i++ {
		got, err := server.RunCommand(ctx, req)
		if err != nil {
			t.Errorf("RunCommand() returned error: %v", err)
		}
		want := &ppb.RunResponse{
			Result: &cpb.CommandResult{
				Status:   cpb.CommandResultStatus_CACHE_HIT,
				ExitCode: 0,
			},
			Stderr: wantStdErr,
		}
		if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
			t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
		}
		path := filepath.Join(env.ExecRoot, wdABOutPath)
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("Error reading from %s: %v", path, err)
		}
		if !bytes.Equal(contents, []byte("output")) {
			t.Errorf("Expected %s to contain \"output\", got %v", path, contents)
		}
	}
	if fmc.GetCacheHits() != 8 || fmc.GetCacheMisses() != 3 {
		t.Errorf("Cache Hits/Misses = %v, %v. Want 8, 3", fmc.GetCacheHits(), fmc.GetCacheMisses())
	}

	// Shutdown reproxy and retrieve logs and metrics
	gotSummary, _ := lg.GetStatusSummary(ctx, &ppb.GetStatusSummaryRequest{})
	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}

	// Validate log records
	rec := &lpb.LogRecord{
		Command:          command.ToProto(wantCmd),
		Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
		CompletionStatus: lpb.CompletionStatus_STATUS_CACHE_HIT,
		RemoteMetadata: &lpb.RemoteMetadata{
			Result:              &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
			NumInputFiles:       3,
			NumInputDirectories: 1,
			NumOutputFiles:      1,
			TotalOutputBytes:    12, // "output" + "stderr"
			OutputFileDigests: map[string]string{
				abOutPath: digest.NewFromBlob([]byte("output")).String(),
			},
		},
		LocalMetadata: &lpb.LocalMetadata{
			Environment: map[string]string{"FOO": "1", "BAR": "2"},
			Labels:      map[string]string{"type": "compile", "lang": "cpp", "compiler": "clang"},
		},
	}
	wantRecs := []*lpb.LogRecord{rec, rec}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Errorf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
	wantSummary := &ppb.GetStatusSummaryResponse{
		CompletedActionStats: map[string]int32{
			lpb.CompletionStatus_STATUS_CACHE_HIT.String(): 2,
		},
		RunningActions: 0,
	}
	if diff := cmp.Diff(wantSummary, gotSummary, protocmp.Transform()); diff != "" {
		t.Errorf("Status summary returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestRemote_WinCross_CanonicalWorkingDir(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip()
	}
	t.Parallel()
	// Setup exec root and working directory
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	wd := filepath.Join("some", "wd")
	absWD := filepath.Join(env.ExecRoot, wd)
	if err := os.MkdirAll(absWD, 0744); err != nil {
		t.Errorf("Unable to create working dir %v: %v", absWD, err)
	}
	wdABOutPath := filepath.Join(wd, abOutPath)
	files := []string{"foo.h", "bar.h", executablePath}
	execroot.AddFiles(t, env.ExecRoot, files)

	// Initialize deps scanner with stub values
	ds := &stubCPPDependencyScanner{
		processInputsReturnValue: []string{
			"foo.h",
			"bar.h",
		},
	}

	// Start reproxy server
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
		FileMetadataStore: fmc,
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(ds, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg

	// Construct run request with remote exec strategy and CanonicalizeWorkingDir=true and Platform=linux
	ctx := context.Background()
	st := time.Now()
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:             []string{filepath.Join("..", "..", executablePath), "-c", "c"},
			ExecRoot:         env.ExecRoot,
			WorkingDirectory: wd,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{wdABOutPath},
			},
			Platform: map[string]string{
				osFamilyKey: "Linux",
			},
		},
		Labels: map[string]string{"type": "compile", "lang": "cpp", "compiler": "clang"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{
			ExecutionStrategy:     ppb.ExecutionStrategy_REMOTE,
			LogEnvironment:        true,
			ReclientTimeout:       3600,
			EnableAtomicDownloads: true,
			RemoteExecutionOptions: &ppb.RemoteExecutionOptions{
				AcceptCached:                 true,
				DoNotCache:                   false,
				DownloadOutputs:              true,
				PreserveUnchangedOutputMtime: false,
				CanonicalizeWorkingDir:       true,
			},
		},
		Metadata: &ppb.Metadata{
			EventTimes:  map[string]*cpb.TimeInterval{"Event": {From: command.TimeToProto(st)}},
			Environment: []string{"FOO=1", "BAR=2"},
		},
	}
	// Preload desired command and result into the fake rexec cache
	// output files, working directory and remote working directory have slashes flipped from \ to /
	wantCmd := &command.Command{
		Identifiers: &command.Identifiers{},
		// TODO(b/294270859): This should also be normalized to forward slashes
		Args:             []string{filepath.Join("..", "..", executablePath), "-c", "c"},
		ExecRoot:         env.ExecRoot,
		WorkingDir:       filepath.ToSlash(wd),
		RemoteWorkingDir: filepath.ToSlash(toRemoteWorkingDir(wd)),
		InputSpec: &command.InputSpec{
			Inputs: []string{"foo.h", "bar.h", executablePath},
		},
		OutputFiles: []string{filepath.ToSlash(abOutPath)},
		Platform: map[string]string{
			osFamilyKey: "Linux",
		},
	}
	res := &command.Result{Status: command.CacheHitResultStatus}
	wantStdErr := []byte("stderr")
	env.Set(wantCmd, command.DefaultExecutionOptions(), res, fakes.StdErr(wantStdErr), &fakes.OutputFile{abOutPath, "output"})

	// Run requests
	for i := 0; i < 2; i++ {
		got, err := server.RunCommand(ctx, req)
		if err != nil {
			t.Errorf("RunCommand() returned error: %v", err)
		}
		want := &ppb.RunResponse{
			Result: &cpb.CommandResult{
				Status:   cpb.CommandResultStatus_CACHE_HIT,
				ExitCode: 0,
			},
			Stderr: wantStdErr,
		}
		if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
			t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
		}
		path := filepath.Join(env.ExecRoot, wdABOutPath)
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("Error reading from %s: %v", path, err)
		}
		if !bytes.Equal(contents, []byte("output")) {
			t.Errorf("Expected %s to contain \"output\", got %v", path, contents)
		}
	}
	if fmc.GetCacheHits() != 8 || fmc.GetCacheMisses() != 3 {
		t.Errorf("Cache Hits/Misses = %v, %v. Want 8, 3", fmc.GetCacheHits(), fmc.GetCacheMisses())
	}

	// Shutdown reproxy and retrieve logs and metrics
	gotSummary, _ := lg.GetStatusSummary(ctx, &ppb.GetStatusSummaryRequest{})
	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}

	// Validate log records
	rec := &lpb.LogRecord{
		Command:          command.ToProto(wantCmd),
		Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
		CompletionStatus: lpb.CompletionStatus_STATUS_CACHE_HIT,
		RemoteMetadata: &lpb.RemoteMetadata{
			Result:              &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
			NumInputFiles:       3,
			NumInputDirectories: 1,
			NumOutputFiles:      1,
			TotalOutputBytes:    12, // "output" + "stderr"
			OutputFileDigests: map[string]string{
				abOutPath: digest.NewFromBlob([]byte("output")).String(),
			},
		},
		LocalMetadata: &lpb.LocalMetadata{
			Environment: map[string]string{"FOO": "1", "BAR": "2"},
			Labels:      map[string]string{"type": "compile", "lang": "cpp", "compiler": "clang"},
		},
	}
	wantRecs := []*lpb.LogRecord{rec, rec}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Errorf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
	wantSummary := &ppb.GetStatusSummaryResponse{
		CompletedActionStats: map[string]int32{
			lpb.CompletionStatus_STATUS_CACHE_HIT.String(): 2,
		},
		RunningActions: 0,
	}
	if diff := cmp.Diff(wantSummary, gotSummary, protocmp.Transform()); diff != "" {
		t.Errorf("Status summary returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestRemoteWithReclientTimeout(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	executor := &subprocess.SystemExecutor{}
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		LocalPool:         NewLocalPool(executor, resMgr),
		FileMetadataStore: fmc,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	var cmdArgs []string
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("sleep 10")}
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprintf("sleep 10")}
	}
	ctx := context.Background()
	wantCmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
	}
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
		},
		Labels: map[string]string{"type": "tool"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{
			RemoteExecutionOptions: &ppb.RemoteExecutionOptions{AcceptCached: false, DoNotCache: true},
			ExecutionStrategy:      ppb.ExecutionStrategy_REMOTE,
			ReclientTimeout:        3,
		},
	}

	errorMsg := "Remote action timed out by reclient timeout."
	res := &command.Result{
		Status:   command.RemoteErrorResultStatus,
		ExitCode: 45,
		Err:      errors.New(errorMsg),
	}
	setPlatformOSFamily(wantCmd)
	executionOptions := command.DefaultExecutionOptions()
	executionOptions.DoNotCache = true
	env.Set(wantCmd, executionOptions, res)

	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}

	internalErrorMsg := "rpc error: code = Internal desc = "
	wantErrPrefix := "reclient[" + got.ExecutionId + "]: " + command.RemoteErrorResultStatus.String() + ": "
	want := &ppb.RunResponse{
		Result: &cpb.CommandResult{
			Status:   cpb.CommandResultStatus_REMOTE_ERROR,
			ExitCode: 45,
			Msg:      internalErrorMsg + errorMsg,
		},
		Stderr: []byte(wantErrPrefix + internalErrorMsg + errorMsg + "\n"),
	}
	// t.Errorf("Error: %v", string(got.Stderr))
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	wantRecs := []*lpb.LogRecord{
		{
			Command: command.ToProto(wantCmd),
			Result: &cpb.CommandResult{
				Status:   cpb.CommandResultStatus_REMOTE_ERROR,
				ExitCode: 45,
			},
			CompletionStatus: lpb.CompletionStatus_STATUS_REMOTE_FAILURE,
			LocalMetadata: &lpb.LocalMetadata{
				Labels: map[string]string{"type": "tool"},
			},
			RemoteMetadata: &lpb.RemoteMetadata{
				NumInputDirectories: 1,
				Result: &cpb.CommandResult{
					Status:   cpb.CommandResultStatus_REMOTE_ERROR,
					ExitCode: 45,
				},
			},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Errorf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
}

// Tests that the correct outputs are downloaded when PreserveUnchangedOutputMtime
func TestRemoteWithPreserveUnchangedOutputMtime(t *testing.T) {
	tests := []struct {
		name        string
		testFile    string
		wantChanged bool
		origOutput  string
		wantOutput  string
		wantNumDirs int32
	}{
		{
			name:        "Test File Unchanged",
			testFile:    "unchanged.txt",
			wantNumDirs: 1,
			wantChanged: false,
			origOutput:  "output",
			wantOutput:  "output",
		},
		{
			name:        "Test Dir Unchanged",
			testFile:    abOutPath,
			wantNumDirs: 3, // abOutPath has 2 nested directories + tree root
			wantChanged: false,
			origOutput:  "output",
			wantOutput:  "output",
		},
		{
			name:        "Test New File",
			testFile:    "new.txt",
			wantNumDirs: 1,
			wantChanged: true,
			origOutput:  "",
			wantOutput:  "output",
		},
		{
			name:        "Test Changed File",
			testFile:    "changed.txt",
			wantNumDirs: 1,
			wantChanged: true,
			origOutput:  "output",
			wantOutput:  "output2",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env, cleanup := fakes.NewTestEnv(t)
			fmc := filemetadata.NewSingleFlightCache()
			env.Client.FileMetadataCache = fmc
			t.Cleanup(cleanup)
			ctx := context.Background()
			ds := &stubCPPDependencyScanner{}
			path := filepath.Join(env.ExecRoot, tc.testFile)
			files := []string{}
			if tc.origOutput != "" {
				execroot.AddFileWithContent(t, path, []byte(tc.origOutput))
				files = append(files, tc.testFile)
			}
			resMgr := localresources.NewDefaultManager()
			server := &Server{
				MaxHoldoff:        time.Minute,
				FileMetadataStore: fmc,
				DownloadTmp:       t.TempDir(),
			}
			server.Init()
			server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(ds, false, nil, resMgr), func() {})
			server.SetREClient(env.Client, func() {})
			lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
			if err != nil {
				t.Errorf("logger.New() returned error: %v", err)
			}
			server.Logger = lg
			req := &ppb.RunRequest{
				Command: &cpb.Command{
					Args:     []string{"fake-exec"},
					ExecRoot: env.ExecRoot,
					Output: &cpb.OutputSpec{
						OutputFiles: []string{tc.testFile},
					},
					Input: &cpb.InputSpec{
						Inputs: files,
					},
				},
				Labels: map[string]string{"type": "tool"},
				ExecutionOptions: &ppb.ProxyExecutionOptions{
					ExecutionStrategy: ppb.ExecutionStrategy_REMOTE,
					RemoteExecutionOptions: &ppb.RemoteExecutionOptions{
						DownloadOutputs:              true,
						PreserveUnchangedOutputMtime: true,
					},
					LogEnvironment:  true,
					ReclientTimeout: 3600,
				},
			}
			wantCmd := &command.Command{
				Identifiers: &command.Identifiers{},
				Args:        []string{"fake-exec"},
				ExecRoot:    env.ExecRoot,
				InputSpec: &command.InputSpec{
					Inputs: files,
				},
				OutputFiles: []string{tc.testFile},
			}
			setPlatformOSFamily(wantCmd)
			res := &command.Result{Status: command.SuccessResultStatus}
			env.Set(wantCmd, command.DefaultExecutionOptions(), res, &fakes.OutputFile{tc.testFile, tc.wantOutput})
			var oldDigest digest.Digest
			var oldMtime time.Time
			if tc.origOutput != "" {
				time.Sleep(time.Second) // Wait for mtime to catch up
				oldDigest, oldMtime = getFileInfo(t, path)
			}
			if _, err := server.RunCommand(ctx, req); err != nil {
				t.Errorf("RunCommand() returned error: %v", err)
			}
			server.DrainAndReleaseResources()
			time.Sleep(time.Second) // Wait for mtime to catch up
			newDigest, newMtime := getFileInfo(t, path)
			if !tc.wantChanged {
				if oldMtime != newMtime {
					t.Errorf("Mtime updated when output is unchanged. Old Mtime: %v, New Mtime: %v",
						oldMtime, newMtime)
				}
				if oldDigest != newDigest {
					t.Errorf("Digest changed but output should be unchanged. Old Digest: %v, New Digest: %v",
						oldDigest, newDigest)
				}
			} else {
				if oldMtime == newMtime {
					t.Errorf("Mtime not updated when output is changed. Old Mtime: %v, New Mtime: %v",
						oldMtime, newMtime)
				}
				if oldDigest == newDigest {
					t.Errorf("Digest not changed but output should be changed. Old Digest: %v, New Digest: %v",
						oldDigest, newDigest)
				}
			}
			// TODO (b/260219409): Logical and real bytes downloaded should actually be 0 but
			// they aren't for some reason. To be investigated, fixed and then added a check for here.
			recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
			if err != nil {
				t.Errorf("logger.ParseFromLogDirs failed: %v", err)
			}
			wantBytes := 0
			if tc.wantChanged {
				wantBytes = len(tc.wantOutput)
			}
			wantRecs := []*lpb.LogRecord{
				{
					Command:          command.ToProto(wantCmd),
					Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
					CompletionStatus: lpb.CompletionStatus_STATUS_REMOTE_EXECUTION,
					LocalMetadata: &lpb.LocalMetadata{
						UpdatedCache: false,
						Labels:       map[string]string{"type": "tool"},
					},
					RemoteMetadata: &lpb.RemoteMetadata{
						Result:              &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
						NumInputDirectories: tc.wantNumDirs,
						NumInputFiles:       int32(len(files)),
						NumOutputFiles:      1,
						TotalOutputBytes:    int64(len(tc.wantOutput)),
						OutputFileDigests: map[string]string{
							tc.testFile: digest.NewFromBlob([]byte(tc.wantOutput)).String(),
						},
						LogicalBytesDownloaded: int64(wantBytes),
					},
				},
			}
			opts := []cmp.Option{
				protocmp.IgnoreFields(&cpb.Command{}, "identifiers"),
				protocmp.IgnoreFields(&cpb.CommandResult{}, "msg"),
				protocmp.IgnoreFields(&lpb.RemoteMetadata{}, "action_digest", "command_digest", "total_input_bytes", "real_bytes_uploaded", "real_bytes_downloaded", "event_times", "stderr_digest", "stdout_digest"),
				protocmp.IgnoreFields(&lpb.LocalMetadata{}, "event_times"),
				protocmp.IgnoreFields(&lpb.RerunMetadata{}, "event_times"),
				protocmp.IgnoreFields(&lpb.Verification_Mismatch{}, "action_digest"),
				protocmp.SortRepeated(func(a, b string) bool { return a < b }),
				protocmp.Transform(),
			}
			if diff := cmp.Diff(wantRecs, recs, opts...); diff != "" {
				t.Fatalf("Server logs returned diff in result: (-want +got)\n%s", diff)
			}
		})
	}
}

// Test that response contains a LogRecord when IncludeActionLog is requested.
func TestRemoteWithSingleActionLog(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	files := []string{"foo.h", "bar.h", executablePath}
	execroot.AddFiles(t, env.ExecRoot, files)
	ds := &stubCPPDependencyScanner{
		processInputsReturnValue: []string{
			"foo.h",
			"bar.h",
		},
	}
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
		FileMetadataStore: fmc,
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(ds, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	ctx := context.Background()
	st := time.Now()
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     []string{executablePath, "-c", "c"},
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels: map[string]string{"type": "compile", "lang": "cpp", "compiler": "clang"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{
			ExecutionStrategy: ppb.ExecutionStrategy_REMOTE,
			LogEnvironment:    true,
			IncludeActionLog:  true, // ask for a full LogRecord in the response
			ReclientTimeout:   3600,
		},
		Metadata: &ppb.Metadata{
			EventTimes:  map[string]*cpb.TimeInterval{"Event": {From: command.TimeToProto(st)}},
			Environment: []string{"FOO=1", "BAR=2"},
		},
	}
	wantCmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        []string{executablePath, "-c", "c"},
		ExecRoot:    env.ExecRoot,
		InputSpec: &command.InputSpec{
			Inputs: []string{executablePath, "foo.h", "bar.h"},
		},
		OutputFiles: []string{abOutPath},
	}
	setPlatformOSFamily(wantCmd)
	res := &command.Result{Status: command.CacheHitResultStatus}
	wantStdErr := []byte("stderr")
	env.Set(wantCmd, command.DefaultExecutionOptions(), res, fakes.StdErr(wantStdErr), &fakes.OutputFile{abOutPath, "output"})

	wantRec := &lpb.LogRecord{
		Command:          command.ToProto(wantCmd),
		Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
		CompletionStatus: lpb.CompletionStatus_STATUS_CACHE_HIT,
		RemoteMetadata: &lpb.RemoteMetadata{
			Result:              &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
			NumInputFiles:       3,
			NumInputDirectories: 1,
			NumOutputFiles:      1,
			TotalOutputBytes:    12, // "output" + "stderr"
			OutputFileDigests: map[string]string{
				abOutPath: digest.NewFromBlob([]byte("output")).String(),
			},
		},
		LocalMetadata: &lpb.LocalMetadata{
			Environment: map[string]string{"FOO": "1", "BAR": "2"},
			Labels:      map[string]string{"type": "compile", "lang": "cpp", "compiler": "clang"},
		},
	}

	got, err := server.RunCommand(ctx, req)
	server.DrainAndReleaseResources()
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	if diff := cmp.Diff(wantRec, got.GetActionLog(), cmpLogRecordsOpts...); diff != "" {
		t.Errorf("RunCommand() returned diff in log: (-want +got)\n%s", diff)
	}
	path := filepath.Join(env.ExecRoot, abOutPath)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("Error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, []byte("output")) {
		t.Errorf("Expected %s to contain \"output\", got %v", path, contents)
	}
}

func TestNoRemoteOnInputFail(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	files := []string{"foo.h", "bar.h", executablePath}
	execroot.AddFiles(t, env.ExecRoot, files)
	ds := &stubCPPDependencyScanner{
		processInputsReturnValue: []string{
			"foo.h",
			"bar.h",
		},
		processInputsError: errors.New("Input process error"),
	}
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
		FileMetadataStore: fmc,
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(ds, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	ctx := context.Background()
	st := time.Now()
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     []string{executablePath, "-c", "c"},
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels:           map[string]string{"type": "compile", "lang": "cpp", "compiler": "clang"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_REMOTE, ReclientTimeout: 3600},
		Metadata: &ppb.Metadata{
			EventTimes:  map[string]*cpb.TimeInterval{"Event": {From: command.TimeToProto(st)}},
			Environment: []string{"FOO=1", "BAR=2"},
		},
	}
	server.RunCommand(ctx, req)
	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	rec := recs[0]
	if rec.RemoteMetadata != nil {
		t.Fatal("Remote execution should not be done when input processing has failed")
	}
}

func TestLERCNoDeps(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	executor := &subprocess.SystemExecutor{}
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		LocalPool:         NewLocalPool(executor, resMgr),
		FileMetadataStore: fmc,
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	var cmdArgs []string
	var wantOutput []byte
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("mkdir %s && echo hello>%s", abPath, abOutPath)}
		wantOutput = []byte("hello\r\n")
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprintf("mkdir -p %s && echo hello > %s", abPath, abOutPath)}
		wantOutput = []byte("hello\n")
	}
	ctx := context.Background()
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels: map[string]string{"type": "tool", "shallow": "true"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{
			ExecutionStrategy: ppb.ExecutionStrategy_LOCAL,
			LocalExecutionOptions: &ppb.LocalExecutionOptions{
				AcceptCached: true,
			},
			ReclientTimeout: 3600,
		},
	}

	// The first execution will be a cache miss, and will execute locally.
	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{Result: &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS}}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	path := filepath.Join(env.ExecRoot, abOutPath)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf(`RunCommand output %s: %q; want %q`, path, contents, wantOutput)
	}

	os.Remove(path)

	t.Logf("The second execution will be a remote cache hit.")
	got, err = server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want = &ppb.RunResponse{Result: &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT}}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	contents, err = os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf(`RunCommand output %s: %q; want %q`, path, contents, wantOutput)
	}

	outDg := digest.NewFromBlob(contents)

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	wantCmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		InputSpec:   &command.InputSpec{},
		ExecRoot:    env.ExecRoot,
		OutputFiles: []string{abOutPath},
		Platform:    map[string]string{platformVersionKey: version.CurrentVersion()},
	}
	setPlatformOSFamily(wantCmd)
	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(wantCmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_LOCAL_EXECUTION,
			RemoteMetadata: &lpb.RemoteMetadata{
				NumInputDirectories: 1,
				NumOutputFiles:      1,
				TotalOutputBytes:    int64(len(wantOutput)),
				OutputFileDigests:   map[string]string{filepath.Clean(abOutPath): outDg.String()},
			},
			LocalMetadata: &lpb.LocalMetadata{
				Result:          &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
				ExecutedLocally: true,
				UpdatedCache:    true,
				Labels:          map[string]string{"type": "tool", "shallow": "true"},
			},
		},
		{
			Command:          command.ToProto(wantCmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
			CompletionStatus: lpb.CompletionStatus_STATUS_CACHE_HIT,
			RemoteMetadata: &lpb.RemoteMetadata{
				Result:              &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
				CacheHit:            true,
				NumInputDirectories: 1,
				NumOutputFiles:      1,
				TotalOutputBytes:    int64(len(wantOutput)),
				OutputFileDigests:   map[string]string{filepath.Clean(abOutPath): outDg.String()},
			},
			LocalMetadata: &lpb.LocalMetadata{
				ValidCacheHit: true,
				Labels:        map[string]string{"type": "tool", "shallow": "true"},
			},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Errorf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestLERC_UsesActionEnvironmentVariables(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	executor := &subprocess.SystemExecutor{}
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		LocalPool:         NewLocalPool(executor, resMgr),
		FileMetadataStore: fmc,
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	var cmdArgs []string
	var wantOutput []byte
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("mkdir %s && echo %%FOO%%>%s", abPath, abOutPath)}
		wantOutput = []byte("foo\r\n")
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprintf("mkdir -p %s && echo $FOO > %s", abPath, abOutPath)}
		wantOutput = []byte("foo\n")
	}
	ctx := context.Background()
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels: map[string]string{"type": "tool", "shallow": "true"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{
			ExecutionStrategy: ppb.ExecutionStrategy_LOCAL,
			LocalExecutionOptions: &ppb.LocalExecutionOptions{
				AcceptCached: true,
			},
			ReclientTimeout: 3600,
		},
		Metadata: &ppb.Metadata{
			Environment: []string{"FOO=foo"},
		},
	}

	// The first execution will be a cache miss, and will execute locally.
	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{Result: &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS}}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	path := filepath.Join(env.ExecRoot, abOutPath)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf(`RunCommand output %s: %q; want %q`, path, contents, wantOutput)
	}

	os.Remove(path)
	os.RemoveAll(filepath.Join(env.ExecRoot, aDir))

	// Even when the local execution environment changes, the cache hit shouldn't change
	// unless the variable is specified in env_var_allowlist flag.
	req.Metadata = &ppb.Metadata{
		Environment: []string{"FOO=bar"},
	}
	t.Logf("The second execution will be a remote cache hit.")
	got, err = server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want = &ppb.RunResponse{Result: &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT}}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	contents, err = os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf(`RunCommand output %s: %q; want %q`, path, contents, wantOutput)
	}

	outDg := digest.NewFromBlob(contents)

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	wantCmd1 := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		InputSpec:   &command.InputSpec{},
		ExecRoot:    env.ExecRoot,
		OutputFiles: []string{abOutPath},
		Platform:    map[string]string{platformVersionKey: version.CurrentVersion()},
	}
	wantCmd2 := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		InputSpec:   &command.InputSpec{},
		ExecRoot:    env.ExecRoot,
		OutputFiles: []string{abOutPath},
		Platform:    map[string]string{platformVersionKey: version.CurrentVersion()},
	}
	setPlatformOSFamily(wantCmd1)
	setPlatformOSFamily(wantCmd2)
	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(wantCmd1),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_LOCAL_EXECUTION,
			RemoteMetadata: &lpb.RemoteMetadata{
				NumInputDirectories: 1,
				NumOutputFiles:      1,
				TotalOutputBytes:    int64(len(wantOutput)),
				OutputFileDigests:   map[string]string{filepath.Clean(abOutPath): outDg.String()},
			},
			LocalMetadata: &lpb.LocalMetadata{
				Result:          &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
				ExecutedLocally: true,
				UpdatedCache:    true,
				Labels:          map[string]string{"type": "tool", "shallow": "true"},
			},
		},
		{
			Command:          command.ToProto(wantCmd2),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
			CompletionStatus: lpb.CompletionStatus_STATUS_CACHE_HIT,
			RemoteMetadata: &lpb.RemoteMetadata{
				Result:              &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
				CacheHit:            true,
				NumInputDirectories: 1,
				NumOutputFiles:      1,
				TotalOutputBytes:    int64(len(wantOutput)),
				OutputFileDigests:   map[string]string{filepath.Clean(abOutPath): outDg.String()},
			},
			LocalMetadata: &lpb.LocalMetadata{
				ValidCacheHit: true,
				Labels:        map[string]string{"type": "tool", "shallow": "true"},
			},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Errorf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestLERC_ChangeInAllowlistedEnvVariablesCausesInvalidation(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	executor := &subprocess.SystemExecutor{}
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		LocalPool:         NewLocalPool(executor, resMgr),
		FileMetadataStore: fmc,
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	var cmdArgs []string
	var wantOutput []byte
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("mkdir %s && echo %%FOO%%>%s", abPath, abOutPath)}
		wantOutput = []byte("foo\r\n")
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprintf("mkdir -p %s && echo $FOO > %s", abPath, abOutPath)}
		wantOutput = []byte("foo\n")
	}
	ctx := context.Background()
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
			Input: &cpb.InputSpec{
				EnvironmentVariables: map[string]string{"BAR": "bar"},
			},
		},
		Labels: map[string]string{"type": "tool", "shallow": "true"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{
			ExecutionStrategy: ppb.ExecutionStrategy_LOCAL,
			LocalExecutionOptions: &ppb.LocalExecutionOptions{
				AcceptCached: true,
			},
			ReclientTimeout: 3600,
		},
		Metadata: &ppb.Metadata{
			Environment: []string{"FOO=foo"},
		},
	}

	// The first execution will be a cache miss, and will execute locally.
	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{Result: &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS}}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	path := filepath.Join(env.ExecRoot, abOutPath)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf(`RunCommand output %s: %q; want %q`, path, contents, wantOutput)
	}
	contents, err = os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf(`RunCommand output %s: %q; want %q`, path, contents, wantOutput)
	}
	outDg1 := digest.NewFromBlob(contents)

	os.Remove(path)
	os.RemoveAll(filepath.Join(env.ExecRoot, aDir))

	// Even when the local execution environment changes, the cache hit shouldn't change
	// unless the variable is specified in env_var_allowlist flag.
	req.Metadata = &ppb.Metadata{
		Environment: []string{"FOO=bar"},
	}
	if runtime.GOOS == "windows" {
		wantOutput = []byte("bar\r\n")
	} else {
		wantOutput = []byte("bar\n")
	}
	req.Command.Input.EnvironmentVariables = map[string]string{"BAR": "bar2"}
	t.Logf("The second execution will NOT be a remote cache hit.")
	got, err = server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want = &ppb.RunResponse{Result: &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS}}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s, gotStderr: %s", diff, got.Stderr)
	}
	contents, err = os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf(`RunCommand output %s: %q; want %q`, path, contents, wantOutput)
	}

	outDg2 := digest.NewFromBlob(contents)

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	wantCmd1 := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		InputSpec: &command.InputSpec{
			EnvironmentVariables: map[string]string{
				"BAR": "bar",
			},
		},
		ExecRoot:    env.ExecRoot,
		OutputFiles: []string{abOutPath},
		Platform:    map[string]string{platformVersionKey: version.CurrentVersion()},
	}
	wantCmd2 := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		InputSpec: &command.InputSpec{
			EnvironmentVariables: map[string]string{
				"BAR": "bar2",
			},
		},
		ExecRoot:    env.ExecRoot,
		OutputFiles: []string{abOutPath},
		Platform:    map[string]string{platformVersionKey: version.CurrentVersion()},
	}
	setPlatformOSFamily(wantCmd1)
	setPlatformOSFamily(wantCmd2)
	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(wantCmd1),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_LOCAL_EXECUTION,
			RemoteMetadata: &lpb.RemoteMetadata{
				NumInputDirectories: 1,
				NumOutputFiles:      1,
				TotalOutputBytes:    int64(len(wantOutput)),
				OutputFileDigests:   map[string]string{filepath.Clean(abOutPath): outDg1.String()},
			},
			LocalMetadata: &lpb.LocalMetadata{
				Result:          &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
				ExecutedLocally: true,
				UpdatedCache:    true,
				Labels:          map[string]string{"type": "tool", "shallow": "true"},
			},
		},
		{
			Command:          command.ToProto(wantCmd2),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_LOCAL_EXECUTION,
			RemoteMetadata: &lpb.RemoteMetadata{
				NumInputDirectories: 1,
				NumOutputFiles:      1,
				TotalOutputBytes:    int64(len(wantOutput)),
				OutputFileDigests:   map[string]string{filepath.Clean(abOutPath): outDg2.String()},
			},
			LocalMetadata: &lpb.LocalMetadata{
				Result:          &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
				ExecutedLocally: true,
				UpdatedCache:    true,
				Labels:          map[string]string{"type": "tool", "shallow": "true"},
			},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Errorf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestLERCNoDeps_CanonicalWorkingDir(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	executor := &subprocess.SystemExecutor{}
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		LocalPool:         NewLocalPool(executor, resMgr),
		FileMetadataStore: fmc,
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	wd := filepath.Join("some", "wd")
	absWD := filepath.Join(env.ExecRoot, wd)
	if err := os.MkdirAll(absWD, 0744); err != nil {
		t.Errorf("Unable to create working dir %v: %v", absWD, err)
	}
	wdABOutPath := filepath.Join(wd, abOutPath)
	server.Logger = lg
	var cmdArgs []string
	var wantOutput []byte
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("mkdir %s && echo hello>%s", abPath, abOutPath)}
		wantOutput = []byte("hello\r\n")
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprintf("mkdir -p %s && echo hello > %s", abPath, abOutPath)}
		wantOutput = []byte("hello\n")
	}
	ctx := context.Background()
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:             cmdArgs,
			ExecRoot:         env.ExecRoot,
			WorkingDirectory: wd,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{wdABOutPath},
			},
		},
		Labels: map[string]string{"type": "tool", "shallow": "true"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{
			ExecutionStrategy: ppb.ExecutionStrategy_LOCAL,
			LocalExecutionOptions: &ppb.LocalExecutionOptions{
				AcceptCached: true,
			},
			RemoteExecutionOptions: &ppb.RemoteExecutionOptions{
				AcceptCached:                 true,
				DoNotCache:                   false,
				DownloadOutputs:              true,
				PreserveUnchangedOutputMtime: false,
				CanonicalizeWorkingDir:       true,
			},
			ReclientTimeout: 3600,
		},
	}

	// The first execution will be a cache miss, and will execute locally.
	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{Result: &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS}}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	path := filepath.Join(env.ExecRoot, wdABOutPath)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf(`RunCommand output %s: %q; want %q`, path, contents, wantOutput)
	}

	os.Remove(path)

	t.Logf("The second execution will be a remote cache hit.")
	got, err = server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want = &ppb.RunResponse{Result: &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT}}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	contents, err = os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf(`RunCommand output %s: %q; want %q`, path, contents, wantOutput)
	}

	outDg := digest.NewFromBlob(contents)

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	wantCmd := &command.Command{
		Identifiers:      &command.Identifiers{},
		Args:             cmdArgs,
		InputSpec:        &command.InputSpec{},
		ExecRoot:         env.ExecRoot,
		WorkingDir:       wd,
		RemoteWorkingDir: toRemoteWorkingDir(wd),
		OutputFiles:      []string{abOutPath},
		Platform:         map[string]string{platformVersionKey: version.CurrentVersion()},
	}
	setPlatformOSFamily(wantCmd)
	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(wantCmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_LOCAL_EXECUTION,
			RemoteMetadata: &lpb.RemoteMetadata{
				NumInputDirectories: 1,
				NumOutputFiles:      1,
				TotalOutputBytes:    int64(len(wantOutput)),
				OutputFileDigests:   map[string]string{filepath.Clean(abOutPath): outDg.String()},
			},
			LocalMetadata: &lpb.LocalMetadata{
				Result:          &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
				ExecutedLocally: true,
				UpdatedCache:    true,
				Labels:          map[string]string{"type": "tool", "shallow": "true"},
			},
		},
		{
			Command:          command.ToProto(wantCmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
			CompletionStatus: lpb.CompletionStatus_STATUS_CACHE_HIT,
			RemoteMetadata: &lpb.RemoteMetadata{
				Result:              &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
				CacheHit:            true,
				NumInputDirectories: 1,
				NumOutputFiles:      1,
				TotalOutputBytes:    int64(len(wantOutput)),
				OutputFileDigests:   map[string]string{filepath.Clean(abOutPath): outDg.String()},
			},
			LocalMetadata: &lpb.LocalMetadata{
				ValidCacheHit: true,
				Labels:        map[string]string{"type": "tool", "shallow": "true"},
			},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Errorf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestLERCLocalFailure(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	executor := &subprocess.SystemExecutor{}
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		LocalPool:         NewLocalPool(executor, resMgr),
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
		FileMetadataStore: fmc,
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	var cmdArgs []string
	var wantOutput []byte
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("mkdir %s && echo hello>>%s && exit 1", abPath, abOutPath)}
		wantOutput = []byte("hello \r\n")
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprintf("mkdir -p %s && echo hello >> %s && exit 1", abPath, abOutPath)}
		wantOutput = []byte("hello\n")
	}
	ctx := context.Background()
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels: map[string]string{"type": "tool", "shallow": "true"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{
			ExecutionStrategy: ppb.ExecutionStrategy_LOCAL,
			LocalExecutionOptions: &ppb.LocalExecutionOptions{
				AcceptCached: true,
			},
			ReclientTimeout: 3600,
		},
	}

	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{Result: &cpb.CommandResult{
		Status:   cpb.CommandResultStatus_NON_ZERO_EXIT,
		ExitCode: 1,
	}}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	path := filepath.Join(env.ExecRoot, abOutPath)
	defer os.Remove(path)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf(`RunCommand output %s: %q; want %q`, path, contents, wantOutput)
	}

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	wantCmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		InputSpec:   &command.InputSpec{},
		ExecRoot:    env.ExecRoot,
		OutputFiles: []string{abOutPath},
		Platform:    map[string]string{platformVersionKey: version.CurrentVersion()},
	}
	setPlatformOSFamily(wantCmd)
	wantRecs := []*lpb.LogRecord{
		{
			Command: command.ToProto(wantCmd),
			Result: &cpb.CommandResult{
				Status:   cpb.CommandResultStatus_NON_ZERO_EXIT,
				ExitCode: 1,
			},
			CompletionStatus: lpb.CompletionStatus_STATUS_NON_ZERO_EXIT,
			RemoteMetadata: &lpb.RemoteMetadata{
				NumInputDirectories: 1,
			},
			LocalMetadata: &lpb.LocalMetadata{
				Result: &cpb.CommandResult{
					Status:   cpb.CommandResultStatus_NON_ZERO_EXIT,
					ExitCode: 1,
				},
				ExecutedLocally: true,
				Labels:          map[string]string{"type": "tool", "shallow": "true"},
			},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Errorf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestLERCNoDeps_NoAcceptCached(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	executor := &subprocess.SystemExecutor{}
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		LocalPool:         NewLocalPool(executor, resMgr),
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
		FileMetadataStore: fmc,
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	var cmdArgs []string
	var wantOutput []byte
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("mkdir %s && echo hello>%s", abPath, abOutPath)}
		wantOutput = []byte("hello\r\n")
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprintf("mkdir -p %s && echo hello > %s", abPath, abOutPath)}
		wantOutput = []byte("hello\n")
	}
	ctx := context.Background()
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels: map[string]string{"type": "tool", "shallow": "true"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{
			ExecutionStrategy: ppb.ExecutionStrategy_LOCAL,
			LocalExecutionOptions: &ppb.LocalExecutionOptions{
				AcceptCached: false,
			},
			ReclientTimeout: 3600,
		},
	}

	// The first execution will be a cache miss, and will execute locally.
	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{Result: &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS}}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	path := filepath.Join(env.ExecRoot, abOutPath)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, wantOutput)
	}

	os.RemoveAll(filepath.Join(env.ExecRoot, "a"))

	t.Logf("The second execution should ignore the cache since AcceptedCached is set to false.")
	got, err = server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want = &ppb.RunResponse{Result: &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS}}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	contents, err = os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, wantOutput)
	}

	outDg := digest.NewFromBlob(contents)

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	wantCmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		InputSpec:   &command.InputSpec{},
		ExecRoot:    env.ExecRoot,
		OutputFiles: []string{abOutPath},
		Platform:    map[string]string{platformVersionKey: version.CurrentVersion()},
	}
	setPlatformOSFamily(wantCmd)
	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(wantCmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_LOCAL_EXECUTION,
			RemoteMetadata: &lpb.RemoteMetadata{
				NumInputDirectories: 1,
				NumOutputFiles:      1,
				TotalOutputBytes:    int64(len(wantOutput)),
				OutputFileDigests:   map[string]string{filepath.Clean(abOutPath): outDg.String()},
			},
			LocalMetadata: &lpb.LocalMetadata{
				Result:          &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
				ExecutedLocally: true,
				UpdatedCache:    true,
				Labels:          map[string]string{"type": "tool", "shallow": "true"},
			},
		},
		{
			Command:          command.ToProto(wantCmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_LOCAL_EXECUTION,
			RemoteMetadata: &lpb.RemoteMetadata{
				CacheHit:            false,
				NumInputDirectories: 1,
				NumOutputFiles:      1,
				TotalOutputBytes:    int64(len(wantOutput)),
				OutputFileDigests:   map[string]string{filepath.Clean(abOutPath): outDg.String()},
			},
			LocalMetadata: &lpb.LocalMetadata{
				Result:          &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
				ValidCacheHit:   false,
				Labels:          map[string]string{"type": "tool", "shallow": "true"},
				ExecutedLocally: true,
				UpdatedCache:    true,
			},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Errorf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
}

func createDFile(t *testing.T, execRoot, oFile string, inputs ...string) {
	t.Helper()
	execroot.AddFileWithContent(t, filepath.Join(execRoot, "foo.d"), []byte(getDFileContents(t, oFile, inputs...)))
}

func getDFileContents(t *testing.T, oFile string, inputs ...string) string {
	t.Helper()
	dFileContent := oFile + ": \\\n"
	for _, i := range inputs {
		dFileContent += i + " \\\n"
	}
	return dFileContent
}

func getDepsFileContents(t *testing.T, execRoot string, fmc filemetadata.Cache, shadowHeaders bool, oFile string, inputDirs []string, inputs ...string) string {
	t.Helper()
	createDFile(t, execRoot, oFile, inputs...)
	p := &deps.Parser{ExecRoot: execRoot, DigestStore: fmc}
	depsFileContent, err := p.GetDeps("foo.d")
	if err != nil {
		t.Fatalf("GetDeps() failed with error: %v", err)
	}
	os.Remove(filepath.Join(execRoot, "foo.d"))
	return depsFileContent
}

func TestLERCNonShallowValidCacheHit(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skipf("windows doesn't have clang++")
		return
	}
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	fileContents := map[string][]byte{
		"bar.h":   []byte("bar"),
		"foo.h":   []byte("foo"),
		"clang++": []byte("fake"),
		"foo.cpp": []byte("fake"),
	}
	execroot.AddFilesWithContent(t, env.ExecRoot, fileContents)
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		FileMetadataStore: fmc,
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	cmdArgs := []string{
		"clang++",
		"-o",
		"foo.o",
		"-MF",
		"foo.d",
		"-c",
		"foo.cpp",
	}
	ctx := context.Background()
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{"foo.o"},
			},
		},
		Labels: map[string]string{"type": "compile", "lang": "cpp", "compiler": "clang"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{
			ExecutionStrategy: ppb.ExecutionStrategy_LOCAL,
			LocalExecutionOptions: &ppb.LocalExecutionOptions{
				AcceptCached: true,
			},
			ReclientTimeout: 3600,
		},
	}
	wantCmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec: &command.InputSpec{
			Inputs: []string{
				"foo.cpp",
				"clang++",
			},
		},
		OutputFiles: []string{"foo.o", "foo.d"},
		Platform:    map[string]string{platformVersionKey: version.CurrentVersion()},
	}
	setPlatformOSFamily(wantCmd)
	res := &command.Result{Status: command.CacheHitResultStatus}

	dFileContent := getDFileContents(t, "foo.o", "foo.h", "bar.h")
	fooDdigest := digest.NewFromBlob([]byte(dFileContent))
	fooOdigest := digest.NewFromBlob([]byte("foo.o"))
	env.Set(wantCmd, command.DefaultExecutionOptions(), res, &fakes.OutputFile{"foo.d", dFileContent}, &fakes.OutputFile{"foo.o", "foo.o"})
	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{Result: &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT}}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	path := filepath.Join(env.ExecRoot, "foo.o")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, []byte("foo.o")) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, "foo.o")
	}

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(wantCmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
			CompletionStatus: lpb.CompletionStatus_STATUS_CACHE_HIT,
			RemoteMetadata: &lpb.RemoteMetadata{
				Result:              &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
				CacheHit:            true,
				NumInputFiles:       2,
				NumInputDirectories: 1,
				NumOutputFiles:      2,
				TotalOutputBytes:    fooDdigest.Size + fooOdigest.Size,
				OutputFileDigests: map[string]string{
					"foo.d": fooDdigest.String(),
					"foo.o": fooOdigest.String(),
				},
			},
			LocalMetadata: &lpb.LocalMetadata{
				ValidCacheHit: true,
				Labels:        map[string]string{"type": "compile", "lang": "cpp", "compiler": "clang"},
			},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Errorf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestLERCInputAsOutput_UploadsUpdatedOutputAsCacheResult(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	executor := &subprocess.SystemExecutor{}
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		FileMetadataStore: fmc,
		LocalPool:         NewLocalPool(executor, resMgr),
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	var cmdArgs []string
	var wantOutput []byte
	var wantAfterDigest digest.Digest
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", `echo newcontent>hello`}
		wantOutput = []byte("newcontent\r\n")
	} else {
		cmdArgs = []string{"/bin/bash", "-c", "echo newcontent > hello"}
		wantOutput = []byte("newcontent\n")
	}
	wantAfterDigest = digest.NewFromBlob(wantOutput)

	ctx := context.Background()
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{"hello"},
			},
		},
		Labels: map[string]string{"type": "tool", "shallow": "true"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{
			ExecutionStrategy: ppb.ExecutionStrategy_LOCAL,
			ReclientTimeout:   3600,
		},
	}
	helloFilePath := filepath.Join(env.ExecRoot, "hello")
	helloFileContent := []byte("oldcontent\n")
	execroot.AddFilesWithContent(t, env.ExecRoot, map[string][]byte{"hello": helloFileContent})
	wantBeforeDigest := digest.NewFromBlob(helloFileContent)
	if gotBeforeDigest := fmc.Get(helloFilePath).Digest; gotBeforeDigest != wantBeforeDigest {
		t.Fatalf("Error in test setup of file %v, wantDigest %v, gotDigest %v", helloFilePath, wantBeforeDigest, gotBeforeDigest)
	}

	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{Result: &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS}}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	contents, err := os.ReadFile(helloFilePath)
	if err != nil {
		t.Errorf("error reading from %s: %v", helloFilePath, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf("RunCommand output %s: %q; want %q", helloFilePath, contents, wantOutput)
	}
	if gotAfterDigest := fmc.Get(helloFilePath).Digest; wantAfterDigest != gotAfterDigest {
		t.Errorf("Invalid digest in file metadata cache after action run, wantDigest %v, gotDigest %v", wantAfterDigest, gotAfterDigest)
	}

	// Delete the hello file and fill it with original content to get a cache-hit.
	os.RemoveAll(helloFilePath)
	fmc.Delete(helloFilePath)
	execroot.AddFilesWithContent(t, env.ExecRoot, map[string][]byte{"hello": helloFileContent})

	got, err = server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want = &ppb.RunResponse{Result: &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS}}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	contents, err = os.ReadFile(helloFilePath)
	if err != nil {
		t.Errorf("error reading from %s: %v", helloFilePath, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf("RunCommand output %s: %q; want %q", helloFilePath, contents, wantOutput)
	}
	server.DrainAndReleaseResources()
}

func TestRemoteLocalFallback_InputAsOutputClearCache(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		FileMetadataStore: fmc,
		LocalPool:         NewLocalPool(&subprocess.SystemExecutor{}, resMgr),
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})

	inOutFileName := "in-out.txt"
	inOutFilepath := filepath.Join(env.ExecRoot, inOutFileName)
	inOutContentOld := "old content"
	inOutContentNew := "new content"
	cmdArgs := []string{"/bin/bash", "-c", fmt.Sprintf("echo -n %s > %s", inOutContentNew, inOutFileName)}
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"PowerShell", "-Command", fmt.Sprintf(`Out-File -InputObject "%s" -FilePath %s -NoNewline -Encoding ASCII`, inOutContentNew, inOutFileName)}
	}
	execroot.AddFilesWithContent(t, env.ExecRoot, map[string][]byte{inOutFileName: []byte(inOutContentOld)})

	cmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec: &command.InputSpec{
			Inputs: []string{inOutFileName},
		},
		OutputFiles: []string{inOutFileName},
	}
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Input: &cpb.InputSpec{
				Inputs: []string{inOutFileName},
			},
			Output: &cpb.OutputSpec{
				OutputFiles: []string{inOutFileName},
			},
		},
		Labels: map[string]string{"type": "tool", "shallow": "true"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{
			ExecutionStrategy: ppb.ExecutionStrategy_REMOTE_LOCAL_FALLBACK,
			ReclientTimeout:   3600,
		},
	}

	res := &command.Result{Status: command.NonZeroExitResultStatus, ExitCode: 5}
	setPlatformOSFamily(cmd)
	stdErr := []byte("something happened")
	env.Set(cmd, command.DefaultExecutionOptions(), res, fakes.StdErr(stdErr))
	wantDigestBefore := digest.NewFromBlob([]byte(inOutContentOld))
	if gotDigestBefore := fmc.Get(inOutFilepath).Digest; gotDigestBefore != wantDigestBefore {
		t.Fatalf("old content digest mismatch for %q: want %q, got %q", inOutFilepath, wantDigestBefore, gotDigestBefore)
	}

	ctx := context.Background()
	gotResp, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("failed to run command: %v", err)
	}
	wantResp := &ppb.RunResponse{
		Result: &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
		RemoteFallbackInfo: &ppb.RemoteFallbackInfo{
			ExitCode: 5,
			Stderr:   stdErr,
		},
	}
	if diff := cmp.Diff(wantResp, gotResp, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("command response mismatch: (-want +got)\n%s", diff)
	}
	inOutFileBytesAfter, err := os.ReadFile(inOutFilepath)
	if err != nil {
		t.Errorf("failed to read content of file %q: %v", inOutFilepath, err)
	}
	if string(inOutFileBytesAfter) != inOutContentNew {
		t.Errorf("new content mismatch for %q: want %q, got %q", inOutFilepath, inOutContentNew, inOutFileBytesAfter)
	}
	wantDigestAfter := digest.NewFromBlob([]byte(inOutContentNew))
	if gotDigestAfter := fmc.Get(inOutFilepath).Digest; gotDigestAfter != wantDigestAfter {
		t.Errorf("new content digest mismatch for %q: want %q, got %q", inOutFilepath, wantDigestAfter, gotDigestAfter)
	}

	server.DrainAndReleaseResources()
}

func TestLERCDepsValidCacheHit(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skipf("windows doesn't have clang++")
		return
	}
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	fileContents := map[string][]byte{
		"bar.h":   []byte("bar"),
		"foo.h":   []byte("foo"),
		"clang++": []byte("fake"),
		"foo.cpp": []byte("fake"),
	}
	execroot.AddFilesWithContent(t, env.ExecRoot, fileContents)
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		FileMetadataStore: fmc,
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	cmdArgs := []string{
		"clang++",
		"-o",
		"foo.o",
		"-MF",
		"foo.d",
		"-c",
		"foo.cpp",
	}
	ctx := context.Background()
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{"foo.o"},
			},
		},
		Labels: map[string]string{"type": "compile", "lang": "cpp", "compiler": "clang", "shallow": "true"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{
			ExecutionStrategy: ppb.ExecutionStrategy_LOCAL,
			LocalExecutionOptions: &ppb.LocalExecutionOptions{
				AcceptCached: true,
			},
			ReclientTimeout: 3600,
		},
	}
	wantCmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec: &command.InputSpec{
			Inputs: []string{
				"foo.cpp",
				"clang++",
			},
		},
		OutputFiles: []string{"foo.o", "foo.d", "foo.d.deps"},
		Platform:    map[string]string{platformVersionKey: version.CurrentVersion()},
	}
	setPlatformOSFamily(wantCmd)
	res := &command.Result{Status: command.CacheHitResultStatus}

	dFileContent := getDFileContents(t, "foo.o", "foo.h", "bar.h")
	fooDdigest := digest.NewFromBlob([]byte(dFileContent))
	depsFileContent := getDepsFileContents(t, env.ExecRoot, fmc, false, "foo.o", nil, "foo.h", "bar.h")
	fooDepsdigest := digest.NewFromBlob([]byte(depsFileContent))
	fooOdigest := digest.NewFromBlob([]byte("foo.o"))
	env.Set(wantCmd, command.DefaultExecutionOptions(), res, &fakes.OutputFile{"foo.d", dFileContent}, &fakes.OutputFile{"foo.d.deps", depsFileContent}, &fakes.OutputFile{"foo.o", "foo.o"})
	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{Result: &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT}}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	path := filepath.Join(env.ExecRoot, "foo.o")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, []byte("foo.o")) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, "foo.o")
	}

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(wantCmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
			CompletionStatus: lpb.CompletionStatus_STATUS_CACHE_HIT,
			RemoteMetadata: &lpb.RemoteMetadata{
				Result:              &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
				CacheHit:            true,
				NumInputFiles:       2,
				NumInputDirectories: 1,
				NumOutputFiles:      3,
				TotalOutputBytes:    fooDdigest.Size + fooOdigest.Size + fooDepsdigest.Size,
				OutputFileDigests: map[string]string{
					"foo.d":      fooDdigest.String(),
					"foo.o":      fooOdigest.String(),
					"foo.d.deps": fooDepsdigest.String(),
				},
			},
			LocalMetadata: &lpb.LocalMetadata{
				ValidCacheHit: true,
				Labels:        map[string]string{"type": "compile", "lang": "cpp", "compiler": "clang", "shallow": "true"},
			},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Errorf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestLERCDepsInvalidCacheHit(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skipf("windows doesn't have clang++")
		return
	}
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	fileContents := map[string][]byte{
		"bar.h":   []byte("bar"),
		"foo.h":   []byte("foo"),
		"clang++": []byte("fake"),
		"foo.cpp": []byte("fake"),
	}
	execroot.AddFilesWithContent(t, env.ExecRoot, fileContents)
	executor := &execStub{localExec: func() {
		createDFile(t, env.ExecRoot, "foo.o", "foo.h", "bar.h")
	}}
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		FileMetadataStore: fmc,
		LocalPool:         NewLocalPool(executor, resMgr),
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	cmdArgs := []string{
		"clang++",
		"-o",
		"foo.o",
		"-MF",
		"foo.d",
		"-c",
		"foo.cpp",
	}
	ctx := context.Background()
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{"foo.o"},
			},
		},
		Labels: map[string]string{"type": "compile", "lang": "cpp", "compiler": "clang", "shallow": "true"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{
			ExecutionStrategy: ppb.ExecutionStrategy_LOCAL,
			LocalExecutionOptions: &ppb.LocalExecutionOptions{
				AcceptCached: true,
			},
			ReclientTimeout: 3600,
		},
	}
	wantCmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec: &command.InputSpec{
			Inputs: []string{
				"foo.cpp",
				"clang++",
			},
		},
		OutputFiles: []string{"foo.d", "foo.d.deps", "foo.o"},
		Platform:    map[string]string{platformVersionKey: version.CurrentVersion()},
	}
	setPlatformOSFamily(wantCmd)
	res := &command.Result{Status: command.CacheHitResultStatus}

	depsFileContent := getDepsFileContents(t, env.ExecRoot, fmc, false, "foo.o", nil, "foo.h", "bar.h")
	dFileContent := getDFileContents(t, "foo.o", "foo.h", "bar.h")
	fooDdigest := digest.NewFromBlob([]byte(dFileContent))
	fooOdigest := digest.NewFromBlob([]byte("foo.o"))
	env.Set(wantCmd, command.DefaultExecutionOptions(), res, &fakes.OutputFile{"foo.d", dFileContent}, &fakes.OutputFile{"foo.d.deps", depsFileContent}, &fakes.OutputFile{"foo.o", "foo.o"})

	t.Logf("Invalidate the hidden bar.h input")
	time.Sleep(time.Second) // Hack to let mtime catch up.
	execroot.AddFileWithContent(t, filepath.Join(env.ExecRoot, "bar.h"), []byte("wrong"))
	filemetadata.ResetGlobalCache()
	depsFile2Content := getDepsFileContents(t, env.ExecRoot, fmc, false, "foo.o", nil, "foo.h", "bar.h")
	fooDepsdigest := digest.NewFromBlob([]byte(depsFile2Content))

	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}

	want := &ppb.RunResponse{Result: &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS}}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	path := filepath.Join(env.ExecRoot, "foo.o")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, []byte("foo.o")) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, "foo.o")
	}

	t.Logf("The next execution will be a cache hit.")
	os.Remove(path)
	got, err = server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want = &ppb.RunResponse{Result: &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT}}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	contents, err = os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, []byte("foo.o")) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, "foo.o")
	}

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(wantCmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_LOCAL_EXECUTION,
			RemoteMetadata: &lpb.RemoteMetadata{
				Result:              &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
				NumInputFiles:       2,
				NumInputDirectories: 1,
				CacheHit:            true,
				NumOutputFiles:      3,
				TotalOutputBytes:    fooDdigest.Size + fooOdigest.Size + fooDepsdigest.Size,
				OutputFileDigests: map[string]string{
					"foo.d":      fooDdigest.String(),
					"foo.o":      fooOdigest.String(),
					"foo.d.deps": fooDepsdigest.String(),
				},
			},
			LocalMetadata: &lpb.LocalMetadata{
				Result:          &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
				ValidCacheHit:   false,
				ExecutedLocally: true,
				UpdatedCache:    true,
				Labels:          map[string]string{"type": "compile", "lang": "cpp", "compiler": "clang", "shallow": "true"},
			},
		},
		{
			Command:          command.ToProto(wantCmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
			CompletionStatus: lpb.CompletionStatus_STATUS_CACHE_HIT,
			RemoteMetadata: &lpb.RemoteMetadata{
				Result:              &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
				NumInputFiles:       2,
				NumInputDirectories: 1,
				CacheHit:            true,
				NumOutputFiles:      3,
				TotalOutputBytes:    fooDdigest.Size + fooOdigest.Size + fooDepsdigest.Size,
				OutputFileDigests: map[string]string{
					"foo.d":      fooDdigest.String(),
					"foo.o":      fooOdigest.String(),
					"foo.d.deps": fooDepsdigest.String(),
				},
			},
			LocalMetadata: &lpb.LocalMetadata{
				ValidCacheHit: true,
				Labels:        map[string]string{"type": "compile", "lang": "cpp", "compiler": "clang", "shallow": "true"},
			},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Errorf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestLERCMismatches(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skipf("windows doesn't have clang++")
		return
	}
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	fileContents := map[string][]byte{
		"bar.h":   []byte("bar"),
		"foo.h":   []byte("foo"),
		"clang++": []byte("fake"),
		"foo.cpp": []byte("fake"),
	}
	execroot.AddFilesWithContent(t, env.ExecRoot, fileContents)
	executor := &execStub{localExec: func() {
		time.Sleep(time.Second) // Hack to let mtime catch up.
		execroot.AddFileWithContent(t, filepath.Join(env.ExecRoot, "foo.o"), []byte("foo.o"))
		createDFile(t, env.ExecRoot, "foo.o", "foo.h", "bar.h")
	}}
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		FileMetadataStore: fmc,
		LocalPool:         NewLocalPool(executor, resMgr),
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	cmdArgs := []string{
		"clang++",
		"-o",
		"foo.o",
		"-MF",
		"foo.d",
		"-c",
		"foo.cpp",
	}
	ctx := context.Background()
	remoteExecOptions := &ppb.RemoteExecutionOptions{AcceptCached: false, DoNotCache: true}
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{"foo.o"},
			},
		},
		Labels: map[string]string{"type": "compile", "lang": "cpp", "compiler": "clang", "shallow": "true"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{
			ExecutionStrategy:      ppb.ExecutionStrategy_LOCAL,
			RemoteExecutionOptions: remoteExecOptions,
			LocalExecutionOptions: &ppb.LocalExecutionOptions{
				AcceptCached: true,
			},
			CompareWithLocal: true,
			ReclientTimeout:  3600,
			NumLocalReruns:   1,
			NumRemoteReruns:  1,
		},
	}
	wantCmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec: &command.InputSpec{
			Inputs: []string{
				"foo.cpp",
				"clang++",
			},
		},
		OutputFiles: []string{"foo.d", "foo.d.deps", "foo.o"},
		Platform:    map[string]string{platformVersionKey: version.CurrentVersion()},
	}
	setPlatformOSFamily(wantCmd)
	res := &command.Result{Status: command.CacheHitResultStatus}

	depsFileContent := getDepsFileContents(t, env.ExecRoot, fmc, false, "foo.o", nil, "foo.h", "bar.h")
	dFileContent := getDFileContents(t, "foo.o", "foo.h", "bar.h")
	fooDdigest := digest.NewFromBlob([]byte(dFileContent))
	fooDepsdigest := digest.NewFromBlob([]byte(depsFileContent))
	fooOdigest := digest.NewFromBlob([]byte("foo.o"))
	fooORemoteDigest := digest.NewFromBlob([]byte("surprize!"))
	executionOptions := command.DefaultExecutionOptions()
	executionOptions.DoNotCache = true
	env.Set(wantCmd, executionOptions, res, &fakes.OutputFile{"foo.d", dFileContent}, &fakes.OutputFile{"foo.d.deps", depsFileContent}, &fakes.OutputFile{"foo.o", "surprize!"})

	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}

	want := &ppb.RunResponse{Result: &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS}}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	path := filepath.Join(env.ExecRoot, "foo.o")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, []byte("foo.o")) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, "foo.o")
	}

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(wantCmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_LOCAL_EXECUTION,
			RemoteMetadata: &lpb.RemoteMetadata{
				NumInputFiles:       2,
				NumInputDirectories: 1,
				RerunMetadata: []*lpb.RerunMetadata{
					{
						Attempt: 1,
						Result:  &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
						OutputFileDigests: map[string]string{
							"foo.d":      fooDdigest.String(),
							"foo.o":      fooORemoteDigest.String(),
							"foo.d.deps": fooDepsdigest.String(),
						},
						NumOutputFiles:   3,
						TotalOutputBytes: 180,
					},
				},
			},
			LocalMetadata: &lpb.LocalMetadata{
				Result:          &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
				ExecutedLocally: true,
				UpdatedCache:    true,
				Labels:          map[string]string{"type": "compile", "lang": "cpp", "compiler": "clang", "shallow": "true"},
				Verification: &lpb.Verification{
					TotalMismatches: 1,
					Mismatches: []*lpb.Verification_Mismatch{
						{
							Path:          "foo.o",
							RemoteDigests: []string{fooORemoteDigest.String()},
							LocalDigests:  []string{fooOdigest.String()},
							Determinism:   lpb.DeterminismStatus_UNKNOWN,
						},
					},
					TotalVerified: 3,
				},
				RerunMetadata: []*lpb.RerunMetadata{
					{
						Attempt: 1,
						Result:  &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
						OutputFileDigests: map[string]string{
							"foo.d":      fooDdigest.String(),
							"foo.o":      fooOdigest.String(),
							"foo.d.deps": fooDepsdigest.String(),
						},
					},
				},
			},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Errorf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestCompareStashRestore(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	fileContents := map[string][]byte{
		"tool":  []byte("fake"),
		"inout": []byte("foo"),
	}
	execroot.AddFilesWithContent(t, env.ExecRoot, fileContents)
	executor := &execStub{localExec: func() {
		time.Sleep(time.Second) // Hack to let mtime catch up.
		content, err := os.ReadFile(filepath.Join(env.ExecRoot, "inout"))
		if err != nil {
			t.Errorf("execStub read input: %v", err)
		}
		execroot.AddFileWithContent(t, filepath.Join(env.ExecRoot, "inout"), append(content, []byte("baz")...))
	}}
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		FileMetadataStore: fmc,
		LocalPool:         NewLocalPool(executor, resMgr),
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(nil, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	t.Cleanup(server.DrainAndReleaseResources)
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	cmdArgs := []string{
		"tool",
		"inout",
	}
	ctx := context.Background()
	remoteExecOptions := &ppb.RemoteExecutionOptions{AcceptCached: false, DoNotCache: true}
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Input: &cpb.InputSpec{
				Inputs: []string{"inout"},
			},
			Output: &cpb.OutputSpec{
				OutputFiles: []string{"inout"},
			},
		},
		Labels: map[string]string{"type": "tool"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{
			ExecutionStrategy:      ppb.ExecutionStrategy_REMOTE,
			RemoteExecutionOptions: remoteExecOptions,
			LocalExecutionOptions: &ppb.LocalExecutionOptions{
				AcceptCached: true,
			},
			CompareWithLocal: true,
			NumLocalReruns:   2,
			NumRemoteReruns:  3,
			ReclientTimeout:  3600,
		},
	}
	wantCmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec: &command.InputSpec{
			Inputs: []string{
				"tool",
				"inout",
			},
		},
		OutputFiles: []string{"inout"},
	}
	setPlatformOSFamily(wantCmd)
	res := &command.Result{Status: command.CacheHitResultStatus}
	executionOptions := command.DefaultExecutionOptions()
	executionOptions.DoNotCache = true
	env.Set(wantCmd, executionOptions, res, &fakes.OutputFile{"inout", "bar"})
	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}

	want := &ppb.RunResponse{Result: &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS}}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	path := filepath.Join(env.ExecRoot, "inout")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	wantContents := "foobaz"
	if !bytes.Equal(contents, []byte(wantContents)) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, wantContents)
	}
}

func TestNumRetriesIfMismatched(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	resMgr := localresources.NewDefaultManager()
	execroot.AddFileWithContent(t, filepath.Join(env.ExecRoot, abOutPath), []byte("foo\n"))
	server := &Server{
		LocalPool:         NewLocalPool(&subprocess.SystemExecutor{}, resMgr),
		FileMetadataStore: fmc,
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	var cmdArgs []string
	var wantOutput []byte
	var wantFileDg string
	var wantDirDg string
	var wantActionDigest []string
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("(if not exist %s mkdir %s) && echo hello>%s", abPath, abPath, abOutPath)}
		wantOutput = []byte("hello\r\n")
		wantFileDg = "cd2eca3535741f27a8ae40c31b0c41d4057a7a7b912b33b9aed86485d1c84676/7"
		wantDirDg = "e04c31681c3dae2046b8caacda5a17160bca360461ba71b62951e9c514d4746d/79"
		wantActionDigest = []string{"a5c9f92241a0522ef34a6d3edd63c44eb7cc930bec8a44c0eff661732745c3f5/140"}
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprintf("mkdir -p %s && echo hello > %s", abPath, abOutPath)}
		wantOutput = []byte("hello\n")
		wantFileDg = "5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03/6"
		wantDirDg = "48bd723bc0791eeeda990c6ca2fc14133a73cb23e1c0aee08aa0a8728d37da93/79"
		// Action digests can differ between linux and mac.
		wantActionDigest = []string{
			"7ac88c6d7b69b581fd7e7baf9629703568737ed6d5bbb94b9ac7f51a8c61c0e6/140", // digest for linux
			"5e189a88f60d48d1df1f0d6277d44154674b350c383d2780b7b05a352f22cec7/140", // digest for mac
		}
	}
	ctx := context.Background()
	remoteExecOptions := &ppb.RemoteExecutionOptions{AcceptCached: false, DoNotCache: true}
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles:       []string{abOutPath},
				OutputDirectories: []string{abPath},
			},
		},
		Labels: map[string]string{"type": "tool", "shallow": "true"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{
			ExecutionStrategy:      ppb.ExecutionStrategy_REMOTE_LOCAL_FALLBACK,
			RemoteExecutionOptions: remoteExecOptions,
			CompareWithLocal:       true,
			NumRetriesIfMismatched: 5,
			NumLocalReruns:         1,
			ReclientTimeout:        3600,
		},
	}

	res := &command.Result{Status: command.SuccessResultStatus}
	cmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec:   &command.InputSpec{},
		OutputFiles: []string{abOutPath},
		OutputDirs:  []string{abPath},
	}
	setPlatformOSFamily(cmd)
	executionOptions := command.DefaultExecutionOptions()
	executionOptions.DoNotCache = true
	env.Set(cmd, executionOptions, res, &fakes.OutputFile{abOutPath, "fake-output"}, &fakes.OutputDir{abPath})
	os.RemoveAll(filepath.Join(env.ExecRoot, abPath))
	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{
		Result: &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
	}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: gotErr %s, (-want +got)\n%s", got.Stderr, diff)
	}
	path := filepath.Join(env.ExecRoot, abOutPath)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, wantOutput)
	}

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}

	wantRemoteFileDg := "c317dd6d0832d1b835458b9b6dcd89e74da5fdecc5f88060c3fdc70d2f017eae/11"
	wantRemoteDirDg := "feaf62a9c58e3228ebe105fe5046773c8f88dca842b3a5c19d7f453684a4a60d/79"
	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(cmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_REMOTE_EXECUTION,
			RemoteMetadata: &lpb.RemoteMetadata{
				NumInputDirectories:  1,
				NumOutputFiles:       1,
				NumOutputDirectories: 1,
				OutputFileDigests: map[string]string{
					abOutPath: wantRemoteFileDg,
				},
				OutputDirectoryDigests: map[string]string{
					abPath: wantRemoteDirDg,
				},
				Result: &cpb.CommandResult{
					Status: cpb.CommandResultStatus_SUCCESS,
				},
				TotalOutputBytes: 90,
				RerunMetadata: []*lpb.RerunMetadata{
					{
						Attempt: 1,
						Result:  &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
						OutputFileDigests: map[string]string{
							abOutPath: wantRemoteFileDg,
						},
						OutputDirectoryDigests: map[string]string{
							abPath: wantRemoteDirDg,
						},
						NumOutputFiles:       1,
						NumOutputDirectories: 1,
						TotalOutputBytes:     90,
					},
					{
						Attempt: 2,
						Result:  &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
						OutputFileDigests: map[string]string{
							abOutPath: wantRemoteFileDg,
						},
						OutputDirectoryDigests: map[string]string{
							abPath: wantRemoteDirDg,
						},
						NumOutputFiles:       1,
						NumOutputDirectories: 1,
						TotalOutputBytes:     90,
					},
					{
						Attempt: 3,
						Result:  &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
						OutputFileDigests: map[string]string{
							abOutPath: wantRemoteFileDg,
						},
						OutputDirectoryDigests: map[string]string{
							abPath: wantRemoteDirDg,
						},
						NumOutputFiles:       1,
						NumOutputDirectories: 1,
						TotalOutputBytes:     90,
					},
					{
						Attempt: 4,
						Result:  &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
						OutputFileDigests: map[string]string{
							abOutPath: wantRemoteFileDg,
						},
						OutputDirectoryDigests: map[string]string{
							abPath: wantRemoteDirDg,
						},
						NumOutputFiles:       1,
						NumOutputDirectories: 1,
						TotalOutputBytes:     90,
					},
					{
						Attempt: 5,
						Result:  &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
						OutputFileDigests: map[string]string{
							abOutPath: wantRemoteFileDg,
						},
						OutputDirectoryDigests: map[string]string{
							abPath: wantRemoteDirDg,
						},
						NumOutputFiles:       1,
						NumOutputDirectories: 1,
						TotalOutputBytes:     90,
					},
				},
			},
			LocalMetadata: &lpb.LocalMetadata{
				Labels: map[string]string{"type": "tool", "shallow": "true"},
				Verification: &lpb.Verification{
					TotalMismatches: 2,
					Mismatches: []*lpb.Verification_Mismatch{
						{
							Path:          filepath.ToSlash(abPath),
							RemoteDigests: []string{wantRemoteDirDg},
							LocalDigests:  []string{wantDirDg},
							Determinism:   lpb.DeterminismStatus_DETERMINISTIC,
						},
						{
							Path:          filepath.ToSlash(abOutPath),
							RemoteDigests: []string{wantRemoteFileDg},
							LocalDigests:  []string{wantFileDg},
							Determinism:   lpb.DeterminismStatus_DETERMINISTIC,
						},
					},
					TotalVerified: 2,
				},
				RerunMetadata: []*lpb.RerunMetadata{
					{
						Attempt: 1,
						Result:  &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
						OutputFileDigests: map[string]string{
							abOutPath: wantFileDg,
						},
						OutputDirectoryDigests: map[string]string{
							abPath: wantDirDg,
						},
					},
				},
			},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Fatalf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
	gotActionDigest := recs[0].LocalMetadata.Verification.Mismatches[0].ActionDigest
	if !contains(wantActionDigest, gotActionDigest) {
		t.Fatalf("Verification Mismatch missing action digest, want one of %+v, got %v", wantActionDigest, gotActionDigest)
	}
}

func TestCompareWithRerunsNoMismatches(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		LocalPool:         NewLocalPool(&subprocess.SystemExecutor{}, resMgr),
		FileMetadataStore: fmc,
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	var cmdArgs []string
	var wantOutput []byte
	var wantFileDg string
	var wantDirDg string
	var wantOutputBytes int64
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("(if not exist %s mkdir %s) && echo hello>%s", abPath, abPath, abOutPath)}
		wantOutput = []byte("hello\r\n")
		wantFileDg = "cd2eca3535741f27a8ae40c31b0c41d4057a7a7b912b33b9aed86485d1c84676/7"
		wantDirDg = "e04c31681c3dae2046b8caacda5a17160bca360461ba71b62951e9c514d4746d/79"
		wantOutputBytes = 86
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprintf("mkdir -p %s && echo hello > %s", abPath, abOutPath)}
		wantOutput = []byte("hello\n")
		wantFileDg = "5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03/6"
		wantDirDg = "48bd723bc0791eeeda990c6ca2fc14133a73cb23e1c0aee08aa0a8728d37da93/79"
		wantOutputBytes = 85
	}
	execroot.AddFileWithContent(t, filepath.Join(env.ExecRoot, abOutPath), wantOutput)
	ctx := context.Background()
	remoteExecOptions := &ppb.RemoteExecutionOptions{AcceptCached: false, DoNotCache: true}
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles:       []string{abOutPath},
				OutputDirectories: []string{abPath},
			},
		},
		Labels: map[string]string{"type": "tool", "shallow": "true"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{
			ExecutionStrategy:      ppb.ExecutionStrategy_REMOTE_LOCAL_FALLBACK,
			RemoteExecutionOptions: remoteExecOptions,
			CompareWithLocal:       true,
			ReclientTimeout:        3600,
			NumLocalReruns:         1,
			NumRemoteReruns:        1,
		},
	}

	res := &command.Result{Status: command.SuccessResultStatus}
	cmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec:   &command.InputSpec{},
		OutputFiles: []string{abOutPath},
		OutputDirs:  []string{abPath},
	}
	setPlatformOSFamily(cmd)
	executionOptions := command.DefaultExecutionOptions()
	executionOptions.DoNotCache = true
	env.Set(cmd, executionOptions, res, &fakes.OutputFile{Path: abOutPath, Contents: string(wantOutput)}, &fakes.OutputDir{Path: abPath})
	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{
		Result: &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
	}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: gotErr %s, (-want +got)\n%s", got.Stderr, diff)
	}
	path := filepath.Join(env.ExecRoot, abOutPath)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, wantOutput)
	}

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}

	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(cmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_REMOTE_EXECUTION,
			RemoteMetadata: &lpb.RemoteMetadata{
				NumInputDirectories:  1,
				NumOutputFiles:       1,
				NumOutputDirectories: 1,
				OutputFileDigests: map[string]string{
					abOutPath: wantFileDg,
				},
				OutputDirectoryDigests: map[string]string{
					abPath: wantDirDg,
				},
				Result: &cpb.CommandResult{
					Status: cpb.CommandResultStatus_SUCCESS,
				},
				TotalOutputBytes: wantOutputBytes,
				RerunMetadata: []*lpb.RerunMetadata{
					{
						Attempt: 1,
						Result:  &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
						OutputFileDigests: map[string]string{
							abOutPath: wantFileDg,
						},
						OutputDirectoryDigests: map[string]string{
							abPath: wantDirDg,
						},
						NumOutputFiles:       1,
						NumOutputDirectories: 1,
						TotalOutputBytes:     wantOutputBytes,
					},
				},
			},
			LocalMetadata: &lpb.LocalMetadata{
				Labels: map[string]string{"type": "tool", "shallow": "true"},
				Verification: &lpb.Verification{
					TotalVerified: 2,
				},
				RerunMetadata: []*lpb.RerunMetadata{
					{
						Attempt: 1,
						Result:  &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
						OutputFileDigests: map[string]string{
							abOutPath: wantFileDg,
						},
						OutputDirectoryDigests: map[string]string{
							abPath: wantDirDg,
						},
					},
				},
			},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Fatalf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestCompareWithReruns(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	resMgr := localresources.NewDefaultManager()
	execroot.AddFileWithContent(t, filepath.Join(env.ExecRoot, abOutPath), []byte("foo\n"))
	server := &Server{
		LocalPool:         NewLocalPool(&subprocess.SystemExecutor{}, resMgr),
		FileMetadataStore: fmc,
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	var cmdArgs []string
	var wantOutput []byte
	var wantFileDg string
	var wantDirDg string
	var wantActionDigest []string
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("(if not exist %s mkdir %s) && echo hello>%s", abPath, abPath, abOutPath)}
		wantOutput = []byte("hello\r\n")
		wantFileDg = "cd2eca3535741f27a8ae40c31b0c41d4057a7a7b912b33b9aed86485d1c84676/7"
		wantDirDg = "e04c31681c3dae2046b8caacda5a17160bca360461ba71b62951e9c514d4746d/79"
		wantActionDigest = []string{"a5c9f92241a0522ef34a6d3edd63c44eb7cc930bec8a44c0eff661732745c3f5/140"}
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprintf("mkdir -p %s && echo hello > %s", abPath, abOutPath)}
		wantOutput = []byte("hello\n")
		wantFileDg = "5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03/6"
		wantDirDg = "48bd723bc0791eeeda990c6ca2fc14133a73cb23e1c0aee08aa0a8728d37da93/79"
		// Action digests can differ between linux and mac.
		wantActionDigest = []string{
			"7ac88c6d7b69b581fd7e7baf9629703568737ed6d5bbb94b9ac7f51a8c61c0e6/140", // digest for linux
			"5e189a88f60d48d1df1f0d6277d44154674b350c383d2780b7b05a352f22cec7/140", // digest for mac
		}
	}
	ctx := context.Background()
	remoteExecOptions := &ppb.RemoteExecutionOptions{AcceptCached: false, DoNotCache: true}
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles:       []string{abOutPath},
				OutputDirectories: []string{abPath},
			},
		},
		Labels: map[string]string{"type": "tool", "shallow": "true"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{
			ExecutionStrategy:      ppb.ExecutionStrategy_REMOTE_LOCAL_FALLBACK,
			RemoteExecutionOptions: remoteExecOptions,
			CompareWithLocal:       true,
			NumRetriesIfMismatched: 5,
			NumLocalReruns:         3,
			NumRemoteReruns:        4,
			ReclientTimeout:        3600,
		},
	}

	res := &command.Result{Status: command.SuccessResultStatus}
	cmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec:   &command.InputSpec{},
		OutputFiles: []string{abOutPath},
		OutputDirs:  []string{abPath},
	}
	setPlatformOSFamily(cmd)
	executionOptions := command.DefaultExecutionOptions()
	executionOptions.DoNotCache = true
	env.Set(cmd, executionOptions, res, &fakes.OutputFile{abOutPath, "fake-output"}, &fakes.OutputDir{abPath})
	os.RemoveAll(filepath.Join(env.ExecRoot, abPath))
	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{
		Result: &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
	}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: gotErr %s, (-want +got)\n%s", got.Stderr, diff)
	}
	path := filepath.Join(env.ExecRoot, abOutPath)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, wantOutput)
	}

	gotSummary, _ := lg.GetStatusSummary(ctx, &ppb.GetStatusSummaryRequest{})
	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}

	wantRemoteFileDg := "c317dd6d0832d1b835458b9b6dcd89e74da5fdecc5f88060c3fdc70d2f017eae/11"
	wantRemoteDirDg := "feaf62a9c58e3228ebe105fe5046773c8f88dca842b3a5c19d7f453684a4a60d/79"
	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(cmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_REMOTE_EXECUTION,
			RemoteMetadata: &lpb.RemoteMetadata{
				NumInputDirectories:  1,
				NumOutputFiles:       1,
				NumOutputDirectories: 1,
				OutputFileDigests: map[string]string{
					abOutPath: wantRemoteFileDg,
				},
				OutputDirectoryDigests: map[string]string{
					abPath: wantRemoteDirDg,
				},
				Result: &cpb.CommandResult{
					Status: cpb.CommandResultStatus_SUCCESS,
				},
				TotalOutputBytes: 90,
				RerunMetadata: []*lpb.RerunMetadata{
					{
						Attempt: 1,
						Result:  &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
						OutputFileDigests: map[string]string{
							abOutPath: wantRemoteFileDg,
						},
						OutputDirectoryDigests: map[string]string{
							abPath: wantRemoteDirDg,
						},
						NumOutputFiles:       1,
						NumOutputDirectories: 1,
						TotalOutputBytes:     90,
					},
					{
						Attempt: 2,
						Result:  &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
						OutputFileDigests: map[string]string{
							abOutPath: wantRemoteFileDg,
						},
						OutputDirectoryDigests: map[string]string{
							abPath: wantRemoteDirDg,
						},
						NumOutputFiles:       1,
						NumOutputDirectories: 1,
						TotalOutputBytes:     90,
					},
					{
						Attempt: 3,
						Result:  &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
						OutputFileDigests: map[string]string{
							abOutPath: wantRemoteFileDg,
						},
						OutputDirectoryDigests: map[string]string{
							abPath: wantRemoteDirDg,
						},
						NumOutputFiles:       1,
						NumOutputDirectories: 1,
						TotalOutputBytes:     90,
					},
					{
						Attempt: 4,
						Result:  &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
						OutputFileDigests: map[string]string{
							abOutPath: wantRemoteFileDg,
						},
						OutputDirectoryDigests: map[string]string{
							abPath: wantRemoteDirDg,
						},
						NumOutputFiles:       1,
						NumOutputDirectories: 1,
						TotalOutputBytes:     90,
					},
				},
			},
			LocalMetadata: &lpb.LocalMetadata{
				Labels: map[string]string{"type": "tool", "shallow": "true"},
				Verification: &lpb.Verification{
					TotalMismatches: 2,
					Mismatches: []*lpb.Verification_Mismatch{
						{
							Path:          filepath.ToSlash(abPath),
							RemoteDigests: []string{wantRemoteDirDg},
							LocalDigests:  []string{wantDirDg},
							Determinism:   lpb.DeterminismStatus_DETERMINISTIC,
						},
						{
							Path:          filepath.ToSlash(abOutPath),
							RemoteDigests: []string{wantRemoteFileDg},
							LocalDigests:  []string{wantFileDg},
							Determinism:   lpb.DeterminismStatus_DETERMINISTIC,
						},
					},
					TotalVerified: 2,
				},
				RerunMetadata: []*lpb.RerunMetadata{
					{
						Attempt: 1,
						Result:  &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
						OutputFileDigests: map[string]string{
							abOutPath: wantFileDg,
						},
						OutputDirectoryDigests: map[string]string{
							abPath: wantDirDg,
						},
					},
					{
						Attempt: 2,
						Result:  &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
						OutputFileDigests: map[string]string{
							abOutPath: wantFileDg,
						},
						OutputDirectoryDigests: map[string]string{
							abPath: wantDirDg,
						},
					},
					{
						Attempt: 3,
						Result:  &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
						OutputFileDigests: map[string]string{
							abOutPath: wantFileDg,
						},
						OutputDirectoryDigests: map[string]string{
							abPath: wantDirDg,
						},
					},
				},
			},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Fatalf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
	gotActionDigest := recs[0].LocalMetadata.Verification.Mismatches[0].ActionDigest
	if !contains(wantActionDigest, gotActionDigest) {
		t.Fatalf("Verification Mismatch missing action digest, want one of %+v, got %v", wantActionDigest, gotActionDigest)
	}
	wantSummary := &ppb.GetStatusSummaryResponse{
		CompletedActionStats: map[string]int32{
			lpb.CompletionStatus_STATUS_REMOTE_EXECUTION.String(): 1,
		},
		RunningActions: 0,
	}
	if diff := cmp.Diff(wantSummary, gotSummary, protocmp.Transform()); diff != "" {
		t.Errorf("Status summary returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestNoRerunWhenNoCompareMode(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	resMgr := localresources.NewDefaultManager()
	execroot.AddFileWithContent(t, filepath.Join(env.ExecRoot, abOutPath), []byte("hello\n"))
	server := &Server{
		LocalPool:         NewLocalPool(&subprocess.SystemExecutor{}, resMgr),
		FileMetadataStore: fmc,
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	var cmdArgs []string
	var wantOutput []byte
	var wantFileDg string
	var wantRemoteDirDg string
	var wantOutputBytes int64
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("(if not exist %s mkdir %s) && echo hello>%s", abPath, abPath, abOutPath)}
		wantOutput = []byte("hello\n")
		wantFileDg = "5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03/6"
		wantRemoteDirDg = "48bd723bc0791eeeda990c6ca2fc14133a73cb23e1c0aee08aa0a8728d37da93/79"
		wantOutputBytes = int64(85)
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprintf("mkdir -p %s && echo hello > %s", abPath, abOutPath)}
		wantOutput = []byte("hello\n")
		wantFileDg = "5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03/6"
		wantRemoteDirDg = "48bd723bc0791eeeda990c6ca2fc14133a73cb23e1c0aee08aa0a8728d37da93/79"
		wantOutputBytes = int64(85)
	}
	ctx := context.Background()
	remoteExecOptions := &ppb.RemoteExecutionOptions{AcceptCached: false, DoNotCache: true}
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles:       []string{abOutPath},
				OutputDirectories: []string{abPath},
			},
		},
		Labels: map[string]string{"type": "tool", "shallow": "true"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{
			ExecutionStrategy:      ppb.ExecutionStrategy_REMOTE,
			RemoteExecutionOptions: remoteExecOptions,
			NumLocalReruns:         2,
			NumRemoteReruns:        3,
			ReclientTimeout:        3600,
		},
	}

	res := &command.Result{Status: command.SuccessResultStatus}
	cmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec:   &command.InputSpec{},
		OutputFiles: []string{abOutPath},
		OutputDirs:  []string{abPath},
	}

	setPlatformOSFamily(cmd)

	executionOptions := command.DefaultExecutionOptions()
	executionOptions.DoNotCache = true
	env.Set(cmd, executionOptions, res, &fakes.OutputFile{abOutPath, string(wantOutput)}, &fakes.OutputDir{abPath})
	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{
		Result: &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
	}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: gotErr %s, (-want +got)\n%s", got.Stderr, diff)
	}
	path := filepath.Join(env.ExecRoot, abOutPath)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, wantOutput)
	}

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}

	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(cmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_REMOTE_EXECUTION,
			RemoteMetadata: &lpb.RemoteMetadata{
				NumInputDirectories:  1,
				NumOutputFiles:       1,
				NumOutputDirectories: 1,
				OutputFileDigests: map[string]string{
					abOutPath: wantFileDg,
				},
				OutputDirectoryDigests: map[string]string{
					abPath: wantRemoteDirDg,
				},
				Result: &cpb.CommandResult{
					Status: cpb.CommandResultStatus_SUCCESS,
				},
				TotalOutputBytes: wantOutputBytes,
			},
			LocalMetadata: &lpb.LocalMetadata{
				Labels: map[string]string{"type": "tool", "shallow": "true"},
			},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Fatalf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestRemoteLocalFallback(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		LocalPool:         NewLocalPool(&subprocess.SystemExecutor{}, resMgr),
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
		FileMetadataStore: fmc,
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	var cmdArgs []string
	var wantOutput []byte
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("mkdir %s && echo hello>%s", abPath, abOutPath)}
		wantOutput = []byte("hello\r\n")
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprintf("mkdir -p %s && echo hello > %s", abPath, abOutPath)}
		wantOutput = []byte("hello\n")
	}
	ctx := context.Background()
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels:           map[string]string{"type": "tool", "shallow": "true"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_REMOTE_LOCAL_FALLBACK, ReclientTimeout: 3600},
	}

	// Setting up remote execution to fail.
	res := &command.Result{Status: command.NonZeroExitResultStatus, ExitCode: 5}
	stdErr := []byte("something happened")
	cmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec:   &command.InputSpec{},
		OutputFiles: []string{abOutPath},
	}
	setPlatformOSFamily(cmd)
	env.Set(cmd, command.DefaultExecutionOptions(), res, fakes.StdErr(stdErr))
	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{
		Result: &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
		// Stderr of failed remote action isn't sent to client.
		// Confirmation of remote attempt is checked below in wantRecs.
		RemoteFallbackInfo: &ppb.RemoteFallbackInfo{
			ExitCode: 5,
			Stderr:   stdErr,
		},
	}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	path := filepath.Join(env.ExecRoot, abOutPath)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, wantOutput)
	}

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(cmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_LOCAL_FALLBACK,
			RemoteMetadata: &lpb.RemoteMetadata{
				NumInputDirectories: 1,
				Result: &cpb.CommandResult{
					Status:   cpb.CommandResultStatus_NON_ZERO_EXIT,
					ExitCode: 5,
				},
				TotalOutputBytes: int64(len(stdErr)),
			},
			LocalMetadata: &lpb.LocalMetadata{
				Result:          &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
				ExecutedLocally: true,
				Labels:          map[string]string{"type": "tool", "shallow": "true"},
			},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Errorf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestForwardErrorLog(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	t.Cleanup(cleanup)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
		FileMetadataStore: fmc,
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	ctx := context.Background()
	// There is an error in the mock code which returns CacheHitResultStatus for RemoteErrorResultStatus. We need to disable the cache here
	// in order to get the expected response.
	remoteExecOptions := &ppb.RemoteExecutionOptions{AcceptCached: false}
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     []string{"clang", "-o", abOutPath, "a"},
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels:           map[string]string{"type": "compile", "lang": "cpp", "compiler": "clang"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_REMOTE, RemoteExecutionOptions: remoteExecOptions, ReclientTimeout: 3600},
	}
	wantCmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        []string{"clang", "-o", abOutPath, "a"},
		ExecRoot:    env.ExecRoot,
		InputSpec:   &command.InputSpec{},
		OutputFiles: []string{abOutPath},
	}
	setPlatformOSFamily(wantCmd)
	errorMsg := "An Error Occured"
	internalErrorMsg := "rpc error: code = Internal desc = "
	res := &command.Result{Status: command.RemoteErrorResultStatus, Err: errors.New(errorMsg), ExitCode: 45}
	env.Set(wantCmd, command.DefaultExecutionOptions(), res)
	got, err := server.RunCommand(ctx, req)
	wantErrPrefix := "reclient[" + got.ExecutionId + "]: " + command.RemoteErrorResultStatus.String() + ": "
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{
		Result: &cpb.CommandResult{
			Status:   cpb.CommandResultStatus_REMOTE_ERROR,
			ExitCode: 45,
			Msg:      internalErrorMsg + errorMsg,
		},
		Stderr: []byte(wantErrPrefix + internalErrorMsg + errorMsg + "\n"),
	}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestFailEarlyOnIpTimeouts(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	resMgr := localresources.NewDefaultManager()

	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     []string{"clang", "-o", abOutPath, "a"},
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels:           map[string]string{"type": "compile", "lang": "cpp", "compiler": "clang"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_REMOTE, ReclientTimeout: 3600},
	}

	cmdsPerTest := 20
	tests := []struct {
		name     string
		depsErr  error
		maxExecs int
	}{
		{
			name:    "deps scan timeouts should trigger fail early condition",
			depsErr: fmt.Errorf("deps scan timeout: %w", context.DeadlineExceeded),
			// because MonitorFailBuildConditions is called asynchronously
			// maxExecs can be either equal to or greater by one than AllowedIPTimeouts
			maxExecs: int(AllowedIPTimeouts) + 1,
		},
		{
			name:     "non-timeout errors should not trigger fail early condition",
			depsErr:  fmt.Errorf("deps scan error: %w", context.Canceled),
			maxExecs: cmdsPerTest,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			depScanner := &stubCPPDependencyScanner{
				processInputsError: test.depsErr,
			}

			server := &Server{
				LocalPool: NewLocalPool(&subprocess.SystemExecutor{}, resMgr),
				// needs to be > 0 for fail early functionality to be active
				// but high enough that we can test fail early for IP timeouts and not fallbacks
				FailEarlyMinActionCount:   4000,
				FailEarlyMinFallbackRatio: 0.5,
				MaxHoldoff:                time.Minute,
				DownloadTmp:               t.TempDir(),
				FileMetadataStore:         fmc,
			}
			server.Init()
			server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(depScanner, false, nil, resMgr), func() {})
			server.SetREClient(env.Client, func() {})

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go server.monitorFailBuildConditions(ctx, 20*time.Millisecond)

			for i := 0; i < cmdsPerTest; i++ {
				_, err := server.RunCommand(ctx, req)
				time.Sleep(time.Millisecond * 50)
				if err != nil {
					t.Errorf("RunCommand() returned error: %v", err)
				}
			}
			if depScanner.counter > test.maxExecs {
				t.Errorf("Expected depScanner to be not called more than: %v", test.maxExecs)
			}
		})
	}
}

func TestFailEarly(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		LocalPool:                 NewLocalPool(&subprocess.SystemExecutor{}, resMgr),
		FailEarlyMinActionCount:   2,
		FailEarlyMinFallbackRatio: 0.5,
		MaxHoldoff:                time.Minute,
		DownloadTmp:               t.TempDir(),
		FileMetadataStore:         fmc,
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go server.monitorFailBuildConditions(ctx, 500*time.Millisecond)
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	var cmdArgs []string
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("mkdir %s && echo hellos>%s", abPath, abOutPath)}
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprintf("mkdir -p %s && echo hellos > %s", abPath, abOutPath)}
	}
	reqFallback := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels:           map[string]string{"type": "tool"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_REMOTE_LOCAL_FALLBACK, ReclientTimeout: 3600},
	}
	reqSuccess := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Platform: map[string]string{"key": "value"},
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels:           map[string]string{"type": "tool"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_REMOTE_LOCAL_FALLBACK, ReclientTimeout: 3600},
	}

	// Setting up remote execution to fail for fallback action.
	res := &command.Result{Status: command.NonZeroExitResultStatus, ExitCode: 5}
	cmdFallback := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec:   &command.InputSpec{},
		OutputFiles: []string{abOutPath},
	}
	setPlatformOSFamily(cmdFallback)
	env.Set(cmdFallback, command.DefaultExecutionOptions(), res)
	// Setting up remote execution to succeed for succesful action.
	res = &command.Result{Status: command.SuccessResultStatus, ExitCode: 0}
	cmdSuccess := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec:   &command.InputSpec{},
		Platform:    map[string]string{"key": "value"},
		OutputFiles: []string{abOutPath},
	}
	setPlatformOSFamily(cmdSuccess)
	wantFallback := &ppb.RunResponse{
		Result: &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
		RemoteFallbackInfo: &ppb.RemoteFallbackInfo{
			ExitCode: 5,
		},
	}
	wantSuccess := &ppb.RunResponse{
		Result: &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
	}
	got, err := server.RunCommand(ctx, reqFallback)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	if diff := cmp.Diff(wantFallback, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	env.Set(cmdSuccess, command.DefaultExecutionOptions(), res)
	got, err = server.RunCommand(ctx, reqSuccess)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	if diff := cmp.Diff(wantSuccess, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	// Third request should be rejected. Wait for 1 second so ticker can check current num of fallbacks.
	time.Sleep(time.Second)
	got, err = server.RunCommand(ctx, reqFallback)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{
		Result: &cpb.CommandResult{
			Status:   cpb.CommandResultStatus_LOCAL_ERROR,
			ExitCode: command.LocalErrorExitCode,
		},
	}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id", "stdout"), protocmp.IgnoreFields(&cpb.CommandResult{}, "msg"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(cmdFallback),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_LOCAL_FALLBACK,
			RemoteMetadata: &lpb.RemoteMetadata{
				NumInputDirectories: 1,
				Result: &cpb.CommandResult{
					Status:   cpb.CommandResultStatus_NON_ZERO_EXIT,
					ExitCode: 5,
				},
			},
			LocalMetadata: &lpb.LocalMetadata{
				Result:          &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
				ExecutedLocally: true,
				Labels:          map[string]string{"type": "tool"},
			},
		},
		{
			Command:          command.ToProto(cmdSuccess),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_REMOTE_EXECUTION,
			RemoteMetadata: &lpb.RemoteMetadata{
				NumInputDirectories: 1,
				Result: &cpb.CommandResult{
					Status: cpb.CommandResultStatus_SUCCESS,
				},
			},
			LocalMetadata: &lpb.LocalMetadata{
				Labels: map[string]string{"type": "tool"},
			},
		},
		{
			Command: command.ToProto(cmdFallback),
			Result: &cpb.CommandResult{
				Status:   cpb.CommandResultStatus_LOCAL_ERROR,
				ExitCode: command.LocalErrorExitCode,
			},
			CompletionStatus: lpb.CompletionStatus_STATUS_LOCAL_FAILURE,
			LocalMetadata: &lpb.LocalMetadata{
				Labels: map[string]string{"type": "tool"},
			},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Errorf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestRunCommand_LabelDigestAddedToCommandID(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		LocalPool:         NewLocalPool(&subprocess.SystemExecutor{}, resMgr),
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
		FileMetadataStore: fmc,
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	var cmdArgs []string
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("mkdir %s && echo hello>%s", abPath, abOutPath)}
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprintf("mkdir -p %s && echo hello > %s", abPath, abOutPath)}
	}
	ctx := context.Background()
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels:           map[string]string{"type": "tool", "shallow": "true"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_REMOTE_LOCAL_FALLBACK, ReclientTimeout: 3600},
	}

	res := &command.Result{Status: command.NonZeroExitResultStatus, ExitCode: 5}
	cmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec:   &command.InputSpec{},
		OutputFiles: []string{abOutPath},
	}
	setPlatformOSFamily(cmd)
	env.Set(cmd, command.DefaultExecutionOptions(), res)

	if _, err := server.RunCommand(ctx, req); err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	server.DrainAndReleaseResources()

	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}

	want := "fd4a710b" // first 8 characters of SHA256 of the above specified labels.
	cid := recs[0].Command.Identifiers.CommandId
	parts := strings.Split(cid, "-")
	if len(parts) != 2 {
		t.Fatalf("RunCommand(%+v) command-id=%q; want in the format <label-digest>-<command-digest>", req, cid)
	}
	if got := parts[0]; want != got {
		t.Errorf("RunCommand(%+v) command-id=%s-*; digest=%s-*", req, got, want)
	}
}

func TestRunCommand_InvalidUTF8InStdout(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	want := []byte{104, 105, 32, 173, 10}
	executor := &execStub{
		localExec: func() {
			time.Sleep(time.Second) // Hack to let mtime catch up.
			execroot.AddFileWithContent(t, filepath.Join(env.ExecRoot, "foo.o"), []byte("foo.o"))
			createDFile(t, env.ExecRoot, "foo.o", "foo.h", "bar.h")
		},
		stdOut: want, // Invalid UTF-8 sequence.
	}
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		LocalPool:         NewLocalPool(executor, resMgr),
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
		FileMetadataStore: fmc,
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	var cmdArgs []string
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("mkdir %s && echo hello>%s", abPath, abOutPath)}
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprintf("mkdir -p %s && echo hello > %s", abPath, abOutPath)}
	}
	ctx := context.Background()
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels:           map[string]string{"type": "tool", "shallow": "true"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_REMOTE_LOCAL_FALLBACK, ReclientTimeout: 3600},
	}

	res := &command.Result{Status: command.SuccessResultStatus, ExitCode: 0}
	cmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec:   &command.InputSpec{},
		OutputFiles: []string{abOutPath},
	}
	setPlatformOSFamily(cmd)
	env.Set(cmd, command.DefaultExecutionOptions(), res, fakes.StdOut(string(want)))

	resp, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	server.DrainAndReleaseResources()

	got := resp.Stdout
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("RunCommand(%+v) returned diff (-want +got) = %v\ngot = %v, want %v", req, diff, got, want)
	}
}

func TestLocalFallback(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		LocalPool:         NewLocalPool(&subprocess.SystemExecutor{}, resMgr),
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
		FileMetadataStore: fmc,
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	var cmdArgs []string
	var wantOutput []byte
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("mkdir %s && echo hello>%s", abPath, abOutPath)}
		wantOutput = []byte("hello\r\n")
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprintf("mkdir -p %s && echo hello > %s", abPath, abOutPath)}
		wantOutput = []byte("hello\n")
	}
	ctx := context.Background()
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels:           map[string]string{"lang": "unsupported"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_LOCAL, ReclientTimeout: 3600},
	}
	cmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec:   &command.InputSpec{},
		OutputFiles: []string{abOutPath},
		Platform:    map[string]string{platformVersionKey: version.CurrentVersion()},
	}
	setPlatformOSFamily(cmd)
	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{
		Result: &cpb.CommandResult{
			Status: cpb.CommandResultStatus_SUCCESS,
		},
	}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	path := filepath.Join(env.ExecRoot, abOutPath)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, wantOutput)
	}

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(cmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_LOCAL_EXECUTION,
			LocalMetadata: &lpb.LocalMetadata{
				Result:          &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
				ExecutedLocally: true,
				UpdatedCache:    false,
				Labels:          map[string]string{"lang": "unsupported"},
			},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Errorf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestLocalFallback_EnvVariables(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		LocalPool:         NewLocalPool(&subprocess.SystemExecutor{}, resMgr),
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
		FileMetadataStore: fmc,
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	var cmdArgs []string
	var wantOutput []byte
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("mkdir %s && echo hello>%s", abPath, abOutPath)}
		wantOutput = []byte("hello\r\n")
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprintf("mkdir -p %s && echo $OTHER_ENV_VAR > %s", abPath, abOutPath)}
		wantOutput = []byte("VALUE\n")
	}
	ctx := context.Background()
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels:           map[string]string{"lang": "unsupported"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_LOCAL, ReclientTimeout: 3600},
		Metadata: &ppb.Metadata{
			Environment: []string{"OTHER_ENV_VAR=VALUE"},
		},
	}

	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{
		Result: &cpb.CommandResult{
			Status: cpb.CommandResultStatus_SUCCESS,
		},
	}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	path := filepath.Join(env.ExecRoot, abOutPath)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, wantOutput)
	}

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	cmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec:   &command.InputSpec{},
		OutputFiles: []string{abOutPath},
		Platform:    map[string]string{platformVersionKey: version.CurrentVersion()},
	}
	setPlatformOSFamily(cmd)
	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(cmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_LOCAL_EXECUTION,
			LocalMetadata: &lpb.LocalMetadata{
				Result:          &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
				ExecutedLocally: true,
				UpdatedCache:    false,
				Labels:          map[string]string{"lang": "unsupported"},
			},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Errorf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestRacingRemoteWinsCopyWorksOnTmpFs(t *testing.T) {
	if runtime.GOOS != "linux" {
		// tmpfs only exists on linux
		t.Skip()
	}
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		LocalPool:         NewLocalPool(&subprocess.SystemExecutor{}, resMgr),
		FileMetadataStore: fmc,
		Forecast:          &Forecast{},
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	var cmdArgs []string
	var wantOutput []byte
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("mkdir %s && sleep 10 && echo hello>%s && echo Done", abPath, abOutPath)}
		wantOutput = []byte("hello\r\n")
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprint("mkdir -p %s && sleep 10 && echo hello > %s && echo Done", abPath, abOutPath)}
		wantOutput = []byte("hello\n")
	}
	ctx := context.Background()
	// Lock entire local resources to prevent local execution from running.
	release, err := resMgr.Lock(ctx, int64(runtime.NumCPU()), 1)
	if err != nil {
		t.Fatalf("Failed to lock entire local resources: %v", err)
	}
	t.Cleanup(release)
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels:           map[string]string{"type": "tool"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_RACING, ReclientTimeout: 3600},
	}
	cmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec:   &command.InputSpec{},
		OutputFiles: []string{abOutPath},
	}
	setPlatformOSFamily(cmd)
	res := &command.Result{Status: command.SuccessResultStatus}
	env.Set(cmd, command.DefaultExecutionOptions(), res, &fakes.OutputFile{abOutPath, string(wantOutput)}, fakes.StdOut("Done"))

	tmpfsdir, err := os.MkdirTemp("/dev/shm", "reproxytesttmp")
	if err != nil {
		t.Fatal(err)
	}
	oldtmp, oldTmpExisted := os.LookupEnv("TMPDIR")
	os.Setenv("TMPDIR", tmpfsdir)
	resetTmpDir := func() {
		if oldTmpExisted {
			os.Setenv("TMPDIR", oldtmp)
		} else {
			os.Unsetenv("TMPDIR")
		}
	}
	t.Cleanup(resetTmpDir)

	t.Cleanup(func() { os.RemoveAll(tmpfsdir) })

	got, err := server.RunCommand(ctx, req)
	resetTmpDir()

	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{
		Result: &cpb.CommandResult{
			Status: cpb.CommandResultStatus_SUCCESS,
		},
		Stdout: []byte("Done"),
	}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	path := filepath.Join(env.ExecRoot, abOutPath)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, wantOutput)
	}

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(cmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_RACING_REMOTE,
			LocalMetadata: &lpb.LocalMetadata{
				UpdatedCache: false,
				Labels:       map[string]string{"type": "tool"},
			},
			RemoteMetadata: &lpb.RemoteMetadata{
				Result:              &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
				NumInputDirectories: 1,
				NumOutputFiles:      1,
				TotalOutputBytes:    int64(len(wantOutput) + len("Done")),
				OutputFileDigests: map[string]string{
					abOutPath: digest.NewFromBlob(wantOutput).String(),
				},
			},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Fatalf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
	if _, ok := recs[0].GetLocalMetadata().GetEventTimes()[event.RacingFinalizationOverhead]; !ok {
		t.Errorf("Server logs does not have stat for %v", event.RacingFinalizationOverhead)
	}
	rm := recs[0].GetRemoteMetadata()
	if rm.GetActionDigest() == "" || rm.GetCommandDigest() == "" {
		t.Errorf("ActionDigest and/or CommandDigest is empty in RemoteMetadata: %v", rm)
	}
}

func TestRacingRemoteWins(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		LocalPool:         NewLocalPool(&subprocess.SystemExecutor{}, resMgr),
		FileMetadataStore: fmc,
		Forecast:          &Forecast{},
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	var cmdArgs []string
	var wantOutput []byte
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("mkdir %s && sleep 10 && echo hello>%s && echo Done", abPath, abOutPath)}
		wantOutput = []byte("hello\r\n")
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprint("mkdir -p %s && sleep 10 && echo hello > %s && echo Done", abPath, abOutPath)}
		wantOutput = []byte("hello\n")
	}
	ctx := context.Background()
	// Lock entire local resources to prevent local execution from running.
	release, err := resMgr.Lock(ctx, int64(runtime.NumCPU()), 1)
	if err != nil {
		t.Fatalf("Failed to lock entire local resources: %v", err)
	}
	t.Cleanup(release)
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels:           map[string]string{"type": "tool"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_RACING, ReclientTimeout: 3600},
	}
	cmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec:   &command.InputSpec{},
		OutputFiles: []string{abOutPath},
	}
	setPlatformOSFamily(cmd)
	res := &command.Result{Status: command.SuccessResultStatus}
	env.Set(cmd, command.DefaultExecutionOptions(), res, &fakes.OutputFile{abOutPath, string(wantOutput)}, fakes.StdOut("Done"))
	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{
		Result: &cpb.CommandResult{
			Status: cpb.CommandResultStatus_SUCCESS,
		},
		Stdout: []byte("Done"),
	}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	path := filepath.Join(env.ExecRoot, abOutPath)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, wantOutput)
	}

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(cmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_RACING_REMOTE,
			LocalMetadata: &lpb.LocalMetadata{
				UpdatedCache: false,
				Labels:       map[string]string{"type": "tool"},
			},
			RemoteMetadata: &lpb.RemoteMetadata{
				Result:              &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
				NumInputDirectories: 1,
				NumOutputFiles:      1,
				TotalOutputBytes:    int64(len(wantOutput) + len("Done")),
				OutputFileDigests: map[string]string{
					abOutPath: digest.NewFromBlob(wantOutput).String(),
				},
			},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Fatalf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
	if _, ok := recs[0].GetLocalMetadata().GetEventTimes()[event.RacingFinalizationOverhead]; !ok {
		t.Errorf("Server logs does not have stat for %v", event.RacingFinalizationOverhead)
	}
	rm := recs[0].GetRemoteMetadata()
	if rm.GetActionDigest() == "" || rm.GetCommandDigest() == "" {
		t.Errorf("ActionDigest and/or CommandDigest is empty in RemoteMetadata: %v", rm)
	}
}

func TestRacingRemoteFailsLocalWins(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		LocalPool:         NewLocalPool(&subprocess.SystemExecutor{}, resMgr),
		FileMetadataStore: fmc,
		Forecast:          &Forecast{},
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	var cmdArgs []string
	var wantOutput []byte
	var wantStdout string
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("mkdir %s && echo hello>%s && echo Done", abPath, abOutPath)}
		wantOutput = []byte("hello \r\n")
		wantStdout = "Done\r\n"
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprintf("mkdir -p %s && echo hello > %s && echo Done", abPath, abOutPath)}
		wantOutput = []byte("hello\n")
		wantStdout = "Done\n"
	}
	ctx := context.Background()
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels:           map[string]string{"type": "tool"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_RACING, ReclientTimeout: 3600},
	}
	cmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec:   &command.InputSpec{},
		OutputFiles: []string{abOutPath},
	}
	setPlatformOSFamily(cmd)
	res := &command.Result{Status: command.NonZeroExitResultStatus, ExitCode: -1}
	env.Set(cmd, command.DefaultExecutionOptions(), res, &fakes.OutputFile{abOutPath, string(wantOutput)}, fakes.StdOut(wantStdout))
	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{
		Result: &cpb.CommandResult{
			Status: cpb.CommandResultStatus_SUCCESS,
		},
		Stdout: []byte(wantStdout),
	}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	path := filepath.Join(env.ExecRoot, abOutPath)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, wantOutput)
	}

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(cmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_RACING_LOCAL,
			LocalMetadata: &lpb.LocalMetadata{
				UpdatedCache:    false,
				Labels:          map[string]string{"type": "tool"},
				ExecutedLocally: true,
				Result:          &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			},
			RemoteMetadata: &lpb.RemoteMetadata{},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Fatalf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
	if _, ok := recs[0].GetLocalMetadata().GetEventTimes()[event.RacingFinalizationOverhead]; !ok {
		t.Errorf("Server logs does not have stat for %v", event.RacingFinalizationOverhead)
	}
	rm := recs[0].GetRemoteMetadata()
	if rm.GetActionDigest() == "" || rm.GetCommandDigest() == "" {
		t.Errorf("ActionDigest and/or CommandDigest is empty in RemoteMetadata: %v", rm)
	}
}

func TestRacingRemoteFailsWhileLocalQueuedLocalWins(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		LocalPool:         NewLocalPool(&subprocess.SystemExecutor{}, resMgr),
		FileMetadataStore: fmc,
		Forecast:          &Forecast{},
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	var cmdArgs []string
	var wantOutput []byte
	var wantStdout string
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("mkdir %s && echo hello>%s && echo Done", abPath, abOutPath)}
		wantOutput = []byte("hello \r\n")
		wantStdout = "Done\r\n"
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprintf("mkdir -p %s && echo hello > %s && echo Done", abPath, abOutPath)}
		wantOutput = []byte("hello\n")
		wantStdout = "Done\n"
	}
	ctx := context.Background()
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels:           map[string]string{"type": "tool"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_RACING, ReclientTimeout: 3600},
	}
	cmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec:   &command.InputSpec{},
		OutputFiles: []string{abOutPath},
	}
	setPlatformOSFamily(cmd)
	res := &command.Result{Status: command.NonZeroExitResultStatus, ExitCode: -1}
	env.Set(cmd, command.DefaultExecutionOptions(), res, &fakes.OutputFile{abOutPath, string(wantOutput)}, fakes.StdOut(wantStdout))
	release, err := resMgr.Lock(context.Background(), math.MaxInt64, math.MaxInt64)
	if err != nil {
		t.Fatalf("Unable to lock all resources: %v", err)
	}
	breCtx := context.WithValue(ctx, testOnlyBlockFallbackKey, func() {
		// Sleep to ensure that a potential context cancellation could cause local execution
		// to be cancelled while waiting for local resources
		time.Sleep(100 * time.Millisecond)
		release()
	})
	got, err := server.RunCommand(breCtx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{
		Result: &cpb.CommandResult{
			Status: cpb.CommandResultStatus_SUCCESS,
		},
		Stdout: []byte(wantStdout),
	}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	path := filepath.Join(env.ExecRoot, abOutPath)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, wantOutput)
	}
	if nf := server.numFallbacks.Load(); nf != 1 {
		t.Errorf("numFallbacks expected to be 1, got %v", nf)
	}

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(cmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_RACING_LOCAL,
			LocalMetadata: &lpb.LocalMetadata{
				UpdatedCache:    false,
				Labels:          map[string]string{"type": "tool"},
				ExecutedLocally: true,
				Result:          &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			},
			RemoteMetadata: &lpb.RemoteMetadata{},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Fatalf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
	if _, ok := recs[0].GetLocalMetadata().GetEventTimes()[event.RacingFinalizationOverhead]; !ok {
		t.Errorf("Server logs does not have stat for %v", event.RacingFinalizationOverhead)
	}
	rm := recs[0].GetRemoteMetadata()
	if rm.GetActionDigest() == "" || rm.GetCommandDigest() == "" {
		t.Errorf("ActionDigest and/or CommandDigest is empty in RemoteMetadata: %v", rm)
	}
}

// TestRacing_DownloadOutputs verifies that racing respects the --download_outputs flag.
func TestRacing_DownloadOutputs(t *testing.T) {
	tests := []struct {
		name            string
		downloadOutputs bool
		winCmdArgs      []string
		cmdArgs         []string
		winOutput       string
		output          string
	}{
		{
			name:            "Test Download Outputs False",
			downloadOutputs: false,
			winCmdArgs:      []string{"cmd", "/c", "sleep 10 && echo hello>" + abPath},
			cmdArgs:         []string{"/bin/bash", "-c", "sleep 10 && echo hello > " + abPath},
			winOutput:       "hello\r\n",
			output:          "hello\n",
		},
		{
			name:            "Test Download Outputs True",
			downloadOutputs: true,
			winCmdArgs:      []string{"cmd", "/c", "sleep 10 && echo hello>" + abPath},
			cmdArgs:         []string{"/bin/bash", "-c", "sleep 10 && echo hello > " + abPath},
			winOutput:       "hello\r\n",
			output:          "hello\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env, cleanup := fakes.NewTestEnv(t)
			fmc := filemetadata.NewSingleFlightCache()
			env.Client.FileMetadataCache = fmc
			t.Cleanup(cleanup)
			testFilePath := filepath.Join(env.ExecRoot, abPath)
			execroot.AddFiles(t, env.ExecRoot, []string{abPath})
			ctx := context.Background()
			resMgr := localresources.NewDefaultManager()
			// Lock entire local resources to prevent local execution from running.
			release, err := resMgr.Lock(ctx, int64(runtime.NumCPU()), 1)
			if err != nil {
				t.Fatalf("Failed to lock entire local resources: %v", err)
			}
			t.Cleanup(release)
			server := &Server{
				LocalPool:         NewLocalPool(&subprocess.SystemExecutor{}, resMgr),
				FileMetadataStore: fmc,
				Forecast:          &Forecast{},
				MaxHoldoff:        time.Minute,
				DownloadTmp:       t.TempDir(),
			}
			server.Init()
			server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
			server.SetREClient(env.Client, func() {})
			lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
			if err != nil {
				t.Errorf("logger.New() returned error: %v", err)
			}
			server.Logger = lg
			cmdArgs := tc.cmdArgs
			wantOutput := tc.output
			if runtime.GOOS == "windows" {
				cmdArgs = tc.winCmdArgs
				wantOutput = tc.winOutput
			}
			outputFiles := []string{abPath}
			outputDirs := []string{}

			req := &ppb.RunRequest{
				Command: &cpb.Command{
					Args:     cmdArgs,
					ExecRoot: env.ExecRoot,
					Output: &cpb.OutputSpec{
						OutputFiles:       outputFiles,
						OutputDirectories: outputDirs,
					},
				},
				Labels: map[string]string{"type": "tool"},
				ExecutionOptions: &ppb.ProxyExecutionOptions{
					ExecutionStrategy: ppb.ExecutionStrategy_RACING,
					RemoteExecutionOptions: &ppb.RemoteExecutionOptions{
						DownloadOutputs: tc.downloadOutputs,
					},
					ReclientTimeout: 3600,
				},
			}
			wantCmd := &command.Command{
				Identifiers: &command.Identifiers{},
				Args:        cmdArgs,
				ExecRoot:    env.ExecRoot,
				InputSpec:   &command.InputSpec{},
				OutputFiles: outputFiles,
				OutputDirs:  outputDirs,
			}
			setPlatformOSFamily(wantCmd)
			res := &command.Result{Status: command.SuccessResultStatus}
			env.Set(wantCmd, command.DefaultExecutionOptions(), res, &fakes.OutputFile{abPath, wantOutput})
			oldDigest, err := digest.NewFromFile(testFilePath)
			if err != nil {
				t.Fatalf("digest.NewFromFile(%v) returned error: %v", testFilePath, err)
			}
			if _, err := server.RunCommand(ctx, req); err != nil {
				t.Errorf("RunCommand() returned error: %v", err)
			}
			server.DrainAndReleaseResources()
			newDigest, err := digest.NewFromFile(testFilePath)
			if err != nil {
				t.Fatalf("digest.NewFromFile(%v) returned error: %v", testFilePath, err)
			}
			if tc.downloadOutputs && oldDigest == newDigest {
				t.Errorf("Digest not updated when output should be changed. Old Digest: %v, New Digest: %v",
					oldDigest, newDigest)
			} else if !tc.downloadOutputs && oldDigest != newDigest {
				t.Errorf("Digest updated when output should be unchanged. Old Digest: %v, New Digest: %v",
					oldDigest, newDigest)
			}
		})
	}
}

// Tests that the correct outputs are downloaded when PreserveUnchangedOutputMtime flag is on and
// the mtimes of unchanged outputs is preserved.
// Note: For this flag, if local wins then it is the same as testing usual racing with local winning,
// and if there is a cache hit then the download process is the same as remote winning.
// So we only need to explicitly cover the case where remote wins for coverage.
func TestRacingRemoteWins_PreserveUnchangedOutputMtime(t *testing.T) {
	tests := []struct {
		name               string
		testFile           string
		wantChanged        bool
		wantLocallyChanged bool
		winCmdArgs         []string
		cmdArgs            []string
		winOutput          string
		output             string
	}{
		{
			name:               "Test Output Changed",
			testFile:           "changed.txt",
			wantChanged:        true,
			wantLocallyChanged: false,
			winCmdArgs:         []string{"cmd", "/c", "sleep 10 && echo hello>changed.txt"},
			cmdArgs:            []string{"/bin/bash", "-c", "sleep 10 && echo hello > changed.txt"},
			winOutput:          "hello\r\n",
			output:             "hello\n",
		},
		{
			name:               "Test Output Dir",
			testFile:           abOutPath,
			wantChanged:        false,
			wantLocallyChanged: false,
			winCmdArgs:         []string{"cmd", "/c", "sleep 10"},
			cmdArgs:            []string{"/bin/bash", "-c", "sleep 10"},
			winOutput:          "",
			output:             "",
		},
		{
			name:               "Test Output Changed Only Locally",
			testFile:           "locally_changed.txt",
			wantChanged:        false,
			wantLocallyChanged: true,
			winCmdArgs:         []string{"cmd", "/c", "sleep 10"},
			cmdArgs:            []string{"/bin/bash", "-c", "sleep 10"},
			winOutput:          "",
			output:             "",
		},
		{
			name:               "Test Output Unchanged",
			testFile:           "unchanged.txt",
			wantChanged:        false,
			wantLocallyChanged: false,
			winCmdArgs:         []string{"cmd", "/c", "sleep 10"},
			cmdArgs:            []string{"/bin/bash", "-c", "sleep 10"},
			winOutput:          "",
			output:             "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env, cleanup := fakes.NewTestEnv(t)
			fmc := filemetadata.NewSingleFlightCache()
			env.Client.FileMetadataCache = fmc
			t.Cleanup(cleanup)
			path := filepath.Join(env.ExecRoot, tc.testFile)
			execroot.AddFiles(t, env.ExecRoot, []string{tc.testFile})
			ctx := context.Background()
			if tc.wantLocallyChanged {
				// Changing file to test if it is updated by remote later and mtime is reset.
				// Instead of modifying file as a part of the command that is run locally,
				// we do it here to guarantee that the file is modifyied before remote finishes.
				ctx = context.WithValue(ctx, testOnlyBlockRemoteExecKey, func() {
					err := os.WriteFile(path, []byte("Hello"), 0755)
					if err != nil {
						t.Fatalf("os.WriteFile(%v) returned error: %v", path, err)
					}
				})
			}
			resMgr := localresources.NewDefaultManager()
			// Lock entire local resources to prevent local execution from running.
			release, err := resMgr.Lock(ctx, int64(runtime.NumCPU()), 1)
			if err != nil {
				t.Fatalf("Failed to lock entire local resources: %v", err)
			}
			t.Cleanup(release)
			server := &Server{
				LocalPool:         NewLocalPool(&subprocess.SystemExecutor{}, resMgr),
				FileMetadataStore: fmc,
				Forecast:          &Forecast{},
				MaxHoldoff:        time.Minute,
				DownloadTmp:       t.TempDir(),
			}
			server.Init()
			server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
			server.SetREClient(env.Client, func() {})
			lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
			if err != nil {
				t.Errorf("logger.New() returned error: %v", err)
			}
			server.Logger = lg
			cmdArgs := tc.cmdArgs
			wantOutput := tc.output
			if runtime.GOOS == "windows" {
				cmdArgs = tc.winCmdArgs
				wantOutput = tc.winOutput
			}
			outputFiles := []string{tc.testFile}
			outputDirs := []string{}
			// This helps test that output files are digested properly even if not expicitly specified
			if tc.testFile == abOutPath {
				outputFiles = []string{}
				outputDirs = []string{abPath}
			}
			req := &ppb.RunRequest{
				Command: &cpb.Command{
					Args:     cmdArgs,
					ExecRoot: env.ExecRoot,
					Output: &cpb.OutputSpec{
						OutputFiles:       outputFiles,
						OutputDirectories: outputDirs,
					},
				},
				Labels: map[string]string{"type": "tool"},
				ExecutionOptions: &ppb.ProxyExecutionOptions{
					ExecutionStrategy: ppb.ExecutionStrategy_RACING,
					RemoteExecutionOptions: &ppb.RemoteExecutionOptions{
						DownloadOutputs:              true,
						PreserveUnchangedOutputMtime: true,
					},
					ReclientTimeout: 3600,
				},
			}
			wantCmd := &command.Command{
				Identifiers: &command.Identifiers{},
				Args:        cmdArgs,
				ExecRoot:    env.ExecRoot,
				InputSpec:   &command.InputSpec{},
				OutputFiles: outputFiles,
				OutputDirs:  outputDirs,
			}
			setPlatformOSFamily(wantCmd)
			res := &command.Result{Status: command.SuccessResultStatus}
			env.Set(wantCmd, command.DefaultExecutionOptions(), res, &fakes.OutputFile{tc.testFile, wantOutput})
			time.Sleep(time.Second) // Wait for mtime to catch up
			oldDigest, oldMtime := getFileInfo(t, path)
			if _, err := server.RunCommand(ctx, req); err != nil {
				t.Errorf("RunCommand() returned error: %v", err)
			}
			server.DrainAndReleaseResources()
			time.Sleep(time.Second) // Wait for mtime to catch up
			newDigest, newMtime := getFileInfo(t, path)
			if tc.wantChanged {
				if oldDigest == newDigest {
					t.Errorf("Digest not updated when output should be changed. Old Digest: %v, New Digest: %v",
						oldDigest, newDigest)
				}
				if oldMtime == newMtime {
					t.Errorf("Mtime not updated when output should be changed. Old Mtime: %v, New Mtime: %v",
						oldMtime, newMtime)
				}
			} else {
				if oldDigest != newDigest {
					t.Errorf("Digest updated when output should be unchanged. Old Digest: %v, New Digest: %v",
						oldDigest, newDigest)
				}
				if oldMtime != newMtime {
					t.Errorf("Mtime updated when output should be unchanged. Old Mtime: %v, New Mtime: %v",
						oldMtime, newMtime)
				}
			}
		})
	}
}

func TestRacingRemoteWins_RelativeWorkingDir(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		LocalPool:         NewLocalPool(&subprocess.SystemExecutor{}, resMgr),
		FileMetadataStore: fmc,
		Forecast:          &Forecast{},
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	var cmdArgs []string
	var wantOutput []byte
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("mkdir %s && sleep 10 && echo hello>%s && echo Done", abPath, abOutPath)}
		wantOutput = []byte("hello\r\n")
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprintf("mkdir -p %s && sleep 10 && echo hello > %s && echo Done", abPath, abOutPath)}
		wantOutput = []byte("hello\n")
	}

	wd := "wd"
	absWD := filepath.Join(env.ExecRoot, wd)
	if err := os.MkdirAll(absWD, 0744); err != nil {
		t.Errorf("Unable to create working dir %v: %v", absWD, err)
	}
	wdABOutPath := filepath.Join(wd, abOutPath)
	ctx := context.Background()
	// Lock entire local resources to prevent local execution from running.
	release, err := resMgr.Lock(ctx, int64(runtime.NumCPU()), 1)
	if err != nil {
		t.Fatalf("Failed to lock entire local resources: %v", err)
	}
	t.Cleanup(release)
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{wdABOutPath},
			},
			WorkingDirectory: wd,
		},
		Labels:           map[string]string{"type": "tool"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_RACING, ReclientTimeout: 3600},
	}
	cmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec:   &command.InputSpec{},
		OutputFiles: []string{abOutPath},
		WorkingDir:  wd,
	}
	setPlatformOSFamily(cmd)
	res := &command.Result{Status: command.SuccessResultStatus}
	env.Set(cmd, command.DefaultExecutionOptions(), res, &fakes.OutputFile{abOutPath, string(wantOutput)}, fakes.StdOut("Done"))
	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{
		Result: &cpb.CommandResult{
			Status: cpb.CommandResultStatus_SUCCESS,
		},
		Stdout: []byte("Done"),
	}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s\nstderr=%s\nstdout=%s\n", diff, got.Stderr, got.Stdout)
	}
	path := filepath.Join(env.ExecRoot, wdABOutPath)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, wantOutput)
	}

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(cmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_RACING_REMOTE,
			LocalMetadata: &lpb.LocalMetadata{
				UpdatedCache: false,
				Labels:       map[string]string{"type": "tool"},
			},
			RemoteMetadata: &lpb.RemoteMetadata{
				Result:              &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
				NumInputDirectories: 1,
				NumOutputFiles:      1,
				TotalOutputBytes:    int64(len(wantOutput) + len("Done")),
				OutputFileDigests: map[string]string{
					abOutPath: digest.NewFromBlob(wantOutput).String(),
				},
			},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Fatalf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
	if _, ok := recs[0].GetLocalMetadata().GetEventTimes()[event.RacingFinalizationOverhead]; !ok {
		t.Errorf("Server logs does not have stat for %v", event.RacingFinalizationOverhead)
	}
	rm := recs[0].GetRemoteMetadata()
	if rm.GetActionDigest() == "" || rm.GetCommandDigest() == "" {
		t.Errorf("ActionDigest and/or CommandDigest is empty in RemoteMetadata: %v", rm)
	}
}

func TestRacingLocalWinsIfStarted(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		LocalPool:         NewLocalPool(&subprocess.SystemExecutor{}, resMgr),
		FileMetadataStore: fmc,
		Forecast:          &Forecast{},
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	var cmdArgs []string
	var wantOutput []byte
	var wantStdout string
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("mkdir %s && echo hello>%s && echo Done", abPath, abOutPath)}
		wantOutput = []byte("hello \r\n")
		wantStdout = "Done\r\n"
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprintf("mkdir -p %s && echo hello > %s && echo Done", abPath, abOutPath)}
		wantOutput = []byte("hello\n")
		wantStdout = "Done\n"
	}
	ctx := context.Background()
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels:           map[string]string{"type": "tool"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_RACING, ReclientTimeout: 3600},
	}
	cmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec:   &command.InputSpec{},
		OutputFiles: []string{abOutPath},
	}
	setPlatformOSFamily(cmd)
	res := &command.Result{Status: command.SuccessResultStatus}
	env.Set(cmd, command.DefaultExecutionOptions(), res, &fakes.OutputFile{abOutPath, string(wantOutput)})
	cCtx, cancel := context.WithCancel(ctx)
	breCtx := context.WithValue(ctx, testOnlyBlockRemoteExecKey, func() { <-cCtx.Done() })
	breCtx = context.WithValue(breCtx, testOnlyBlockLocalExecKey, func() { cancel() })
	got, err := server.RunCommand(breCtx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{
		Result: &cpb.CommandResult{
			Status: cpb.CommandResultStatus_SUCCESS,
		},
		Stdout: []byte(wantStdout),
	}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	path := filepath.Join(env.ExecRoot, abOutPath)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, wantOutput)
	}

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(cmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_RACING_LOCAL,
			LocalMetadata: &lpb.LocalMetadata{
				UpdatedCache:    false,
				Labels:          map[string]string{"type": "tool"},
				ExecutedLocally: true,
				Result:          &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			},
			RemoteMetadata: &lpb.RemoteMetadata{},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, append(cmpLogRecordsOpts, protocmp.IgnoreFields(&lpb.LogRecord{}, "remote_metadata"))...); diff != "" {
		t.Fatalf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
	if _, ok := recs[0].GetLocalMetadata().GetEventTimes()[event.RacingFinalizationOverhead]; !ok {
		t.Errorf("Server logs does not have stat for %v", event.RacingFinalizationOverhead)
	}
	rm := recs[0].GetRemoteMetadata()
	if rm.GetActionDigest() == "" || rm.GetCommandDigest() == "" {
		t.Errorf("ActionDigest and/or CommandDigest is empty in RemoteMetadata: %v", rm)
	}
}

func TestRacingemoteWins(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		LocalPool:         NewLocalPool(&subprocess.SystemExecutor{}, resMgr),
		FileMetadataStore: fmc,
		Forecast:          &Forecast{},
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	var cmdArgs []string
	var wantOutput []byte
	var wantStdout string
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("mkdir %s && echo hello>%s && echo Done", abPath, abOutPath)}
		wantOutput = []byte("hello \r\n")
		wantStdout = "Done\r\n"
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprintf("mkdir -p %s && echo hello > %s && echo Done", abPath, abOutPath)}
		wantOutput = []byte("hello\n")
		wantStdout = "Done\n"
	}
	ctx := context.Background()
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels:           map[string]string{"type": "tool"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_RACING, ReclientTimeout: 3600},
	}
	cmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec:   &command.InputSpec{},
		OutputFiles: []string{abOutPath},
	}
	setPlatformOSFamily(cmd)
	res := &command.Result{Status: command.SuccessResultStatus}
	env.Set(cmd, command.DefaultExecutionOptions(), res, &fakes.OutputFile{abOutPath, string(wantOutput)})
	cCtx, cancel := context.WithCancel(ctx)
	breCtx := context.WithValue(ctx, testOnlyBlockRemoteExecKey, func() { <-cCtx.Done() })
	got, err := server.RunCommand(breCtx, req)
	// defer cancel last so it runs first, unblocking the goroutine blocked on testOnlyBlockRemoteExec
	// and preventing a data race on setting the block function.
	t.Cleanup(cancel)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{
		Result: &cpb.CommandResult{
			Status: cpb.CommandResultStatus_SUCCESS,
		},
		Stdout: []byte(wantStdout),
	}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	path := filepath.Join(env.ExecRoot, abOutPath)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, wantOutput)
	}

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(cmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_RACING_LOCAL,
			LocalMetadata: &lpb.LocalMetadata{
				UpdatedCache:    false,
				Labels:          map[string]string{"type": "tool"},
				ExecutedLocally: true,
				Result:          &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, append(cmpLogRecordsOpts, protocmp.IgnoreFields(&lpb.LogRecord{}, "remote_metadata"))...); diff != "" {
		t.Fatalf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
	if _, ok := recs[0].GetLocalMetadata().GetEventTimes()[event.RacingFinalizationOverhead]; !ok {
		t.Errorf("Server logs does not have stat for %v", event.RacingFinalizationOverhead)
	}
	rm := recs[0].GetRemoteMetadata()
	if rm.GetActionDigest() == "" || rm.GetCommandDigest() == "" {
		t.Errorf("ActionDigest and/or CommandDigest is empty in RemoteMetadata: %v", rm)
	}
}

func TestRacingHoldoffCacheWins(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	resMgr := localresources.NewDefaultManager()
	t.Cleanup(cleanup)
	server := &Server{
		LocalPool:         NewLocalPool(&subprocess.SystemExecutor{}, resMgr),
		FileMetadataStore: fmc,
		Forecast:          &Forecast{},
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	var cmdArgs []string
	var wantOutput []byte
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("mkdir %s && sleep 10 && echo hello>%s", abPath, abOutPath)}
		wantOutput = []byte("hello\r\n")
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprintf("mkdir -p %s && sleep 10 && echo hello > %s", abPath, abOutPath)}
		wantOutput = []byte("hello\n")
	}
	ctx := context.Background()
	// Lock entire local resources to prevent local execution from running.
	release, err := resMgr.Lock(ctx, int64(runtime.NumCPU()), 1)
	if err != nil {
		t.Fatalf("Failed to lock entire local resources: %v", err)
	}
	t.Cleanup(release)
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels:           map[string]string{"type": "tool"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_RACING, ReclientTimeout: 3600},
	}
	cmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec:   &command.InputSpec{},
		OutputFiles: []string{abOutPath},
	}
	setPlatformOSFamily(cmd)
	res := &command.Result{Status: command.CacheHitResultStatus}
	env.Set(cmd, command.DefaultExecutionOptions(), res, &fakes.OutputFile{abOutPath, string(wantOutput)})
	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{
		Result: &cpb.CommandResult{
			Status: cpb.CommandResultStatus_CACHE_HIT,
		},
	}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	path := filepath.Join(env.ExecRoot, abOutPath)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, wantOutput)
	}

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(cmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
			CompletionStatus: lpb.CompletionStatus_STATUS_CACHE_HIT,
			LocalMetadata: &lpb.LocalMetadata{
				UpdatedCache: false,
				Labels:       map[string]string{"type": "tool"},
			},
			RemoteMetadata: &lpb.RemoteMetadata{
				Result:              &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
				NumInputDirectories: 1,
				NumOutputFiles:      1,
				TotalOutputBytes:    int64(len(wantOutput)),
				OutputFileDigests: map[string]string{
					abOutPath: digest.NewFromBlob(wantOutput).String(),
				},
			},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Errorf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
	rm := recs[0].GetRemoteMetadata()
	if rm.GetActionDigest() == "" || rm.GetCommandDigest() == "" {
		t.Errorf("ActionDigest and/or CommandDigest is empty in RemoteMetadata: %v", rm)
	}
}

func TestRacingHoldoffCacheWins_CanonicalWorkingDir(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	resMgr := localresources.NewDefaultManager()
	t.Cleanup(cleanup)
	server := &Server{
		LocalPool:         NewLocalPool(&subprocess.SystemExecutor{}, resMgr),
		FileMetadataStore: fmc,
		Forecast:          &Forecast{},
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	var cmdArgs []string
	var wantOutput []byte
	wd := filepath.Join("some", "wd")
	absWD := filepath.Join(env.ExecRoot, wd)
	if err := os.MkdirAll(absWD, 0744); err != nil {
		t.Errorf("Unable to create working dir %v: %v", absWD, err)
	}
	wdABOutPath := filepath.Join(wd, abOutPath)
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("mkdir %s && sleep 10 && echo hello>%s", abPath, abOutPath)}
		wantOutput = []byte("hello\r\n")
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprintf("mkdir -p %s && sleep 10 && echo hello > %s", abPath, abOutPath)}
		wantOutput = []byte("hello\n")
	}
	ctx := context.Background()
	// Lock entire local resources to prevent local execution from running.
	release, err := resMgr.Lock(ctx, int64(runtime.NumCPU()), 1)
	if err != nil {
		t.Fatalf("Failed to lock entire local resources: %v", err)
	}
	t.Cleanup(release)
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:             cmdArgs,
			ExecRoot:         env.ExecRoot,
			WorkingDirectory: wd,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{wdABOutPath},
			},
		},
		Labels: map[string]string{"type": "tool"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{
			ExecutionStrategy: ppb.ExecutionStrategy_RACING,
			ReclientTimeout:   3600,
			RemoteExecutionOptions: &ppb.RemoteExecutionOptions{
				AcceptCached:                 true,
				DoNotCache:                   false,
				DownloadOutputs:              true,
				PreserveUnchangedOutputMtime: false,
				CanonicalizeWorkingDir:       true,
			},
		},
	}
	cmd := &command.Command{
		Identifiers:      &command.Identifiers{},
		Args:             cmdArgs,
		ExecRoot:         env.ExecRoot,
		WorkingDir:       wd,
		RemoteWorkingDir: toRemoteWorkingDir(wd),
		InputSpec:        &command.InputSpec{},
		OutputFiles:      []string{abOutPath},
	}
	setPlatformOSFamily(cmd)
	res := &command.Result{Status: command.CacheHitResultStatus}
	opts := command.DefaultExecutionOptions()
	opts.DownloadOutputs = false
	env.Set(cmd, opts, res, &fakes.OutputFile{abOutPath, string(wantOutput)})
	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{
		Result: &cpb.CommandResult{
			Status: cpb.CommandResultStatus_CACHE_HIT,
		},
	}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	path := filepath.Join(env.ExecRoot, wdABOutPath)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, wantOutput)
	}

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(cmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
			CompletionStatus: lpb.CompletionStatus_STATUS_CACHE_HIT,
			LocalMetadata: &lpb.LocalMetadata{
				UpdatedCache: false,
				Labels:       map[string]string{"type": "tool"},
			},
			RemoteMetadata: &lpb.RemoteMetadata{
				Result:              &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
				NumInputDirectories: 1,
				NumOutputFiles:      1,
				TotalOutputBytes:    int64(len(wantOutput)),
				OutputFileDigests: map[string]string{
					abOutPath: digest.NewFromBlob(wantOutput).String(),
				},
			},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Errorf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
	rm := recs[0].GetRemoteMetadata()
	if rm.GetActionDigest() == "" || rm.GetCommandDigest() == "" {
		t.Errorf("ActionDigest and/or CommandDigest is empty in RemoteMetadata: %v", rm)
	}
}

func TestRacingHoldoffQuickDownload(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	resMgr := localresources.NewDefaultManager()
	t.Cleanup(cleanup)
	server := &Server{
		LocalPool:         NewLocalPool(&subprocess.SystemExecutor{}, resMgr),
		FileMetadataStore: fmc,
		Forecast:          &Forecast{minSizeForStats: 1},
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})

	// Update forecast with one datapoint taking 1s to download.
	a := actionsWithLatencies(t, map[string]string{"type": "tool"}, []int{1000})
	server.Forecast.RecordSample(a[0])
	ctx := context.Background()
	cCtx, cancel := context.WithCancel(ctx)
	go server.Forecast.Run(cCtx)
	time.Sleep(2 * time.Second)
	t.Cleanup(cancel)

	// Lock entire local resources to prevent local execution from running.
	release, err := resMgr.Lock(ctx, int64(runtime.NumCPU()), 1)
	if err != nil {
		t.Fatalf("Failed to lock entire local resources: %v", err)
	}
	t.Cleanup(release)

	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	var cmdArgs []string
	var wantOutput []byte
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("mkdir %s && sleep 10 && echo hello>%s", abPath, abOutPath)}
		wantOutput = []byte("hello\r\n")
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprintf("mkdir -p %s && sleep 10 && echo hello > %s", abPath, abOutPath)}
		wantOutput = []byte("hello\n")
	}
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels:           map[string]string{"type": "tool"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_RACING, ReclientTimeout: 3600},
	}
	cmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec:   &command.InputSpec{},
		OutputFiles: []string{abOutPath},
	}
	setPlatformOSFamily(cmd)
	res := &command.Result{Status: command.CacheHitResultStatus}
	env.Set(cmd, command.DefaultExecutionOptions(), res, &fakes.OutputFile{abOutPath, string(wantOutput)})
	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{
		Result: &cpb.CommandResult{
			Status: cpb.CommandResultStatus_CACHE_HIT,
		},
	}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	path := filepath.Join(env.ExecRoot, abOutPath)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, wantOutput)
	}

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(cmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
			CompletionStatus: lpb.CompletionStatus_STATUS_CACHE_HIT,
			LocalMetadata: &lpb.LocalMetadata{
				UpdatedCache: false,
				Labels:       map[string]string{"type": "tool"},
			},
			RemoteMetadata: &lpb.RemoteMetadata{
				Result:              &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
				NumInputDirectories: 1,
				NumOutputFiles:      1,
				TotalOutputBytes:    int64(len(wantOutput)),
				OutputFileDigests: map[string]string{
					abOutPath: digest.NewFromBlob(wantOutput).String(),
				},
			},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Errorf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
	rm := recs[0].GetRemoteMetadata()
	if rm.GetActionDigest() == "" || rm.GetCommandDigest() == "" {
		t.Errorf("ActionDigest and/or CommandDigest is empty in RemoteMetadata: %v", rm)
	}
}

func TestRacingHoldoffLongDownload(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	resMgr := localresources.NewDefaultManager()
	t.Cleanup(cleanup)
	server := &Server{
		LocalPool:         NewLocalPool(&subprocess.SystemExecutor{}, resMgr),
		FileMetadataStore: fmc,
		Forecast:          &Forecast{minSizeForStats: 1},
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})

	// Update forecast with one datapoint taking 1s to download.
	a := actionsWithLatencies(t, map[string]string{"type": "tool"}, []int{1000})
	server.Forecast.RecordSample(a[0])
	ctx := context.Background()
	cCtx, cancel := context.WithCancel(ctx)
	go server.Forecast.Run(cCtx)
	time.Sleep(2 * time.Second)
	wg := &sync.WaitGroup{}
	wg.Add(1)
	breCtx := context.WithValue(ctx, testOnlyBlockRemoteExecKey, func() {
		// Since we are testing holdoff, it is hard to ensure when we get
		// to the point where we are downloading remote artifacts, which
		// is when we call this testOnly function. Signaling the WaitGroup
		// here and blocking on it ensures we have reached this function
		// before cancelling the context and resetting the testOnly function.
		wg.Done()
		<-cCtx.Done()
	})
	t.Cleanup(cancel)
	t.Cleanup(wg.Wait)

	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	var cmdArgs []string
	var wantOutput []byte
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("mkdir %s && sleep 10 && echo hello>%s", abPath, abOutPath)}
		wantOutput = []byte("hello\r\n")
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprintf("mkdir -p %s && sleep 10 && echo hello > %s", abPath, abOutPath)}
		wantOutput = []byte("hello\n")
	}
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels:           map[string]string{"type": "tool"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_RACING, ReclientTimeout: 3600},
	}
	cmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec:   &command.InputSpec{},
		OutputFiles: []string{abOutPath},
	}
	setPlatformOSFamily(cmd)
	res := &command.Result{Status: command.CacheHitResultStatus}
	env.Set(cmd, command.DefaultExecutionOptions(), res, &fakes.OutputFile{abOutPath, string(wantOutput)})
	got, err := server.RunCommand(breCtx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{
		Result: &cpb.CommandResult{
			Status: cpb.CommandResultStatus_SUCCESS,
		},
	}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	path := filepath.Join(env.ExecRoot, abOutPath)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, wantOutput)
	}

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(cmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_RACING_LOCAL,
			LocalMetadata: &lpb.LocalMetadata{
				UpdatedCache:    false,
				ExecutedLocally: true,
				Labels:          map[string]string{"type": "tool"},
				Result:          &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			},
			RemoteMetadata: &lpb.RemoteMetadata{},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Errorf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
	rm := recs[0].GetRemoteMetadata()
	if rm.GetActionDigest() == "" || rm.GetCommandDigest() == "" {
		t.Errorf("ActionDigest and/or CommandDigest is empty in RemoteMetadata: %v", rm)
	}
}

func TestRacingHoldoffVeryLongDownloadClamped(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	resMgr := localresources.NewDefaultManager()
	t.Cleanup(cleanup)
	server := &Server{
		LocalPool:         NewLocalPool(&subprocess.SystemExecutor{}, resMgr),
		FileMetadataStore: fmc,
		Forecast:          &Forecast{minSizeForStats: 1},
		MaxHoldoff:        100 * time.Millisecond,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})

	// Update forecast with one datapoint taking 20s to download.
	a := actionsWithLatencies(t, map[string]string{"type": "tool"}, []int{20000})
	server.Forecast.RecordSample(a[0])
	ctx := context.Background()
	cCtx, cancel := context.WithCancel(ctx)
	go server.Forecast.Run(cCtx)
	time.Sleep(2 * time.Second)
	breCtx := context.WithValue(ctx, testOnlyBlockRemoteExecKey, func() {
		time.Sleep(2 * time.Second)
	})
	t.Cleanup(cancel)

	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	var cmdArgs []string
	var wantOutput []byte
	if runtime.GOOS == "windows" {
		cmdArgs = []string{"cmd", "/c", fmt.Sprintf("mkdir %s && sleep 10 && echo hello>%s", abPath, abOutPath)}
		wantOutput = []byte("hello\r\n")
	} else {
		cmdArgs = []string{"/bin/bash", "-c", fmt.Sprintf("mkdir -p %s && sleep 10 && echo hello > %s", abPath, abOutPath)}
		wantOutput = []byte("hello\n")
	}
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     cmdArgs,
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels:           map[string]string{"type": "tool"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_RACING, ReclientTimeout: 3600},
	}
	cmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        cmdArgs,
		ExecRoot:    env.ExecRoot,
		InputSpec:   &command.InputSpec{},
		OutputFiles: []string{abOutPath},
	}
	setPlatformOSFamily(cmd)
	res := &command.Result{Status: command.CacheHitResultStatus}
	env.Set(cmd, command.DefaultExecutionOptions(), res, &fakes.OutputFile{abOutPath, string(wantOutput)})
	got, err := server.RunCommand(breCtx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{
		Result: &cpb.CommandResult{
			Status: cpb.CommandResultStatus_SUCCESS,
		},
	}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	path := filepath.Join(env.ExecRoot, abOutPath)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, wantOutput) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, wantOutput)
	}

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	wantRecs := []*lpb.LogRecord{
		{
			Command:          command.ToProto(cmd),
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_RACING_LOCAL,
			LocalMetadata: &lpb.LocalMetadata{
				UpdatedCache:    false,
				ExecutedLocally: true,
				Labels:          map[string]string{"type": "tool"},
				Result:          &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			},
			RemoteMetadata: &lpb.RemoteMetadata{},
		},
	}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Errorf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
	rm := recs[0].GetRemoteMetadata()
	if rm.GetActionDigest() == "" || rm.GetCommandDigest() == "" {
		t.Errorf("ActionDigest and/or CommandDigest is empty in RemoteMetadata: %v", rm)
	}
}

func TestDupOutputs(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	t.Cleanup(cleanup)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	files := []string{"foo.h", "bar.h"}
	execroot.AddFiles(t, env.ExecRoot, files)
	ds := &stubCPPDependencyScanner{processInputsReturnValue: files}
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		MaxHoldoff:        time.Minute,
		FileMetadataStore: fmc,
		DownloadTmp:       t.TempDir(),
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(ds, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	ctx := context.Background()
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     []string{"clang", "-o", abOutPath, "a"},
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels:           map[string]string{"type": "compile", "lang": "cpp", "compiler": "clang"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_REMOTE, ReclientTimeout: 3600},
	}
	wantCmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        []string{"clang", "-o", abOutPath, "a"},
		ExecRoot:    env.ExecRoot,
		InputSpec: &command.InputSpec{
			Inputs: []string{"foo.h", "bar.h"},
		},
		OutputFiles: []string{abOutPath},
	}
	setPlatformOSFamily(wantCmd)
	res := &command.Result{Status: command.CacheHitResultStatus}
	wantStdErr := []byte("stderr")
	env.Set(wantCmd, command.DefaultExecutionOptions(), res, fakes.StdErr(wantStdErr), &fakes.OutputFile{abOutPath, "output"})
	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{
		Result: &cpb.CommandResult{
			Status:   cpb.CommandResultStatus_CACHE_HIT,
			ExitCode: 0,
		},
		Stderr: wantStdErr,
	}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}
	path := filepath.Join(env.ExecRoot, abOutPath)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("error reading from %s: %v", path, err)
	}
	if !bytes.Equal(contents, []byte("output")) {
		t.Errorf("RunCommand output %s: %q; want %q", path, contents, "output")
	}
}

func TestRunCommandError(t *testing.T) {
	ds := &stubCPPDependencyScanner{processInputsError: errors.New("cannot determine inputs")}
	resMgr := localresources.NewDefaultManager()
	execRoot := filepath.Join(t.TempDir(), "home", "user", "code")
	tests := []struct {
		name       string
		inp        *inputprocessor.InputProcessor
		req        *ppb.RunRequest
		wantStatus cpb.CommandResultStatus_Value
		wantCode   codes.Code
	}{
		{
			name: "input processor failure",
			inp:  inputprocessor.NewInputProcessorWithStubDependencyScanner(ds, false, nil, resMgr),
			req: &ppb.RunRequest{
				Command: &cpb.Command{
					Args:     []string{"a", "b", "c"},
					ExecRoot: execRoot,
				},
				Labels:           map[string]string{"type": "compile"},
				ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_REMOTE, ReclientTimeout: 3600},
			},
			wantStatus: cpb.CommandResultStatus_LOCAL_ERROR,
		},
		{
			name: "command invalid: missing execroot",
			inp:  inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr),
			req: &ppb.RunRequest{
				Command: &cpb.Command{
					Args: []string{"a", "b", "c"},
				},
				Labels:           map[string]string{"type": "compile"},
				ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_REMOTE, ReclientTimeout: 3600},
			},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "missing execution strategy",
			inp:  inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr),
			req: &ppb.RunRequest{
				Command: &cpb.Command{
					Args:     []string{"a", "b", "c"},
					ExecRoot: execRoot,
				},
				Labels:           map[string]string{"type": "compile"},
				ExecutionOptions: &ppb.ProxyExecutionOptions{ReclientTimeout: 3600},
			},
			wantStatus: cpb.CommandResultStatus_LOCAL_ERROR,
		},
		{
			name: "invalid reclient timeout",
			inp:  inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr),
			req: &ppb.RunRequest{
				Command: &cpb.Command{
					Args: []string{"a", "b", "c"},
				},
				Labels:           map[string]string{"type": "compile"},
				ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_REMOTE, ReclientTimeout: 0},
			},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "invalid reruns",
			inp:  inputprocessor.NewInputProcessorWithStubDependencyScanner(&stubCPPDependencyScanner{}, false, nil, resMgr),
			req: &ppb.RunRequest{
				Command: &cpb.Command{
					Args: []string{"a", "b", "c"},
				},
				Labels: map[string]string{"type": "compile"},
				ExecutionOptions: &ppb.ProxyExecutionOptions{
					ExecutionStrategy: ppb.ExecutionStrategy_REMOTE,
					CompareWithLocal:  true,
					NumLocalReruns:    0,
					NumRemoteReruns:   0,
				},
			},
			wantCode: codes.InvalidArgument,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env, cleanup := fakes.NewTestEnv(t)
			t.Cleanup(cleanup)
			fmc := filemetadata.NewSingleFlightCache()
			env.Client.FileMetadataCache = fmc
			server := &Server{
				MaxHoldoff:        time.Minute,
				DownloadTmp:       t.TempDir(),
				FileMetadataStore: fmc,
			}
			server.Init()
			server.SetInputProcessor(test.inp, func() {})
			server.SetREClient(env.Client, func() {})
			ctx := context.Background()
			res, err := server.RunCommand(ctx, test.req)
			if test.wantStatus != cpb.CommandResultStatus_UNKNOWN {
				if res.Result == nil || res.Result.GetStatus() != test.wantStatus {
					t.Errorf("RunCommand() = %+v, %v, want result with status %v", res, err, test.wantStatus)
				}
			} else if err == nil || status.Code(err) != test.wantCode {
				t.Errorf("RunCommand() = _, %v (%v), want error with code %v", err, status.Code(err), test.wantCode)
			}
		})
	}
}

func TestCacheSilo(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	files := []string{"foo.h", "bar.h", executablePath}
	execroot.AddFiles(t, env.ExecRoot, files)
	ds := &stubCPPDependencyScanner{
		processInputsReturnValue: []string{
			"foo.h",
			"bar.h",
		},
	}
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		CacheSilo:         "test-test",
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
		FileMetadataStore: fmc,
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(ds, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	ctx := context.Background()
	st := time.Now()
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     []string{executablePath, "-c", "c"},
			ExecRoot: env.ExecRoot,
			Output: &cpb.OutputSpec{
				OutputFiles: []string{abOutPath},
			},
		},
		Labels: map[string]string{"type": "compile", "lang": "cpp", "compiler": "clang"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{
			ExecutionStrategy: ppb.ExecutionStrategy_REMOTE,
			LogEnvironment:    true,
			ReclientTimeout:   3600,
		},
		Metadata: &ppb.Metadata{
			EventTimes:  map[string]*cpb.TimeInterval{"Event": {From: command.TimeToProto(st)}},
			Environment: []string{"FOO=1", "BAR=2"},
		},
	}
	wantCmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        []string{executablePath, "-c", "c"},
		ExecRoot:    env.ExecRoot,
		InputSpec: &command.InputSpec{
			Inputs: []string{"foo.h", "bar.h", executablePath},
		},
		OutputFiles: []string{abOutPath},
		Platform: map[string]string{
			cacheSiloKey: "test-test",
		},
	}
	setPlatformOSFamily(wantCmd)
	res := &command.Result{Status: command.CacheHitResultStatus}
	wantStdErr := "stderr"
	env.Set(wantCmd, command.DefaultExecutionOptions(), res, fakes.StdErr(wantStdErr), &fakes.OutputFile{abOutPath, "output"})

	if _, err := server.RunCommand(ctx, req); err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	rec := &lpb.LogRecord{
		Command:          command.ToProto(wantCmd),
		Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
		CompletionStatus: lpb.CompletionStatus_STATUS_CACHE_HIT,
		RemoteMetadata: &lpb.RemoteMetadata{
			Result:              &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
			NumInputFiles:       3,
			NumInputDirectories: 1,
			NumOutputFiles:      1,
			TotalOutputBytes:    12, // "output" + "stderr"
			OutputFileDigests: map[string]string{
				abOutPath: digest.NewFromBlob([]byte("output")).String(),
			},
		},
		LocalMetadata: &lpb.LocalMetadata{
			Environment: map[string]string{"FOO": "1", "BAR": "2"},
			Labels:      map[string]string{"type": "compile", "lang": "cpp", "compiler": "clang"},
		},
	}
	wantRecs := []*lpb.LogRecord{rec}
	if diff := cmp.Diff(wantRecs, recs, append(cmpLogRecordsOpts, protocmp.IgnoreFields(&lpb.RemoteMetadata{}))...); diff != "" {
		t.Errorf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestRemoteDisabled(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	files := []string{"foo.h", "bar.h", executablePath}
	execroot.AddFiles(t, env.ExecRoot, files)
	ds := &stubCPPDependencyScanner{
		processInputsReturnValue: []string{
			"foo.h",
			"bar.h",
		},
	}
	wantStdErr := "stderr"
	executor := &execStub{
		localExec: func() {
			time.Sleep(time.Second) // Hack to let mtime catch up.
			execroot.AddFileWithContent(t, filepath.Join(env.ExecRoot, "foo.o"), []byte("foo.o"))
			createDFile(t, env.ExecRoot, "foo.o", "foo.h", "bar.h")
		},
		stdErr: []byte(wantStdErr),
	}
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		LocalPool:         NewLocalPool(executor, resMgr),
		RemoteDisabled:    true,
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
		FileMetadataStore: fmc,
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(ds, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	ctx := context.Background()
	st := time.Now()
	req := &ppb.RunRequest{
		Command: &cpb.Command{
			Args:     []string{executablePath, "-c", "c"},
			ExecRoot: env.ExecRoot,
		},
		Labels:           map[string]string{"type": "compile", "lang": "cpp", "compiler": "clang"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_REMOTE, ReclientTimeout: 3600},
		Metadata: &ppb.Metadata{
			EventTimes:  map[string]*cpb.TimeInterval{"Event": {From: command.TimeToProto(st)}},
			Environment: []string{"FOO=1", "BAR=2"},
		},
	}
	wantCmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        []string{executablePath, "-c", "c"},
		ExecRoot:    env.ExecRoot,
		InputSpec:   &command.InputSpec{},
	}
	setPlatformOSFamily(wantCmd)
	res := &command.Result{Status: command.CacheHitResultStatus}
	env.Set(wantCmd, command.DefaultExecutionOptions(), res, fakes.StdErr("remoteStderr"), &fakes.OutputFile{abOutPath, "output"})

	got, err := server.RunCommand(ctx, req)
	if err != nil {
		t.Errorf("RunCommand() returned error: %v", err)
	}
	want := &ppb.RunResponse{
		Result: &cpb.CommandResult{
			Status: cpb.CommandResultStatus_SUCCESS,
		},
		Stderr: []byte(wantStdErr),
	}
	if diff := cmp.Diff(want, got, protocmp.IgnoreFields(&ppb.RunResponse{}, "execution_id"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand() returned diff in result: (-want +got)\n%s", diff)
	}

	server.DrainAndReleaseResources()
	recs, _, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	rec := &lpb.LogRecord{
		Command:          command.ToProto(wantCmd),
		Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
		CompletionStatus: lpb.CompletionStatus_STATUS_LOCAL_EXECUTION,
		LocalMetadata: &lpb.LocalMetadata{
			Result:          &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			ExecutedLocally: true,
			UpdatedCache:    false,
			Labels:          map[string]string{"type": "compile", "lang": "cpp", "compiler": "clang"},
		},
	}
	wantRecs := []*lpb.LogRecord{rec}
	if diff := cmp.Diff(wantRecs, recs, cmpLogRecordsOpts...); diff != "" {
		t.Errorf("Server logs returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestProxyInfoUptime(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	ds := &stubCPPDependencyScanner{}
	executor := &execStub{}
	resMgr := localresources.NewDefaultManager()
	st := time.Now()
	server := &Server{
		LocalPool:         NewLocalPool(executor, resMgr),
		RemoteDisabled:    true,
		StartTime:         st,
		MaxHoldoff:        time.Minute,
		DownloadTmp:       t.TempDir(),
		FileMetadataStore: fmc,
	}
	server.Init()
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(ds, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() {})
	lg, err := logger.New(logger.TextFormat, env.ExecRoot, stats.New(), nil, nil, nil)
	if err != nil {
		t.Errorf("error initializing logger: %v", err)
	}
	server.Logger = lg
	time.Sleep(time.Second)
	server.DrainAndReleaseResources()
	_, pInfos, err := logger.ParseFromLogDirs(logger.TextFormat, []string{env.ExecRoot})
	if err != nil {
		t.Errorf("logger.ParseFromLogDirs failed: %v", err)
	}
	if len(pInfos) != 1 {
		t.Fatalf("Wrong number of ProxyInfos found in log: want 1, got %v", len(pInfos))
	}
	pInfo := pInfos[0]
	if pInfo == nil || pInfo.GetEventTimes() == nil {
		t.Errorf("No ProxyInfo found in written log: %+v", pInfo)
	}
	var gotInterval *command.TimeInterval
	for e, et := range pInfo.GetEventTimes() {
		if e == event.ProxyUptime {
			gotInterval = command.TimeIntervalFromProto(et)
		}
	}
	if gotInterval == nil || gotInterval.To.Sub(gotInterval.From) == 0 {
		t.Errorf("Logged ProxyInfo has zero Uptime")
	}
}

func TestCleanIncludePaths(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skipf("not worth to fix it for windows. http://b/157442013")
		return
	}
	cmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        []string{executablePath, "-c", "c", "-I/a/b/c", "-Id/e", "-I /a/h/i", "-I", "/a/t/x", "-I", "r/f/g"},
		ExecRoot:    "/a",
	}
	cleanArgs := cleanIncludePaths(cmd)
	wantArgs := []string{executablePath, "-c", "c", "-Ib/c", "-Id/e", "-Ih/i", "-It/x", "-Ir/f/g"}

	if diff := cmp.Diff(wantArgs, cleanArgs); diff != "" {
		t.Errorf("cleanIncludePaths() returned diff: (-want +got)\n%s", diff)
	}
}

func TestDrainAndReleaseResourcesDoesNotBlockOnInputProcessor(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	ds := &stubCPPDependencyScanner{}
	executor := &execStub{}
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		LocalPool:         NewLocalPool(executor, resMgr),
		RemoteDisabled:    true,
		DownloadTmp:       t.TempDir(),
		FileMetadataStore: fmc,
	}
	server.Init()
	blockCleanup := make(chan bool)
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(ds, false, nil, resMgr), func() { <-blockCleanup })
	server.SetREClient(env.Client, func() {})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan bool)
	go func() {
		defer close(done)
		server.DrainAndReleaseResources()
	}()
	select {
	case <-ctx.Done():
		t.Errorf("DrainAndReleaseResources() never returned, possible deadlock")
	case <-done:
	}
	select {
	case <-server.WaitForCleanupDone():
		t.Errorf("WaitForCleanupDone() is closed before cleanup is done")
	case <-time.After(100 * time.Millisecond):
	}
	close(blockCleanup)
	select {
	case <-server.WaitForCleanupDone():
	case <-time.After(100 * time.Millisecond):
		t.Errorf("WaitForCleanupDone() is not closed after cleanup is done")
	}
}

func TestDrainAndReleaseResourcesDoesNotBlockOnREClient(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	fmc := filemetadata.NewSingleFlightCache()
	env.Client.FileMetadataCache = fmc
	t.Cleanup(cleanup)
	ds := &stubCPPDependencyScanner{}
	executor := &execStub{}
	resMgr := localresources.NewDefaultManager()
	server := &Server{
		LocalPool:         NewLocalPool(executor, resMgr),
		RemoteDisabled:    true,
		DownloadTmp:       t.TempDir(),
		FileMetadataStore: fmc,
	}
	server.Init()
	blockCleanup := make(chan bool)
	server.SetInputProcessor(inputprocessor.NewInputProcessorWithStubDependencyScanner(ds, false, nil, resMgr), func() {})
	server.SetREClient(env.Client, func() { <-blockCleanup })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan bool)
	go func() {
		defer close(done)
		server.DrainAndReleaseResources()
	}()
	select {
	case <-ctx.Done():
		t.Errorf("DrainAndReleaseResources() never returned, possible deadlock")
	case <-done:
	}
	select {
	case <-server.WaitForCleanupDone():
		t.Errorf("WaitForCleanupDone() is closed before cleanup is done")
	case <-time.After(100 * time.Millisecond):
	}
	close(blockCleanup)
	select {
	case <-server.WaitForCleanupDone():
	case <-time.After(100 * time.Millisecond):
		t.Errorf("WaitForCleanupDone() is not closed after cleanup is done")
	}
}

type execStub struct {
	localExec func()
	stdErr    []byte
	stdOut    []byte
}

func (e *execStub) ExecuteWithOutErr(ctx context.Context, cmd *command.Command, oe outerr.OutErr) error {
	e.localExec()
	oe.WriteOut(e.stdOut)
	oe.WriteErr(e.stdErr)
	return nil
}

type stubCPPDependencyScanner struct {
	processInputsReturnValue []string
	processInputsError       error
	capabilities             *spb.CapabilitiesResponse
	counter                  int
}

func (s *stubCPPDependencyScanner) ProcessInputs(context.Context, string, []string, string, string, []string) ([]string, bool, error) {
	s.counter++
	return s.processInputsReturnValue, false, s.processInputsError
}

func (s *stubCPPDependencyScanner) Capabilities() *spb.CapabilitiesResponse {
	return s.capabilities
}

func TestSetPlatformOSfamily(t *testing.T) {
	defaultOSFamily := knownOSFamilies[runtime.GOOS]
	for _, tc := range []struct {
		name         string
		platform     map[string]string
		wantOSFamily string
	}{
		{
			name:         "noProperty",
			platform:     map[string]string{},
			wantOSFamily: defaultOSFamily,
		},
		{
			name: "OSFamilyLinux",
			platform: map[string]string{
				osFamilyKey: "Linux",
			},
			wantOSFamily: "Linux",
		},
		{
			name: "OSFamilyWindows",
			platform: map[string]string{
				osFamilyKey: "Windows",
			},
			wantOSFamily: "Windows",
		},
		{
			name: "Pool",
			platform: map[string]string{
				poolKey: "worker-pool-name",
			},
			wantOSFamily: "",
		},
		{
			name: "label",
			platform: map[string]string{
				"label:osFamily": "worker-label",
			},
			wantOSFamily: "",
		},
	} {
		tc := tc // rebind tc into this lexical scope
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cmd := &command.Command{
				Platform: tc.platform,
				Identifiers: &command.Identifiers{
					ExecutionID: "execution-id",
				},
			}
			setPlatformOSFamily(cmd)
			if got, want := tc.platform[osFamilyKey], tc.wantOSFamily; got != want {
				t.Errorf("OSFamily=%q; want=%q", got, want)
			}
		})
	}
}

func TestDeleteOldLogFiles(t *testing.T) {
	t.Parallel()
	env, cleanup := fakes.NewTestEnv(t)
	t.Cleanup(cleanup)

	dir := env.ExecRoot
	tests := []struct {
		name        string
		wantDeleted bool
	}{
		{name: "non-matching.txt", wantDeleted: false},
		// name==pattern
		{name: "reproxy", wantDeleted: true},
		{name: "rewrapper", wantDeleted: true},
		{name: "bootstrap", wantDeleted: true},
		// name starts with pattern
		{name: "reproxy.host.user.log.INFO.20211115-134920.3317660", wantDeleted: true},
		{name: "rewrapper.host.user.log.INFO.20211115-134920.3317660", wantDeleted: true},
		{name: "bootstrap.host.user.log.INFO.20211115-134920.3317660", wantDeleted: true},
		// name starts with pattern (no dot separator)
		{name: "reproxyhost.user.log.INFO.20211115-134920.3317660", wantDeleted: true},
		{name: "rewrapperhost.user.log.INFO.20211115-134920.3317660", wantDeleted: true},
		{name: "bootstraphost.user.log.INFO.20211115-134920.3317660", wantDeleted: true},
		// name starts with pattern (but case does not match)
		{name: "Reproxy.host.user.log.INFO.20211115-134920.3317660", wantDeleted: false},
		{name: "Rewrapper.host.user.log.INFO.20211115-134920.3317660", wantDeleted: false},
		{name: "Bootstrap.host.user.log.INFO.20211115-134920.3317660", wantDeleted: false},
		// name contains pattern (but does not start with it)
		{name: "prefixreproxy.log", wantDeleted: false},
		{name: "prefixrewrapper.log", wantDeleted: false},
		{name: "prefixbootstrap.log", wantDeleted: false},
		// truncate interceptor dump file
		{name: "execution-827dc809-64a9-47cb-bbb2-38d47a68bc78.dump", wantDeleted: true},
		{name: "prefix-execution-827dc809-64a9-47cb-bbb2-38d47a68bc78.dump", wantDeleted: false},
		{name: "execution-827dc809-64a9-47cb-bbb2-38d47a68bc78.dumpsuffix", wantDeleted: false},
		{name: "execution-827dc809-64a9-47cb-bbb2-38d47a68bc78.log", wantDeleted: false},
		{name: "Execution-827dc809-64a9-47cb-bbb2-38d47a68bc78.dump", wantDeleted: false},
		{name: "execution-827dc809-64a9-47cb-bbb2-38d47a68bc789.dump", wantDeleted: false},
	}
	for _, tc := range tests {
		filename := filepath.Join(dir, tc.name)
		file, err := os.Create(filename)
		if err != nil {
			t.Errorf("Failed to create file %q, err: %v", filename, err)
		}
		err = file.Close()
		if err != nil {
			t.Errorf("Failed to close file %q, err: %v", filename, err)
		}
		DeleteOldLogFiles(-1, dir)
		_, err = os.Stat(filename)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%q: %v", tc.name, err)
			continue
		}
		gotDeleted := err != nil
		if tc.wantDeleted != gotDeleted {
			t.Errorf("%q: wantDeleted=%v, gotDeleted=%v", tc.name, tc.wantDeleted, gotDeleted)
		}
		if !gotDeleted {
			os.Remove(filename)
		}
	}
}

func TestWindowedCountNoWindow(t *testing.T) {
	t.Parallel()
	wc := &windowedCount{}
	wc.Add(1)
	time.Sleep(100 * time.Millisecond)
	wc.Add(3)
	time.Sleep(100 * time.Millisecond)
	wc.Add(5)
	time.Sleep(100 * time.Millisecond)
	if got := wc.Load(); got != 9 {
		t.Errorf("windowedCount.Load() returned %v wanted 9", got)
	}
}

func TestWindowedCountWithWindow(t *testing.T) {
	t.Parallel()
	wc := &windowedCount{window: 350 * time.Millisecond}
	wc.Add(1)
	time.Sleep(100 * time.Millisecond)
	wc.Add(3)
	time.Sleep(100 * time.Millisecond)
	wc.Add(5)
	time.Sleep(100 * time.Millisecond)
	if got := wc.Load(); got != 9 {
		t.Errorf("windowedCount.Load() returned %v wanted 9", got)
	}
	time.Sleep(100 * time.Millisecond)
	if got := wc.Load(); got != 8 {
		t.Errorf("windowedCount.Load() returned %v wanted 8", got)
	}
	time.Sleep(100 * time.Millisecond)
	if got := wc.Load(); got != 5 {
		t.Errorf("windowedCount.Load() returned %v wanted 5", got)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func getFileInfo(t *testing.T, path string) (digest.Digest, time.Time) {
	d, err := digest.NewFromFile(path)
	if err != nil {
		t.Fatalf("digest.NewFromFile(%v) returned error: %v", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat(%v) returned error: %v", path, err)
	}
	return d, info.ModTime()
}
