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

package reproxystatus

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"google.golang.org/grpc"

	lpb "github.com/bazelbuild/reclient/api/log"
	ppb "github.com/bazelbuild/reclient/api/proxy"
	"github.com/bazelbuild/reclient/internal/pkg/ipc"
)

var (
	equateErrorString = cmp.Comparer(func(a, b error) bool {
		if a == nil || b == nil {
			return a == nil && b == nil
		}
		return a.Error() == b.Error()
	})
)

func TestHumanReadable(t *testing.T) {
	type test struct {
		name  string
		input *Summary
		want  string
	}

	tests := []test{
		{
			name: "SingleInstanceOK",
			input: &Summary{
				Addr: "unix://reproxy.sock",
				Resp: &ppb.GetStatusSummaryResponse{
					CompletedActionStats: map[string]int32{
						lpb.CompletionStatus_STATUS_REMOTE_EXECUTION.String(): 5,
						lpb.CompletionStatus_STATUS_CACHE_HIT.String():        2,
						lpb.CompletionStatus_STATUS_LOCAL_EXECUTION.String():  6,
					},
					RunningActions: 11,
					Qps:            1,
				},
			},
			want: `Reproxy(unix://reproxy.sock) is OK
Actions completed: 13 (2 cache hits, 5 remote executions, 6 local executions)
Actions in progress: 11
QPS: 1
`,
		},
		{
			name: "SingleInstanceFallback",
			input: &Summary{
				Addr: "unix://reproxy.sock",
				Resp: &ppb.GetStatusSummaryResponse{
					CompletedActionStats: map[string]int32{
						lpb.CompletionStatus_STATUS_REMOTE_EXECUTION.String(): 5,
						lpb.CompletionStatus_STATUS_CACHE_HIT.String():        1,
						lpb.CompletionStatus_STATUS_LOCAL_FALLBACK.String():   6,
					},
					RunningActions: 11,
				},
			},
			want: `Reproxy(unix://reproxy.sock) had local fallbacks
Actions completed: 12 (1 cache hit, 5 remote executions, 6 local fallbacks)
Actions in progress: 11
QPS: 0
`,
		},
		{
			name: "SingleInstanceFailedActions",
			input: &Summary{
				Addr: "unix://reproxy.sock",
				Resp: &ppb.GetStatusSummaryResponse{
					CompletedActionStats: map[string]int32{
						lpb.CompletionStatus_STATUS_REMOTE_EXECUTION.String(): 5,
						lpb.CompletionStatus_STATUS_CACHE_HIT.String():        2,
						lpb.CompletionStatus_STATUS_NON_ZERO_EXIT.String():    6,
					},
					RunningActions: 11,
				},
			},
			want: `Reproxy(unix://reproxy.sock) had failed actions
Actions completed: 13 (2 cache hits, 5 remote executions, 6 non zero exits)
Actions in progress: 11
QPS: 0
`,
		},
		{
			name: "SingleInstanceErrors",
			input: &Summary{
				Addr: "unix://reproxy.sock",
				Resp: &ppb.GetStatusSummaryResponse{
					CompletedActionStats: map[string]int32{
						lpb.CompletionStatus_STATUS_REMOTE_EXECUTION.String(): 5,
						lpb.CompletionStatus_STATUS_CACHE_HIT.String():        2,
						lpb.CompletionStatus_STATUS_LOCAL_FAILURE.String():    6,
					},
					RunningActions: 11,
				},
			},
			want: `Reproxy(unix://reproxy.sock) had errors
Actions completed: 13 (2 cache hits, 5 remote executions, 6 local failures)
Actions in progress: 11
QPS: 0
`,
		},
		{
			name: "SingleInstanceFetchError",
			input: &Summary{
				Addr: "unix://reproxy.sock",
				Err:  errors.New("sample error occurred"),
			},
			want: `Reproxy(unix://reproxy.sock) was not reachable
sample error occurred
`,
		},
		{
			name: "SingleInstanceMultipleStatus",
			input: &Summary{
				Addr: "unix://reproxy.sock",
				Resp: &ppb.GetStatusSummaryResponse{
					CompletedActionStats: map[string]int32{
						lpb.CompletionStatus_STATUS_REMOTE_EXECUTION.String(): 5,
						lpb.CompletionStatus_STATUS_CACHE_HIT.String():        2,
						lpb.CompletionStatus_STATUS_LOCAL_FAILURE.String():    2,
						lpb.CompletionStatus_STATUS_NON_ZERO_EXIT.String():    4,
					},
					RunningActions: 11,
				},
			},
			want: `Reproxy(unix://reproxy.sock) had errors, had failed actions
Actions completed: 13 (2 cache hits, 5 remote executions, 2 local failures, 4 non zero exits)
Actions in progress: 11
QPS: 0
`,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.input.HumanReadable()

			if diff := cmp.Diff(tc.want, got, cmp.Transformer("Quote", func(s string) string { return strconv.Quote(s) })); diff != "" {
				t.Fatalf("HumanReadable() had diff in result: (-want +got)\n%s", diff)
			}
		})
	}
}

