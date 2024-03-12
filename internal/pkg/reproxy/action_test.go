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

package reproxy

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	ppb "github.com/bazelbuild/reclient/api/proxy"
	"github.com/bazelbuild/reclient/internal/pkg/execroot"
	"github.com/bazelbuild/reclient/internal/pkg/localresources"
	"github.com/bazelbuild/reclient/internal/pkg/subprocess"
	"github.com/bazelbuild/reclient/pkg/inputprocessor"
	cpb "github.com/bazelbuild/remote-apis-sdks/go/api/command"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/digest"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/fakes"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/filemetadata"
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
