// Copyright 2024 Google LLC
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
//
// Example invocation:
//
// bazelisk test //internal/pkg/reproxy:reproxy_test --test_filter=TestInOutFiles --test_arg=--logtostderr --test_output=streamed

package reproxy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	lpb "github.com/bazelbuild/reclient/api/log"
	ppb "github.com/bazelbuild/reclient/api/proxy"
	"github.com/bazelbuild/reclient/internal/pkg/execroot"
	"github.com/bazelbuild/reclient/internal/pkg/localresources"
	"github.com/bazelbuild/reclient/internal/pkg/logger"
	"github.com/bazelbuild/reclient/internal/pkg/subprocess"
	"github.com/bazelbuild/reclient/pkg/inputprocessor"
	cpb "github.com/bazelbuild/remote-apis-sdks/go/api/command"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/digest"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/fakes"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/filemetadata"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/outerr"
)

// TestDownloadRegex verifies that --download_regex controls which files to download from output list.
func TestDownloadRegex(t *testing.T) {
	fooPath := filepath.Join("a", "b", "common_parent_dir", "foo.cpp")
	barPath := filepath.Join("a", "b", "common_parent_dir", "bar.cpp")
	bazPath := filepath.Join("a", "b", "baz.cpp")
	generatedDownloadFiles := []string{fooPath, barPath, bazPath}
	execStrategies := []ppb.ExecutionStrategy_Value{ppb.ExecutionStrategy_RACING, ppb.ExecutionStrategy_REMOTE}
	echoCmd := ""
	for _, p := range generatedDownloadFiles {
		echoCmd += " && echo hello>" + p
	}

	tests := []struct {
		name                string
		downloadOutputs     bool
		winCmdArgs          []string
		cmdArgs             []string
		winOutput           string
		output              string
		downloadRegex       string
		remoteDownloadFiles map[string]bool
	}{
		{
			name:                "Test download_outputs false is ignored with a positive download_regex filter",
			downloadOutputs:     false,
			winCmdArgs:          []string{"cmd", "/c", "echo start" + echoCmd},
			cmdArgs:             []string{"/bin/bash", "-c", "echo start" + echoCmd},
			winOutput:           "hello\r\n",
			output:              "hello\n",
			downloadRegex:       ".*ba.*",
			remoteDownloadFiles: map[string]bool{barPath: true, bazPath: true},
		},
		{
			name:                "Test download_outputs true is ignored with a negative download_regex filter",
			downloadOutputs:     true,
			winCmdArgs:          []string{"cmd", "/c", "echo start" + echoCmd},
			cmdArgs:             []string{"/bin/bash", "-c", "echo start" + echoCmd},
			winOutput:           "hello\r\n",
			output:              "hello\n",
			downloadRegex:       "-.*ba.*",
			remoteDownloadFiles: map[string]bool{fooPath: true},
		},
		{
			name:            "Test fallback to download_outputs true for an invalid positive download_regex filter",
			downloadOutputs: true,
			winCmdArgs:      []string{"cmd", "/c", "echo start" + echoCmd},
			cmdArgs:         []string{"/bin/bash", "-c", "echo start" + echoCmd},
			winOutput:       "hello\r\n",
			output:          "hello\n",
			// * just means to match 0 to n repetitions of the preceding element,
			// without a preceding element, asterisk by itself is not a valid regex, .* is the correct format.
			// download_outputs will be the fallback in case if the regex cannot be compiled.
			downloadRegex:       "*",
			remoteDownloadFiles: map[string]bool{fooPath: true, barPath: true, bazPath: true},
		},
		{
			name:                "Test fallback to download_outputs false for an invalid positive download_regex filter",
			downloadOutputs:     false,
			winCmdArgs:          []string{"cmd", "/c", "echo start" + echoCmd},
			cmdArgs:             []string{"/bin/bash", "-c", "echo start" + echoCmd},
			winOutput:           "hello\r\n",
			output:              "hello\n",
			downloadRegex:       "*",
			remoteDownloadFiles: map[string]bool{},
		},
		{
			name:                "Test fallback to download_outputs true for an invalid negative download_regex filter",
			downloadOutputs:     true,
			winCmdArgs:          []string{"cmd", "/c", "echo start" + echoCmd},
			cmdArgs:             []string{"/bin/bash", "-c", "echo start" + echoCmd},
			winOutput:           "hello\r\n",
			output:              "hello\n",
			downloadRegex:       "-*",
			remoteDownloadFiles: map[string]bool{fooPath: true, barPath: true, bazPath: true},
		},
		{
			name:                "Test fallback to download_outputs false for an invalid negative download_regex filter",
			downloadOutputs:     false,
			winCmdArgs:          []string{"cmd", "/c", "echo start" + echoCmd},
			cmdArgs:             []string{"/bin/bash", "-c", "echo start" + echoCmd},
			winOutput:           "hello\r\n",
			output:              "hello\n",
			downloadRegex:       "-*",
			remoteDownloadFiles: map[string]bool{},
		},
		{
			name:                "Test positive download_regex filter that matches more than one files",
			downloadOutputs:     true,
			winCmdArgs:          []string{"cmd", "/c", "echo start" + echoCmd},
			cmdArgs:             []string{"/bin/bash", "-c", "echo start" + echoCmd},
			winOutput:           "hello\r\n",
			output:              "hello\n",
			downloadRegex:       ".*common_parent_dir.*",
			remoteDownloadFiles: map[string]bool{fooPath: true, barPath: true},
		},
		{
			name:                "Test negative download_regex filter that matches more than one files",
			downloadOutputs:     true,
			winCmdArgs:          []string{"cmd", "/c", "echo start" + echoCmd},
			cmdArgs:             []string{"/bin/bash", "-c", "echo start" + echoCmd},
			winOutput:           "hello\r\n",
			output:              "hello\n",
			downloadRegex:       "-.*bar|baz.*",
			remoteDownloadFiles: map[string]bool{fooPath: true},
		},
		{
			name:                "Test negative download_regex filter that matches everything",
			downloadOutputs:     true,
			winCmdArgs:          []string{"cmd", "/c", "echo start" + echoCmd},
			cmdArgs:             []string{"/bin/bash", "-c", "echo start" + echoCmd},
			winOutput:           "hello\r\n",
			output:              "hello\n",
			downloadRegex:       "-.*",
			remoteDownloadFiles: map[string]bool{},
		},
		{
			name:                "Test positive download_regex filter that matches everything",
			downloadOutputs:     false,
			winCmdArgs:          []string{"cmd", "/c", "echo start" + echoCmd},
			cmdArgs:             []string{"/bin/bash", "-c", "echo start" + echoCmd},
			winOutput:           "hello\r\n",
			output:              "hello\n",
			downloadRegex:       ".*",
			remoteDownloadFiles: map[string]bool{fooPath: true, barPath: true, bazPath: true},
		},
	}

	for _, exec := range execStrategies {
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				env, cleanup := fakes.NewTestEnv(t)
				fmc := filemetadata.NewSingleFlightCache()
				env.Client.FileMetadataCache = fmc
				t.Cleanup(cleanup)

				ctx := context.Background()
				resMgr := localresources.NewDefaultManager()
				// Lock entire local resources to prevent local execution from running.
				if exec != ppb.ExecutionStrategy_LOCAL {
					release, err := resMgr.Lock(ctx, int64(runtime.NumCPU()), 1)
					if err != nil {
						t.Fatalf("Failed to lock entire local resources: %v", err)
					}
					t.Cleanup(release)
				}

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
				cmdArgs := tc.cmdArgs
				wantOutput := tc.output
				if runtime.GOOS == "windows" {
					cmdArgs = tc.winCmdArgs
					wantOutput = tc.winOutput
				}
				outputFiles := generatedDownloadFiles
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
						ExecutionStrategy: exec,
						DownloadRegex:     tc.downloadRegex,
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

				var fakeOutputs []fakes.Option
				oldDgs := make(map[string]digest.Digest)
				for _, f := range generatedDownloadFiles {
					fullPath := filepath.Join(env.ExecRoot, f)
					execroot.AddFiles(t, env.ExecRoot, []string{f})
					dg, err := digest.NewFromFile(fullPath)
					if err != nil {
						t.Fatalf("Get old digest with digest.NewFromFile(%v) returned error: %v", fullPath, err)
					}
					oldDgs[f] = dg
					fakeOutputs = append(fakeOutputs, &fakes.OutputFile{Path: f, Contents: wantOutput})
				}
				env.Set(wantCmd, command.DefaultExecutionOptions(), res, fakeOutputs...)

				if _, err := server.RunCommand(ctx, req); err != nil {
					t.Errorf("RunCommand() returned error: %v", err)
				}
				server.DrainAndReleaseResources()
				time.Sleep(100)
				// Get all the newDgs after run the cmd.
				newDgs := make(map[string]digest.Digest)
				for _, f := range generatedDownloadFiles {
					fullPath := filepath.Join(env.ExecRoot, f)
					dg, err := digest.NewFromFile(fullPath)
					if err != nil {
						t.Fatalf("Get new digest.NewFromFile(%v) returned error: %v", fullPath, err)
					}
					newDgs[f] = dg
				}

				for _, f := range generatedDownloadFiles {
					if _, ok := tc.remoteDownloadFiles[f]; ok {
						if oldDgs[f] == newDgs[f] {
							t.Errorf("Digest for file %v not updated when output should be changed with exec strategy %v. Old Digest: %v, New Digest: %v", f, exec, oldDgs[f], newDgs[f])
						}
					} else {
						if oldDgs[f] != newDgs[f] {
							t.Errorf("Digest for file %v updated when output should be unchanged with exec strategy %v. Old Digest: %v, New Digest: %v", f, exec, oldDgs[f], newDgs[f])
						}
					}
				}
			})
		}
	}
}