type fakeStatusServer struct {
	resp *ppb.GetStatusSummaryResponse
	err  error
}

func (f *fakeStatusServer) GetStatusSummary(ctx context.Context, _ *ppb.GetStatusSummaryRequest) (*ppb.GetStatusSummaryResponse, error) {
	return f.resp, f.err
}

// GenRandomUDSAddress generates a random socket file address on which reproxy
// can be started.
func genRandomUDSAddress() string {
	switch runtime.GOOS {
	case "windows":
		return fmt.Sprintf("pipe://re-client-%s.ipc", uuid.New().String())
	default:
		return fmt.Sprintf("unix:///tmp/%s.sock", uuid.New().String())
	}
}

func TestPrintSummaries_SingleReproxyTracker(t *testing.T) {
	serverAddress := genRandomUDSAddress()
	statusServer := &fakeStatusServer{
		resp: &ppb.GetStatusSummaryResponse{
			CompletedActionStats: map[string]int32{
				lpb.CompletionStatus_STATUS_REMOTE_EXECUTION.String(): 5,
			},
			RunningActions: 7,
		},
	}
	startStatusServer(t, statusServer, serverAddress)

	tracker := &SingleReproxyTracker{
		ServerAddress: serverAddress,
	}

	ctx := context.Background()
	var sb strings.Builder
	PrintSummaries(ctx, &sb, tracker)
	wantSummaries := fmt.Sprintf(`Reproxy(%s) is OK
Actions completed: 5 (5 remote executions)
Actions in progress: 7
QPS: 0

`, serverAddress)
	if diff := cmp.Diff(wantSummaries, sb.String()); diff != "" {
		t.Errorf("PrintSummaries() generated wrong output \n (-want +got): %v", diff)
	}
}

func TestPrintSummaries_SingleReproxyTracker_NotRunning(t *testing.T) {
	serverAddress := genRandomUDSAddress()

	tracker := &SingleReproxyTracker{
		ServerAddress: serverAddress,
	}

	ctx := context.Background()
	var sb strings.Builder
	PrintSummaries(ctx, &sb, tracker)
	var dialError string
	switch runtime.GOOS {
	case "windows":
		dialError = fmt.Sprintf(`open \\\\.\\pipe\\%s: The system cannot find the file specified.`, strings.TrimPrefix(serverAddress, "pipe://"))
	default:
		dialError = fmt.Sprintf("dial unix %s: connect: no such file or directory", strings.TrimPrefix(serverAddress, "unix://"))
	}
	wantSummaries := fmt.Sprintf(`Reproxy(%s) was not reachable
rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: %s"

`, serverAddress, dialError)
	if diff := cmp.Diff(wantSummaries, sb.String()); diff != "" {
		t.Errorf("PrintSummaries() generated wrong output \n (-want +got): %v", diff)
	}
}

