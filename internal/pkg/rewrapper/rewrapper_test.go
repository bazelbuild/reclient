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

package rewrapper

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/testing/protocmp"

	ppb "github.com/bazelbuild/reclient/api/proxy"

	cpb "github.com/bazelbuild/remote-apis-sdks/go/api/command"
)

type proxyStub struct {
	req *ppb.RunRequest
	err error
}

func (s *proxyStub) RunCommand(_ context.Context, req *ppb.RunRequest, _ ...grpc.CallOption) (*ppb.RunResponse, error) {
	s.req = req
	return nil, s.err
}

func TestRunCommand(t *testing.T) {
	envKey := "KEY"
	envVal := "VALUE"
	if err := os.Setenv(envKey, envVal); err != nil {
		t.Fatalf("Failed to set environment variable")
	}
	defer os.Unsetenv(envKey)
	inputs := createRspFile(t, []string{"foo.h", "\"bar.h\""})
	defer os.RemoveAll(inputs)
	outputs := createRspFile(t, []string{"foo.o.extra"})
	defer os.RemoveAll(outputs)
	cmd := []string{"clang", "-c", "test.c"}
	st := time.Now()
	opts := &CommandOptions{
		CommandID:              "1234",
		InvocationID:           "inv001",
		ToolName:               "ninja",
		Labels:                 map[string]string{"type": "compile", "lang": "cpp"},
		ExecRoot:               "/home/user/code",
		Inputs:                 []string{"baz.h"},
		InputListPaths:         []string{inputs},
		OutputListPaths:        []string{outputs},
		OutputFiles:            []string{"foo.o"},
		OutputDirectories:      []string{"bar"},
		ExecTimeout:            time.Minute * 5,
		ExecStrategy:           "remote",
		EnvVarAllowlist:        []string{envKey},
		Platform:               map[string]string{"docker": "a1b2c3"},
		RemoteAcceptCache:      true,
		RemoteUpdateCache:      true,
		DownloadOutputs:        true,
		StartTime:              st,
		PreserveSymlink:        true,
		CanonicalizeWorkingDir: true,
	}
	want := &ppb.RunRequest{
		Command: &cpb.Command{
			Identifiers: &cpb.Identifiers{
				CommandId:    "1234",
				InvocationId: "inv001",
				ToolName:     "ninja",
			},
			ExecRoot: "/home/user/code",
			Input: &cpb.InputSpec{
				EnvironmentVariables: map[string]string{envKey: envVal},
				Inputs:               []string{"baz.h", "foo.h", "bar.h"},
				SymlinkBehavior:      cpb.SymlinkBehaviorType_PRESERVE,
			},
			Output: &cpb.OutputSpec{
				OutputFiles:       []string{"foo.o", "foo.o.extra"},
				OutputDirectories: []string{"bar"},
			},
			Args:             []string{"clang", "-c", "test.c"},
			ExecutionTimeout: 5 * 60,
			Platform:         map[string]string{"docker": "a1b2c3"},
		},
		Labels: map[string]string{"type": "compile", "lang": "cpp"},
		ExecutionOptions: &ppb.ProxyExecutionOptions{
			ExecutionStrategy: ppb.ExecutionStrategy_REMOTE,
			RemoteExecutionOptions: &ppb.RemoteExecutionOptions{
				AcceptCached:           true,
				DoNotCache:             false,
				DownloadOutputs:        true,
				CanonicalizeWorkingDir: true,
			},
			LocalExecutionOptions: &ppb.LocalExecutionOptions{
				AcceptCached: true,
			},
		},
		Metadata: &ppb.Metadata{
			EventTimes: map[string]*cpb.TimeInterval{WrapperOverheadKey: &cpb.TimeInterval{From: command.TimeToProto(st)}},
		},
	}
	p := &proxyStub{}
	if _, err := RunCommand(context.Background(), time.Hour, p, cmd, opts); err != nil {
		t.Errorf("RunCommand(%v,%v) returned error: %v", cmd, opts, err)
	}
	strSliceCmp := protocmp.SortRepeated(func(a, b string) bool { return a < b })
	if diff := cmp.Diff(want, p.req, strSliceCmp, protocmp.IgnoreFields(&ppb.Metadata{}, "environment"), protocmp.Transform()); diff != "" {
		t.Errorf("RunCommand(cmd, opts) created bad RunRequest. (-want +got): %s", diff)
	}
}

func TestRunCommandTimeout(t *testing.T) {
	p := &proxyStub{err: status.Error(codes.Unavailable, "error")}
	if _, err := RunCommand(context.Background(), time.Second, p, []string{}, &CommandOptions{}); err == nil {
		t.Fatalf("RunCommand returned no error, expecting dial timeout error")
	}
}

func createRspFile(t *testing.T, contents []string) string {
	tmpFile, err := os.CreateTemp(os.TempDir(), "")
	if err != nil {
		t.Fatalf("Cannot create temporary file: %v", err)
	}
	text := strings.Join(contents, "\n")
	if _, err = tmpFile.Write([]byte(text)); err != nil {
		t.Fatalf("Failed to write to temporary file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temporary file: %v", err)
	}
	return tmpFile.Name()
}
