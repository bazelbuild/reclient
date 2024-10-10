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

// Package reproxystatus provides a human-readable formatted status for running reproxy instances
package reproxystatus

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/fatih/color"

	lpb "github.com/bazelbuild/reclient/api/log"
	ppb "github.com/bazelbuild/reclient/api/proxy"
	"github.com/bazelbuild/reclient/internal/pkg/ipc"
)

// Summary describes the result of calling Status.GetStatusSummary on a reproxy instance at Addr.
type Summary struct {
	Addr string
	Resp *ppb.GetStatusSummaryResponse
	Err  error
}

// HumanReadable formats Summary into a human-readable format.
func (s *Summary) HumanReadable() string {
	name := "Reproxy"
	if s.Addr != "" {
		name = fmt.Sprintf("Reproxy(%s)", s.Addr)
	}
	if s.Err != nil {
		return fmt.Sprintf(
			`%s was not reachable
%s
`,
			name,
			color.RedString("%v", s.Err))
	}
	return fmt.Sprintf(
		`%s %s
Actions completed: %s
Actions in progress: %d
QPS: %d
`,
		name, strings.Join(s.overallStatuses(), ", "),
		s.completedActions(),
		s.Resp.GetRunningActions(),
		s.Resp.GetQps(),
	)
}

func (s *Summary) overallStatuses() []string {
	var statuses []string
	if s.Resp.GetCompletedActionStats()[lpb.CompletionStatus_STATUS_REMOTE_FAILURE.String()] > 0 ||
		s.Resp.GetCompletedActionStats()[lpb.CompletionStatus_STATUS_LOCAL_FAILURE.String()] > 0 {
		statuses = append(statuses, color.MagentaString("had errors"))
	}
	if s.Resp.GetCompletedActionStats()[lpb.CompletionStatus_STATUS_NON_ZERO_EXIT.String()] > 0 {
		statuses = append(statuses, color.RedString("had failed actions"))
	}
	if s.Resp.GetCompletedActionStats()[lpb.CompletionStatus_STATUS_LOCAL_FALLBACK.String()] > 0 {
		statuses = append(statuses, color.YellowString("had local fallbacks"))
	}
	if len(statuses) == 0 {
		statuses = append(statuses, color.GreenString("is OK"))
	}
	return statuses
}

func (s *Summary) completedActions() string {
	var totalCompleted int32
	for _, cnt := range s.Resp.GetCompletedActionStats() {
		totalCompleted += cnt
	}
	if totalCompleted == 0 {
		return "0"
	}

	return fmt.Sprintf("%d (%s)", totalCompleted, CompletedActionsSummary(s.Resp.GetCompletedActionStats()))
}

// CompletedActionsSummary returns a human readable summary of completed actions
// grouped by their completion status.
func CompletedActionsSummary(stats map[string]int32) string {
	var totalsByStatus []string
	for _, status := range sortByOrdinal(stats) {
		cnt := stats[status]
		if cnt > 0 {
			statusStr := strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(status, "STATUS_"), "_", " "))
			if cnt > 1 {
				statusStr += "s"
			}
			totalsByStatus = append(totalsByStatus, fmt.Sprintf("%d %s", cnt, statusStr))
		}
	}
	return strings.Join(totalsByStatus, ", ")
}

// ReproxyTracker manages connections to reproxy instances
type ReproxyTracker interface {
	FetchAllStatusSummaries(ctx context.Context) []*Summary
}

// SingleReproxyTracker manages a connection to a single reproxy instance listening on ServerAddress
type SingleReproxyTracker struct {
	ServerAddress string
	client        ppb.StatusClient
	mu            sync.RWMutex
}

// FetchAllStatusSummaries a Summary the reproxy instance at ServerAddress.
func (rt *SingleReproxyTracker) FetchAllStatusSummaries(ctx context.Context) []*Summary {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	if rt.client == nil {
		conn, err := ipc.DialContext(ctx, rt.ServerAddress)
		if err != nil {
			return []*Summary{
				{
					Addr: rt.ServerAddress,
					Err:  fmt.Errorf("failed to dial reproxy: %w", err),
				},
			}
		}
		rt.client = ppb.NewStatusClient(conn)
	}
	resp, err := rt.client.GetStatusSummary(ctx, &ppb.GetStatusSummaryRequest{})
	return []*Summary{
		{
			Addr: rt.ServerAddress,
			Err:  err,
			Resp: resp,
		},
	}
}