func TestInOutFiles(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping test on non-Linux platform")
	}
	tests := []struct {
		name        string
		execRoot    string
		workingDir  string   // workingDir is relative to execRoot.
		inputs      []string // inputs are relative to execRoot.
		outputFiles []string // outputs are relative to workingDir or absolute.
		outputDirs  []string
		wantInOut   []string // wantInOut files should has execRoot as prefix.
	}{
		{
			// For more details, see local re-run failures in b/435160524.
			name:       "Input file is inside of the output directory",
			execRoot:   "/usr/local/home/aosp",
			workingDir: ".",
			inputs: []string{
				// This is actually a file, not a directory!
				"out/soong/.intermediates/external/jsr305/jsr305/linux_glibc_common/javac/anno/list",
			},
			outputDirs: []string{
				"out/soong/.intermediates/external/jsr305/jsr305/linux_glibc_common/javac/classes",
				"out/soong/.intermediates/external/jsr305/jsr305/linux_glibc_common/javac/anno",
			},
			wantInOut: []string{
				"/usr/local/home/aosp/out/soong/.intermediates/external/jsr305/jsr305/linux_glibc_common/javac/anno/list",
			},
		},
		{
			name:       "Input file has similar prefix as output directory",
			execRoot:   "/usr/local/home/aosp",
			workingDir: ".",
			inputs:     []string{"fooooo/bar.txt"},
			outputDirs: []string{"foo"},
			wantInOut:  []string{},
		},
		{
			name:       "Input directory is the same as output directory",
			execRoot:   "/usr/local/home/aosp",
			workingDir: ".",
			inputs:     []string{"out"},
			outputDirs: []string{"out"},
			wantInOut:  []string{"/usr/local/home/aosp/out"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := &action{
				cmd: &command.Command{
					ExecRoot:   tc.execRoot,
					WorkingDir: tc.workingDir,
					InputSpec: &command.InputSpec{
						Inputs: tc.inputs,
					},
					OutputFiles: tc.outputFiles,
					OutputDirs:  tc.outputDirs,
				},
			}
			gotInOut := a.inOutFiles()
			sort.Strings(gotInOut)
			sort.Strings(tc.wantInOut)
			if fmt.Sprintf("%v", gotInOut) != fmt.Sprintf("%v", tc.wantInOut) {
				t.Errorf("inOutFiles() = %v, want %v", gotInOut, tc.wantInOut)
			}
		})
	}
}

