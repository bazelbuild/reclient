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
	"sort"
	"strings"
	"sync"

	"github.com/fatih/color"

	lpb "team/foundry-x/re-client/api/log"
	ppb "team/foundry-x/re-client/api/proxy"
)

// Summary describes the result of calling Status.GetStatusSummary on a reproxy instance at Addr.
type Summary struct {
	Addr string
	Resp *ppb.GetStatusSummaryResponse
	Err  error
}

// HumanReadable formats Summary into a human-readable format.
func (s *Summary) HumanReadable() string {
	if s.Err != nil {
		return fmt.Sprintf(
			`Reproxy(%s) was not reachable
%s
`,
			s.Addr,
			color.RedString("%v", s.Err))
	}
	return fmt.Sprintf(
		`Reproxy(%s) %s
Actions completed: %s
Actions in progress: %d
`,
		s.Addr, strings.Join(s.overallStatuses(), ", "),
		s.completedActions(),
		s.Resp.GetRunningActions(),
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

// FetchAllStatusSummaries returns a list of Summary for each client in statusClients sorted by key.
func FetchAllStatusSummaries(ctx context.Context, statusClients map[string]ppb.StatusClient) []*Summary {
	var wg sync.WaitGroup
	out := make(chan *Summary, len(statusClients))
	for addr, statusClient := range statusClients {
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

	summaries := make([]*Summary, 0, len(statusClients))
	for summary := range out {
		summaries = append(summaries, summary)
	}

	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Addr < summaries[j].Addr })

	return summaries
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