// SocketReproxyTracker manages connections to all reproxy instances listening on unix sockets or windows pipes.
type SocketReproxyTracker struct {
	clients    map[string]ppb.StatusClient
	dialErrors map[string]error
	mu         sync.RWMutex
}

var (
	// Exists for unit test so that the logic can be tested without actual reproxy processes
	testOnlyReproxySocketsKey []string
)

// FetchAllStatusSummaries returns a list of Summary for each client in statusClients sorted by key.
func (rt *SocketReproxyTracker) FetchAllStatusSummaries(ctx context.Context) []*Summary {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	rt.updateClients(ctx)
	var wg sync.WaitGroup
	out := make(chan *Summary, len(rt.clients))
	for addr, statusClient := range rt.clients {
		wg.Add(1)
		go func(addr string, statusClient ppb.StatusClient) {
			defer wg.Done()
			resp, err := statusClient.GetStatusSummary(ctx, &ppb.GetStatusSummaryRequest{})
			out <- &Summary{
				Addr: addr,
				Err:  err,
				Resp: resp,
			}

		}(addr, statusClient)
	}
	go func() {
		wg.Wait()
		close(out)
	}()

	summaries := make([]*Summary, 0, len(rt.clients)+len(rt.dialErrors))
	for summary := range out {
		summaries = append(summaries, summary)
	}
	for addr, err := range rt.dialErrors {
		summaries = append(summaries, &Summary{
			Addr: addr,
			Err:  err,
		})
	}

	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Addr < summaries[j].Addr })

	return summaries
}

// updateClients discovers and connects to all reproxy instances listening on unix sockets or windows pipes.
// These connections will be cached for future calls.
func (rt *SocketReproxyTracker) updateClients(ctx context.Context) {
	if rt.clients == nil {
		rt.clients = map[string]ppb.StatusClient{}
	}
	var addrs []string
	if testOnlyReproxySocketsKey != nil {
		addrs = testOnlyReproxySocketsKey
	} else {
		var err error
		addrs, err = ipc.GetAllReproxySockets(ctx)
		if err != nil {
			rt.dialErrors[""] = fmt.Errorf("error finding reproxy sockets: %w", err)
			return
		}
	}
	rt.dialErrors = map[string]error{}
	oldClients := rt.clients
	rt.clients = make(map[string]ppb.StatusClient, len(addrs))
	for _, addr := range addrs {
		if _, ok := oldClients[addr]; ok {
			rt.clients[addr] = oldClients[addr]
		} else if conn, err := ipc.DialContext(ctx, addr); err == nil {
			rt.clients[addr] = ppb.NewStatusClient(conn)
		} else {
			rt.dialErrors[addr] = fmt.Errorf("failed to dial reproxy: %w", err)
		}
	}
}

// PrintSummaries calls updates the tracker then fetches and prints all summaries from the connected reproxy instances.
func PrintSummaries(ctx context.Context, writer io.Writer, tracker ReproxyTracker) {
	if sums := tracker.FetchAllStatusSummaries(ctx); len(sums) > 0 {
		for _, s := range sums {
			fmt.Fprintf(writer, "%s\n", s.HumanReadable())
		}
	} else {
		fmt.Fprintf(writer, "%s\n", color.RedString("Reproxy is not running"))
	}
}

func sortByOrdinal(stats map[string]int32) []string {
	keys := make([]string, 0, len(stats))
	for k := range stats {
		keys = append(keys, k)
	}
	maxOrd := int32(len(lpb.CompletionStatus_value))
	sort.Slice(keys, func(i, j int) bool {
		ordi, ok := lpb.CompletionStatus_value[keys[i]]
		if !ok {
			ordi = maxOrd
		}
		ordj, ok := lpb.CompletionStatus_value[keys[j]]
		if !ok {
			ordj = maxOrd
		}
		return ordi < ordj
	})
	return keys
}