func startStatusServer(t *testing.T, statusServer ppb.StatusServer, address string) {
	t.Helper()
	listener, err := ipc.Listen(address)
	t.Cleanup(func() { listener.Close() })
	if err != nil {
		t.Fatalf("Unable to start status server: %v", err)
	}
	grpcServer := grpc.NewServer()
	ppb.RegisterStatusServer(grpcServer, statusServer)
	t.Cleanup(grpcServer.Stop)
	go grpcServer.Serve(listener)
}

func TestPrintSummaries_SocketReproxyTracker(t *testing.T) {
	addrs := []string{
		genRandomUDSAddress(),
		genRandomUDSAddress(),
		genRandomUDSAddress(),
		genRandomUDSAddress(),
	}
	sort.Strings(addrs)
	setReproxySockets(t, addrs)
	startStatusServer(t,
		&fakeStatusServer{
			resp: &ppb.GetStatusSummaryResponse{
				CompletedActionStats: map[string]int32{
					lpb.CompletionStatus_STATUS_REMOTE_EXECUTION.String(): 5,
				},
				RunningActions: 7,
			},
		},
		addrs[0])
	startStatusServer(t,
		&fakeStatusServer{
			err: errors.New("boom"),
		},
		addrs[1])
	startStatusServer(t,
		&fakeStatusServer{
			resp: &ppb.GetStatusSummaryResponse{
				CompletedActionStats: map[string]int32{
					lpb.CompletionStatus_STATUS_LOCAL_FALLBACK.String():   2,
					lpb.CompletionStatus_STATUS_REMOTE_EXECUTION.String(): 9,
				},
				RunningActions: 1,
			},
		},
		addrs[2])
	tracker := &SocketReproxyTracker{}

	var sb strings.Builder
	PrintSummaries(context.Background(), &sb, tracker)
	var dialError string
	switch runtime.GOOS {
	case "windows":
		dialError = fmt.Sprintf(`open \\\\.\\pipe\\%s: The system cannot find the file specified.`, strings.TrimPrefix(addrs[3], "pipe://"))
	default:
		dialError = fmt.Sprintf("dial unix %s: connect: no such file or directory", strings.TrimPrefix(addrs[3], "unix://"))
	}
	wantSummaries := fmt.Sprintf(`Reproxy(%s) is OK
Actions completed: 5 (5 remote executions)
Actions in progress: 7
QPS: 0

Reproxy(%s) was not reachable
rpc error: code = Unknown desc = boom

Reproxy(%s) had local fallbacks
Actions completed: 11 (9 remote executions, 2 local fallbacks)
Actions in progress: 1
QPS: 0

Reproxy(%s) was not reachable
rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: %s"

`, addrs[0], addrs[1], addrs[2], addrs[3], dialError)
	if diff := cmp.Diff(wantSummaries, sb.String()); diff != "" {
		t.Errorf("PrintSummaries() generated wrong output \n (-want +got): %v", diff)
	}
}

func TestPrintSummaries_SocketReproxyTracker_NoneRunning(t *testing.T) {
	setReproxySockets(t, []string{})
	tracker := &SocketReproxyTracker{}

	var sb strings.Builder
	PrintSummaries(context.Background(), &sb, tracker)
	wantSummaries := "Reproxy is not running\n"
	if diff := cmp.Diff(wantSummaries, sb.String()); diff != "" {
		t.Errorf("PrintSummaries() generated wrong output \n (-want +got): %v", diff)
	}
}

// Mutex to prevent multiple tests from using testOnlyReproxySocketsKey at once
var reproxySocketMu sync.Mutex

func setReproxySockets(t *testing.T, addrs []string) {
	t.Helper()
	reproxySocketMu.Lock()
	t.Cleanup(func() {
		testOnlyReproxySocketsKey = nil
		reproxySocketMu.Unlock()
	})
	testOnlyReproxySocketsKey = addrs
}