func TestGetCachedResult(t *testing.T) {
	dFileRel := filepath.Join("sub", "foo.d")
	depsFileRel := filepath.Join("sub", "foo.d.deps")
	outDirRel := "out_dir"
	staleDirFileRel := filepath.Join(outDirRel, "stale.txt")
	checkFileContent := func(t *testing.T, path, want string) {
		t.Helper()
		if got, err := os.ReadFile(path); err != nil || string(got) != want {
			t.Errorf("os.ReadFile(%s) = (%q, %v), want (%q, nil)", path, got, err, want)
		}
	}
	checkFileNotExist := func(t *testing.T, path string) {
		t.Helper()
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("os.Stat(%s) error = %v, want not-exist", path, err)
		}
	}
	for _, tc := range []struct {
		name                 string
		withDepsFile         bool
		staleDepsFile        bool
		withOutDir           bool
		downloadOutputs      bool
		downloadRegex        string
		clearCAS             bool
		keepDepsInCAS        bool
		wantStatus           command.ResultStatus
		wantCacheHit         bool
		wantValidCache       bool
		wantOutputDownloaded bool
		wantStaleRemoved     bool
		wantDepsFileReads    int
		wantOutputReads      int
	}{
		{
			name:                 "valid_cache_hit",
			withOutDir:           true,
			downloadOutputs:      true,
			wantStatus:           command.CacheHitResultStatus,
			wantCacheHit:         true,
			wantValidCache:       true,
			wantOutputDownloaded: true,
			wantStaleRemoved:     true,
			wantOutputReads:      1,
		},
		{
			name:                 "valid_cache_hit_with_deps",
			withDepsFile:         true,
			withOutDir:           true,
			downloadOutputs:      true,
			wantStatus:           command.CacheHitResultStatus,
			wantCacheHit:         true,
			wantValidCache:       true,
			wantOutputDownloaded: true,
			wantStaleRemoved:     true,
			wantDepsFileReads:    2,
			wantOutputReads:      1,
		},
		{
			name:              "stale_deps_skips_output_download",
			withDepsFile:      true,
			staleDepsFile:     true,
			withOutDir:        true,
			downloadOutputs:   true,
			wantStatus:        command.LocalErrorResultStatus,
			wantCacheHit:      true,
			wantDepsFileReads: 1,
		},
		{
			name:              "download_outputs_false_skips_output_download",
			withDepsFile:      true,
			withOutDir:        true,
			wantStatus:        command.CacheHitResultStatus,
			wantCacheHit:      true,
			wantValidCache:    true,
			wantDepsFileReads: 1,
		},
		{
			name:              "download_regex_excludes_output",
			withDepsFile:      true,
			withOutDir:        true,
			downloadOutputs:   true,
			downloadRegex:     "-.*",
			wantStatus:        command.CacheHitResultStatus,
			wantCacheHit:      true,
			wantValidCache:    true,
			wantDepsFileReads: 1,
		},
		{
			name:                 "download_regex_overrides_download_outputs_false",
			withDepsFile:         true,
			withOutDir:           true,
			downloadRegex:        ".*",
			wantStatus:           command.CacheHitResultStatus,
			wantCacheHit:         true,
			wantValidCache:       true,
			wantOutputDownloaded: true,
			wantDepsFileReads:    1,
			wantOutputReads:      1,
		},
		{
			name:            "flatten_outputs_failure_resets_cache_hit",
			withOutDir:      true,
			downloadOutputs: true,
			clearCAS:        true,
			wantStatus:      command.RemoteErrorResultStatus,
		},
		{
			name:              "download_deps_failure_resets_cache_hit",
			withDepsFile:      true,
			downloadOutputs:   true,
			clearCAS:          true,
			wantStatus:        command.RemoteErrorResultStatus,
			wantDepsFileReads: 1,
		},
		{
			name:              "download_output_failure_with_deps_resets_cache_hit",
			withDepsFile:      true,
			downloadOutputs:   true,
			clearCAS:          true,
			keepDepsInCAS:     true,
			wantStatus:        command.RemoteErrorResultStatus,
			wantDepsFileReads: 2,
		},
		{
			name:            "download_output_failure_resets_cache_hit",
			downloadOutputs: true,
			clearCAS:        true,
			wantStatus:      command.RemoteErrorResultStatus,
			wantOutputReads: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, cleanup := fakes.NewTestEnv(t)
			t.Cleanup(cleanup)
			filemetadata.ResetGlobalCache()
			fmc := filemetadata.NewSingleFlightCache()
			env.Client.FileMetadataCache = fmc
			execroot.AddFileWithContent(t, filepath.Join(env.ExecRoot, "foo.h"), []byte("foo"))
			headerBlob := "foo"
			if tc.staleDepsFile {
				headerBlob = "stale"
			}
			depsContent := fmt.Sprintf("foo.h:%s\n", digest.NewFromBlob([]byte(headerBlob)))
			cmd := &command.Command{
				Identifiers: &command.Identifiers{ExecutionID: "exec-id"},
				Args:        []string{"tool"},
				ExecRoot:    env.ExecRoot,
				InputSpec:   &command.InputSpec{},
				OutputFiles: []string{"foo.o"},
			}
			outs := []fakes.Option{&fakes.OutputFile{Path: "foo.o", Contents: "foo.o"}}
			if tc.withDepsFile {
				cmd.OutputFiles = append(cmd.OutputFiles, depsFileRel)
				outs = append(outs, &fakes.OutputFile{Path: depsFileRel, Contents: depsContent})
			}
			if tc.withOutDir {
				if err := os.MkdirAll(filepath.Join(env.ExecRoot, outDirRel), 0755); err != nil {
					t.Fatalf("MkdirAll(%v) failed: %v", outDirRel, err)
				}
				cmd.OutputDirs = []string{outDirRel}
				outs = append(outs, &fakes.OutputDir{Path: outDirRel})
			}
			env.Set(cmd, command.DefaultExecutionOptions(), &command.Result{Status: command.CacheHitResultStatus}, outs...)
			if tc.withOutDir {
				execroot.AddFileWithContent(t, filepath.Join(env.ExecRoot, staleDirFileRel), []byte("stale-dir-file"))
			}
			if tc.clearCAS {
				env.Server.CAS.Clear()
				if tc.keepDepsInCAS {
					env.Server.CAS.Put([]byte(depsContent))
				}
			}
			rec := logger.NewLogRecord()
			rec.LocalMetadata = &lpb.LocalMetadata{}
			a := &action{
				cmd:           cmd,
				rOpt:          &ppb.RemoteExecutionOptions{AcceptCached: true, DownloadOutputs: tc.downloadOutputs},
				fmc:           fmc,
				rec:           rec,
				oe:            outerr.NewRecordingOutErr(),
				downloadRegex: tc.downloadRegex,
			}
			if tc.withDepsFile {
				a.dFile, a.depsFile = dFileRel, depsFileRel
			}
			ctx := t.Context()
			if err := a.createExecContext(ctx, env.Client); err != nil {
				t.Fatalf("createExecContext() failed: %v", err)
			}
			a.getCachedResult(ctx)
			if a.res.Status != tc.wantStatus {
				t.Errorf("a.res.Status = %v, want %v", a.res.Status, tc.wantStatus)
			}
			if got := a.rec.RemoteMetadata.GetCacheHit(); got != tc.wantCacheHit {
				t.Errorf("RemoteMetadata.CacheHit = %v, want %v", got, tc.wantCacheHit)
			}
			if got := a.rec.LocalMetadata.GetValidCacheHit(); got != tc.wantValidCache {
				t.Errorf("LocalMetadata.ValidCacheHit = %v, want %v", got, tc.wantValidCache)
			}
			if reads := env.Server.CAS.BlobReads(digest.NewFromBlob([]byte("foo.o"))); reads != tc.wantOutputReads {
				t.Errorf("CAS.BlobReads(foo.o) = %d, want %d", reads, tc.wantOutputReads)
			}
			if reads := env.Server.CAS.BlobReads(digest.NewFromBlob([]byte(depsContent))); reads != tc.wantDepsFileReads {
				t.Errorf("CAS.BlobReads(foo.d.deps) = %d, want %d", reads, tc.wantDepsFileReads)
			}
			fooPath := filepath.Join(env.ExecRoot, "foo.o")
			if tc.wantOutputDownloaded {
				checkFileContent(t, fooPath, "foo.o")
			} else {
				checkFileNotExist(t, fooPath)
			}
			if tc.withOutDir {
				stalePath := filepath.Join(env.ExecRoot, staleDirFileRel)
				if tc.wantStaleRemoved {
					checkFileNotExist(t, stalePath)
				} else {
					checkFileContent(t, stalePath, "stale-dir-file")
				}
			}
			depsPath := filepath.Join(env.ExecRoot, depsFileRel)
			if tc.withDepsFile && tc.wantValidCache {
				checkFileContent(t, depsPath, depsContent)
			} else {
				checkFileNotExist(t, depsPath)
				var fe *filemetadata.FileError
				if err := fmc.Get(depsPath).Err; !errors.As(err, &fe) || !fe.IsNotFound {
					t.Errorf("fmc.Get(foo.d.deps).Err = %v, want not-found FileError", err)
				}
			}
		})
	}
}
