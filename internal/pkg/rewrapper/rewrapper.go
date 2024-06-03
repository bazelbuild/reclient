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

// Package rewrapper contains the rewrapper logic for converting a command via CLI to a gRPC
// request to the RE Proxy.
package rewrapper

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bazelbuild/reclient/internal/pkg/rsp"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/retry"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	tspb "google.golang.org/protobuf/types/known/timestamppb"

	ppb "github.com/bazelbuild/reclient/api/proxy"
	cpb "github.com/bazelbuild/remote-apis-sdks/go/api/command"
)

const (
	// WrapperOverheadKey is the key for the wrapper overhead metric passed to the proxy.
	WrapperOverheadKey = "WrapperOverhead"
)

var (
	// backoff has unlimited attempts because retry overall time is controlled by dialTimeout.
	backoff     = retry.ExponentialBackoff(1*time.Second, 15*time.Second, retry.UnlimitedAttempts)
	shouldRetry = func(err error) bool {
		s, ok := status.FromError(err)
		if !ok {
			return false
		}
		switch s.Code() {
		case codes.Canceled, codes.Unknown, codes.DeadlineExceeded, codes.Aborted,
			codes.Unavailable, codes.ResourceExhausted:
			return true
		default:
			return false
		}
	}
)

// Proxy is the interface of hte RE Proxy API.
type Proxy interface {
	RunCommand(context.Context, *ppb.RunRequest, ...grpc.CallOption) (*ppb.RunResponse, error)
}

// CommandOptions contains command execution options passed to the rewrapper.
type CommandOptions struct {
	CommandID                    string
	InvocationID                 string
	ToolName                     string
	Labels                       map[string]string
	ExecRoot                     string
	WorkDir                      string
	OutputFiles                  []string
	OutputDirectories            []string
	Inputs                       []string
	VirtualInputs                string
	ToolchainInputs              []string
	InputListPaths               []string
	OutputListPaths              []string
	EnvVarAllowlist              []string
	ExecTimeout                  time.Duration
	ReclientTimeout              time.Duration
	ExecStrategy                 string
	Compare                      bool
	Platform                     map[string]string
	RemoteAcceptCache            bool
	RemoteUpdateCache            bool
	DownloadOutputs              bool
	DownloadRegex                string
	EnableAtomicDownloads        bool
	LogEnvironment               bool
	StartTime                    time.Time
	NumRetriesIfMismatched       int
	NumLocalReruns               int
	NumRemoteReruns              int
	FailOnMismatch               bool
	LocalWrapper                 string
	RemoteWrapper                string
	PreserveSymlink              bool
	CanonicalizeWorkingDir       bool
	ActionLog                    string
	PreserveUnchangedOutputMtime bool
}

// RunCommand runs a command through the RE proxy.
func RunCommand(ctx context.Context, dialTimeout time.Duration, proxy Proxy, cmd []string, opts *CommandOptions) (*ppb.RunResponse, error) {
	req, err := createRequest(cmd, opts)
	if err != nil {
		return nil, err
	}
	var resp *ppb.RunResponse
	st := time.Now()
	err = retry.WithPolicy(ctx, shouldRetry, backoff, func() error {
		if time.Since(st) > dialTimeout {
			return fmt.Errorf("dial_timeout of %v expired before being able to connect to reproxy", dialTimeout)
		}
		resp, err = proxy.RunCommand(ctx, req)
		return err
	})
	return resp, err
}

