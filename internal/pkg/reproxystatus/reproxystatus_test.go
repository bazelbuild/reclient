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
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/testing/protocmp"

	lpb "team/foundry-x/re-client/api/log"
	ppb "team/foundry-x/re-client/api/proxy"
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
				},
			},
			want: `Reproxy(unix://reproxy.sock) is OK
Actions completed: 13 (2 cache hits, 5 remote executions, 6 local executions)
Actions in progress: 11
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

type fakeStatusClient struct {
	resp *ppb.GetStatusSummaryResponse
	err  error
}

func (f fakeStatusClient) GetStatusSummary(context.Context, *ppb.GetStatusSummaryRequest, ...grpc.CallOption) (*ppb.GetStatusSummaryResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func TestFetchAllStatusSummaries(t *testing.T) {
	clients := map[string]ppb.StatusClient{
		"unix://reproxy.sock": fakeStatusClient{
			resp: &ppb.GetStatusSummaryResponse{
				CompletedActionStats: map[string]int32{
					lpb.CompletionStatus_STATUS_REMOTE_EXECUTION.String(): 5,
				},
				RunningActions: 7,
			},
		},
		"unix://reproxy2.sock": fakeStatusClient{
			err: errors.New("boom"),
		},
		"unix://reproxy3.sock": fakeStatusClient{
			resp: &ppb.GetStatusSummaryResponse{
				CompletedActionStats: map[string]int32{
					lpb.CompletionStatus_STATUS_REMOTE_EXECUTION.String(): 9,
				},
				RunningActions: 1,
			},
		},
	}

	ctx := context.Background()
	gotSummaries := FetchAllStatusSummaries(ctx, clients)

	wantSummaries := []*Summary{
		{
			Addr: "unix://reproxy.sock",
			Resp: &ppb.GetStatusSummaryResponse{
				CompletedActionStats: map[string]int32{
					lpb.CompletionStatus_STATUS_REMOTE_EXECUTION.String(): 5,
				},
				RunningActions: 7,
			},
		},
		{
			Addr: "unix://reproxy2.sock",
			Err:  errors.New("boom"),
		},
		{
			Addr: "unix://reproxy3.sock",
			Resp: &ppb.GetStatusSummaryResponse{
				CompletedActionStats: map[string]int32{
					lpb.CompletionStatus_STATUS_REMOTE_EXECUTION.String(): 9,
				},
				RunningActions: 1,
			},
		},
	}

	if diff := cmp.Diff(wantSummaries, gotSummaries, protocmp.Transform(), equateErrorString); diff != "" {
		t.Fatalf("FetchAllStatusSummaries() diff in result: (-want +got)\n%s", diff)
	}
}
