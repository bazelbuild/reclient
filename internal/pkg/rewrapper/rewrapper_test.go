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

	tspb "google.golang.org/protobuf/types/known/timestamppb"

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
	allEnv := os.Environ()
	os.Clearenv()
	defer func() {
		for _, v := range allEnv {
			parts := strings.SplitN(v, "=", 2)
			os.Setenv(parts[0], parts[1])
		}
	}()

	var (
		envKey = "KEY"
		envVal = "VALUE"
	)
	t.Setenv(envKey, envVal)

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
				EnvironmentVariables: map[string]string{
					envKey: envVal,
				},
				Inputs:          []string{"baz.h", "foo.h", "bar.h"},
				SymlinkBehavior: cpb.SymlinkBehaviorType_PRESERVE,
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

func TestParseVirtualInputs(t *testing.T) {
	cmd := []string{"cat", "foo.txt"}
	st := time.Now()
	tests := []struct {
		name    string
		opts    *CommandOptions
		want    *ppb.RunRequest
		wantErr bool
	}{
		{
			name: "VirtualInput with path and digest",
			opts: &CommandOptions{
				Labels:            map[string]string{"type": "tool"},
				ExecRoot:          "/home/user/code",
				VirtualInputs:     "path=foo.txt,digest=b5bb9d8014a0f9b1d61e21e796d78dccdf1352f23cd32812f4850b878ae4944c/4",
				ExecStrategy:      "remote",
				Platform:          map[string]string{"docker": "a1b2c3"},
				RemoteAcceptCache: true,
				RemoteUpdateCache: true,
				DownloadOutputs:   true,
				StartTime:         st,
			},
			want: &ppb.RunRequest{
				Command: &cpb.Command{
					Identifiers: &cpb.Identifiers{},
					ExecRoot:    "/home/user/code",
					Input: &cpb.InputSpec{
						VirtualInputs: []*cpb.VirtualInput{
							&cpb.VirtualInput{
								Path:   "foo.txt",
								Digest: "b5bb9d8014a0f9b1d61e21e796d78dccdf1352f23cd32812f4850b878ae4944c/4",
							},
						},
					},
					Output:   &cpb.OutputSpec{},
					Args:     []string{"cat", "foo.txt"},
					Platform: map[string]string{"docker": "a1b2c3"},
				},
				Labels: map[string]string{"type": "tool"},
				ExecutionOptions: &ppb.ProxyExecutionOptions{
					ExecutionStrategy: ppb.ExecutionStrategy_REMOTE,
					RemoteExecutionOptions: &ppb.RemoteExecutionOptions{
						AcceptCached:    true,
						DownloadOutputs: true,
					},
					LocalExecutionOptions: &ppb.LocalExecutionOptions{
						AcceptCached: true,
					},
				},
				Metadata: &ppb.Metadata{
					EventTimes: map[string]*cpb.TimeInterval{WrapperOverheadKey: &cpb.TimeInterval{From: command.TimeToProto(st)}},
				},
			},
		},
		{
			name: "VirtualInput with path,digest,mtime, and filemode",
			opts: &CommandOptions{
				Labels:            map[string]string{"type": "tool"},
				ExecRoot:          "/home/user/code",
				VirtualInputs:     "mtime=1712169729000000000,path=foo.txt,filemode=0777,digest=b5bb9d8014a0f9b1d61e21e796d78dccdf1352f23cd32812f4850b878ae4944c/4",
				ExecStrategy:      "remote",
				Platform:          map[string]string{"docker": "a1b2c3"},
				RemoteAcceptCache: true,
				RemoteUpdateCache: true,
				DownloadOutputs:   true,
				StartTime:         st,
			},
			want: &ppb.RunRequest{
				Command: &cpb.Command{
					Identifiers: &cpb.Identifiers{},
					ExecRoot:    "/home/user/code",
					Input: &cpb.InputSpec{
						VirtualInputs: []*cpb.VirtualInput{
							&cpb.VirtualInput{
								Path:     "foo.txt",
								Digest:   "b5bb9d8014a0f9b1d61e21e796d78dccdf1352f23cd32812f4850b878ae4944c/4",
								Mtime:    tspb.New(time.Unix(0, 1712169729000000000).UTC()),
								Filemode: uint32(0777),
							},
						},
					},
					Output:   &cpb.OutputSpec{},
					Args:     []string{"cat", "foo.txt"},
					Platform: map[string]string{"docker": "a1b2c3"},
				},
				Labels: map[string]string{"type": "tool"},
				ExecutionOptions: &ppb.ProxyExecutionOptions{
					ExecutionStrategy: ppb.ExecutionStrategy_REMOTE,
					RemoteExecutionOptions: &ppb.RemoteExecutionOptions{
						AcceptCached:    true,
						DownloadOutputs: true,
					},
					LocalExecutionOptions: &ppb.LocalExecutionOptions{
						AcceptCached: true,
					},
				},
				Metadata: &ppb.Metadata{
					EventTimes: map[string]*cpb.TimeInterval{WrapperOverheadKey: &cpb.TimeInterval{From: command.TimeToProto(st)}},
				},
			},
		},
		{
			name: "VirtualInput with multiple inputs",
			opts: &CommandOptions{
				Labels:            map[string]string{"type": "tool"},
				ExecRoot:          "/home/user/code",
				VirtualInputs:     "path=foo.txt,digest=b5bb9d8014a0f9b1d61e21e796d78dccdf1352f23cd32812f4850b878ae4944c/4;path=bar.txt,digest=b5bb9d8014a0f9b1d61e21e796d78dccdf1352f23cd32812f4850b878ae4944c/4",
				ExecStrategy:      "remote",
				Platform:          map[string]string{"docker": "a1b2c3"},
				RemoteAcceptCache: true,
				RemoteUpdateCache: true,
				DownloadOutputs:   true,
				StartTime:         st,
			},
			want: &ppb.RunRequest{
				Command: &cpb.Command{
					Identifiers: &cpb.Identifiers{},
					ExecRoot:    "/home/user/code",
					Input: &cpb.InputSpec{
						VirtualInputs: []*cpb.VirtualInput{
							&cpb.VirtualInput{
								Path:   "foo.txt",
								Digest: "b5bb9d8014a0f9b1d61e21e796d78dccdf1352f23cd32812f4850b878ae4944c/4",
							},
							&cpb.VirtualInput{
								Path:   "bar.txt",
								Digest: "b5bb9d8014a0f9b1d61e21e796d78dccdf1352f23cd32812f4850b878ae4944c/4",
							},
						},
					},
					Output:   &cpb.OutputSpec{},
					Args:     []string{"cat", "foo.txt"},
					Platform: map[string]string{"docker": "a1b2c3"},
				},
				Labels: map[string]string{"type": "tool"},
				ExecutionOptions: &ppb.ProxyExecutionOptions{
					ExecutionStrategy: ppb.ExecutionStrategy_REMOTE,
					RemoteExecutionOptions: &ppb.RemoteExecutionOptions{
						AcceptCached:    true,
						DownloadOutputs: true,
					},
					LocalExecutionOptions: &ppb.LocalExecutionOptions{
						AcceptCached: true,
					},
				},
				Metadata: &ppb.Metadata{
					EventTimes: map[string]*cpb.TimeInterval{WrapperOverheadKey: &cpb.TimeInterval{From: command.TimeToProto(st)}},
				},
			},
		},
		{
			name: "VirtualInput with no Path",
			opts: &CommandOptions{
				Labels:            map[string]string{"type": "tool"},
				ExecRoot:          "/home/user/code",
				VirtualInputs:     "digest=b5bb9d8014a0f9b1d61e21e796d78dccdf1352f23cd32812f4850b878ae4944c/4",
				ExecStrategy:      "remote",
				Platform:          map[string]string{"docker": "a1b2c3"},
				RemoteAcceptCache: true,
				RemoteUpdateCache: true,
				DownloadOutputs:   true,
				StartTime:         st,
			},
			wantErr: true,
		},
		{
			name: "VirtualInput with no digest",
			opts: &CommandOptions{
				Labels:            map[string]string{"type": "tool"},
				ExecRoot:          "/home/user/code",
				VirtualInputs:     "path=foo.txt",
				ExecStrategy:      "remote",
				Platform:          map[string]string{"docker": "a1b2c3"},
				RemoteAcceptCache: true,
				RemoteUpdateCache: true,
				DownloadOutputs:   true,
				StartTime:         st,
			},
			wantErr: true,
		},
		{
			name: "VirtualInput with bad mtime",
			opts: &CommandOptions{
				Labels:            map[string]string{"type": "tool"},
				ExecRoot:          "/home/user/code",
				VirtualInputs:     "path=foo.txt,digest=b5bb9d8014a0f9b1d61e21e796d78dccdf1352f23cd32812f4850b878ae4944c/4,mtime=foo",
				ExecStrategy:      "remote",
				Platform:          map[string]string{"docker": "a1b2c3"},
				RemoteAcceptCache: true,
				RemoteUpdateCache: true,
				DownloadOutputs:   true,
				StartTime:         st,
			},
			wantErr: true,
		},
		{
			name: "VirtualInput with bad filemode",
			opts: &CommandOptions{
				Labels:            map[string]string{"type": "tool"},
				ExecRoot:          "/home/user/code",
				VirtualInputs:     "path=foo.txt,digest=b5bb9d8014a0f9b1d61e21e796d78dccdf1352f23cd32812f4850b878ae4944c/4,filemode=foo",
				ExecStrategy:      "remote",
				Platform:          map[string]string{"docker": "a1b2c3"},
				RemoteAcceptCache: true,
				RemoteUpdateCache: true,
				DownloadOutputs:   true,
				StartTime:         st,
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := &proxyStub{}
			_, err := RunCommand(context.Background(), time.Hour, p, cmd, test.opts)
			if err != nil && !test.wantErr {
				t.Errorf("RunCommand(%v,%v) returned error: %v", cmd, test.opts, err)
			}

			if err == nil && test.wantErr {
				t.Errorf("RunCommand(%v,%v) succeeded. want error", cmd, test.opts)
			}

			if !test.wantErr {
				strSliceCmp := protocmp.SortRepeated(func(a, b string) bool { return a < b })
				if diff := cmp.Diff(test.want, p.req, strSliceCmp, protocmp.IgnoreFields(&ppb.Metadata{}, "environment"), protocmp.Transform()); diff != "" {
					t.Errorf("RunCommand(cmd, opts) created bad RunRequest. (-want +got): %s", diff)
				}
			}
		})
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