func createRequest(cmd []string, opts *CommandOptions) (*ppb.RunRequest, error) {
	inputs := opts.Inputs
	for _, p := range opts.InputListPaths {
		more, err := rsp.Parse(p)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, more...)
	}
	outputs := opts.OutputFiles
	for _, p := range opts.OutputListPaths {
		more, err := rsp.Parse(p)
		if err != nil {
			return nil, err
		}
		outputs = append(outputs, more...)
	}
	virtualinputs, err := parseVirtualInputs(opts.VirtualInputs)
	if err != nil {
		return nil, err
	}
	c := &cpb.Command{
		Identifiers: &cpb.Identifiers{
			CommandId:    opts.CommandID,
			InvocationId: opts.InvocationID,
			ToolName:     opts.ToolName,
		},
		ExecRoot: opts.ExecRoot,
		Input: &cpb.InputSpec{
			Inputs:               inputs,
			VirtualInputs:        virtualinputs,
			EnvironmentVariables: envVars(opts.EnvVarAllowlist),
		},
		Output: &cpb.OutputSpec{
			OutputFiles:       outputs,
			OutputDirectories: opts.OutputDirectories,
		},
		Args:             cmd,
		ExecutionTimeout: int32(opts.ExecTimeout.Seconds()),
		WorkingDirectory: opts.WorkDir,
		Platform:         opts.Platform,
	}
	strategy := ppb.ExecutionStrategy_UNSPECIFIED
	if res, ok := ppb.ExecutionStrategy_Value_value[strings.ToUpper(opts.ExecStrategy)]; ok {
		strategy = ppb.ExecutionStrategy_Value(res)
	}
	md := &ppb.Metadata{EventTimes: map[string]*cpb.TimeInterval{
		WrapperOverheadKey: &cpb.TimeInterval{From: command.TimeToProto(opts.StartTime)},
	}}
	md.Environment = os.Environ()
	if opts.PreserveSymlink {
		c.Input.SymlinkBehavior = cpb.SymlinkBehaviorType_PRESERVE
	}
	rDnc := !opts.RemoteUpdateCache
	if rDnc && strategy == ppb.ExecutionStrategy_LOCAL {
		// Do not set DoNotCache in the remote execution options, since in local execution
		// mode, this being set means there will never be a remote cache hit for that
		// action.
		rDnc = false
	}
	return &ppb.RunRequest{
		Command: c,
		Labels:  opts.Labels,
		ExecutionOptions: &ppb.ProxyExecutionOptions{
			ExecutionStrategy:      strategy,
			CompareWithLocal:       opts.Compare,
			NumRetriesIfMismatched: int32(opts.NumRetriesIfMismatched),
			NumLocalReruns:         int32(opts.NumLocalReruns),
			NumRemoteReruns:        int32(opts.NumRemoteReruns),
			ReclientTimeout:        int32(opts.ReclientTimeout.Seconds()),
			EnableAtomicDownloads:  opts.EnableAtomicDownloads,
			DownloadRegex:          opts.DownloadRegex,
			RemoteExecutionOptions: &ppb.RemoteExecutionOptions{
				AcceptCached:                 opts.RemoteAcceptCache,
				DoNotCache:                   rDnc,
				DownloadOutputs:              opts.DownloadOutputs,
				Wrapper:                      opts.RemoteWrapper,
				CanonicalizeWorkingDir:       opts.CanonicalizeWorkingDir,
				PreserveUnchangedOutputMtime: opts.PreserveUnchangedOutputMtime,
			},
			LocalExecutionOptions: &ppb.LocalExecutionOptions{
				AcceptCached: opts.RemoteAcceptCache,
				DoNotCache:   !opts.RemoteUpdateCache,
				Wrapper:      opts.LocalWrapper,
			},
			LogEnvironment:   opts.LogEnvironment,
			IncludeActionLog: opts.ActionLog != "" || (opts.Compare && opts.FailOnMismatch),
		},
		ToolchainInputs: opts.ToolchainInputs,
		Metadata:        md,
	}, nil
}

func envVars(allowlist []string) map[string]string {
	vars := make(map[string]string)
	for _, v := range allowlist {
		vars[v] = os.Getenv(v)
	}
	return vars
}

func parseVirtualInputs(viFlag string) ([]*cpb.VirtualInput, error) {
	if viFlag == "" {
		return nil, nil
	}
	virtualinputs := make([]*cpb.VirtualInput, 0)
	files := strings.Split(viFlag, ";")
	for _, file := range files {
		if len(file) == 0 {
			continue
		}
		fileProperties := strings.Split(file, ",")
		vi := &cpb.VirtualInput{}
		for _, fp := range fileProperties {
			if strings.HasPrefix(fp, "path=") {
				vi.Path = strings.TrimPrefix(fp, "path=")
			} else if strings.HasPrefix(fp, "digest=") {
				vi.Digest = strings.TrimPrefix(fp, "digest=")
			} else if strings.HasPrefix(fp, "mtime=") {
				var nanos int64
				m := strings.TrimPrefix(fp, "mtime=")
				if m != "" {
					mtime, err := strconv.ParseInt(m, 10, 64)
					if err != nil {
						return nil, err
					}
					nanos = mtime
				}
				vi.Mtime = tspb.New(time.Unix(0, nanos).UTC())
			} else if strings.HasPrefix(fp, "filemode=") {
				fm := strings.TrimPrefix(fp, "filemode=")
				if fm == "" {
					continue
				}
				filemode, err := strconv.ParseInt(fm, 8, 32)
				if err != nil {
					return nil, err
				}
				vi.Filemode = uint32(filemode)
			}
		}
		if vi.Path == "" || vi.Digest == "" {
			return nil, fmt.Errorf("invalid format provided for virtual input %v. path and digest are required", file)
		}
		virtualinputs = append(virtualinputs, vi)
	}
	return virtualinputs, nil
}
