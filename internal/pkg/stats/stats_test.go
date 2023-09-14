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

package stats

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"

	lpb "team/foundry-x/re-client/api/log"
	stpb "team/foundry-x/re-client/api/stat"
	spb "team/foundry-x/re-client/api/stats"

	cpb "github.com/bazelbuild/remote-apis-sdks/go/api/command"
)

var (
	minfo                      = machineInfo()
	cmpStatsOpts               = []cmp.Option{protocmp.Transform(), protocmp.SortRepeatedFields(&stpb.Stat{}, "counts_by_value"), protocmp.SortRepeatedFields(&spb.Stats{}, "stats")}
	cmpStatsIgnoreBuildLatency = protocmp.IgnoreFields(&spb.Stats{}, "build_latency") // This is invalid if no proxy execution times provided.
)

// TODO (b/269614799): Test refactor/clean up

func TestStatsToProto(t *testing.T) {
	st := &Stats{
		NumRecords: 5,
		Stats: map[string]*Stat{
			"empty": &Stat{},
			"b": &Stat{
				Count: 3,
				CountByValue: map[string]int64{
					"v1": 4,
					"v2": 5,
				},
			},
			"a": &Stat{
				Count:        6,
				Median:       10,
				Percentile75: 11,
				Percentile85: 12,
				Percentile95: 13,
				Average:      9.8,
				Outlier1:     &stpb.Outlier{CommandId: "foo", Value: 15},
				Outlier2:     &stpb.Outlier{CommandId: "foo", Value: 14},
			},
		},
		ProxyInfos: []*lpb.ProxyInfo{&lpb.ProxyInfo{
			EventTimes: map[string]*cpb.TimeInterval{
				"Event": &cpb.TimeInterval{},
			},
		}},
		cacheHits:         10,
		minProxyExecStart: 23.5,
		maxProxyExecEnd:   50.75,
	}
	expected := &spb.Stats{
		NumRecords: 5,
		Stats: []*stpb.Stat{
			&stpb.Stat{
				Name:         "a",
				Count:        6,
				Median:       10,
				Percentile75: 11,
				Percentile85: 12,
				Percentile95: 13,
				Average:      9.8,
				Outliers: []*stpb.Outlier{
					&stpb.Outlier{CommandId: "foo", Value: 15},
					&stpb.Outlier{CommandId: "foo", Value: 14},
				},
			},
			&stpb.Stat{
				Name:  "b",
				Count: 3,
				CountsByValue: []*stpb.Stat_Value{
					&stpb.Stat_Value{Name: "v1", Count: 4},
					&stpb.Stat_Value{Name: "v2", Count: 5},
				},
			},
		},
		MachineInfo: minfo,
		ProxyInfo: []*lpb.ProxyInfo{&lpb.ProxyInfo{
			EventTimes: map[string]*cpb.TimeInterval{
				"Event": &cpb.TimeInterval{},
			},
		}},
		BuildCacheHitRatio: 2,
		BuildLatency:       27.25,
	}
	if diff := cmp.Diff(expected, st.ToProto(), protocmp.Transform()); diff != "" {
		t.Errorf("ToProto returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestWriteStats(t *testing.T) {
	st := &Stats{
		NumRecords: 5,
		Stats: map[string]*Stat{
			"empty": &Stat{},
			"b": &Stat{
				Count: 3,
				CountByValue: map[string]int64{
					"v1": 4,
					"v2": 5,
				},
			},
			"a": &Stat{
				Count:        6,
				Median:       10,
				Percentile75: 11,
				Percentile85: 12,
				Percentile95: 13,
				Average:      9.8,
				Outlier1:     &stpb.Outlier{CommandId: "foo", Value: 15},
				Outlier2:     &stpb.Outlier{CommandId: "foo", Value: 14},
			},
		},
		ProxyInfos: []*lpb.ProxyInfo{&lpb.ProxyInfo{
			EventTimes: map[string]*cpb.TimeInterval{
				"Event": &cpb.TimeInterval{},
			},
		}},
	}
	outDir := t.TempDir()
	WriteStats(st.ToProto(), outDir)

	got := fileContent(t, filepath.Join(outDir, "rbe_metrics.txt"))

	if len(strings.Fields(got)) <= 2 {
		t.Errorf("WriteStats did not generate multiline output, got: \n%s", got)
	}
}

func fileContent(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to open %v", path)
	}
	return string(b)
}

func TestBoolStats(t *testing.T) {
	recs := []*lpb.LogRecord{
		&lpb.LogRecord{
			RemoteMetadata: &lpb.RemoteMetadata{
				CacheHit: true,
			},
			LocalMetadata: &lpb.LocalMetadata{
				ValidCacheHit:   true,
				ExecutedLocally: false,
				UpdatedCache:    false,
			},
		},
		&lpb.LogRecord{
			RemoteMetadata: &lpb.RemoteMetadata{
				CacheHit: true,
			},
			LocalMetadata: &lpb.LocalMetadata{
				ValidCacheHit:   true,
				ExecutedLocally: false,
				UpdatedCache:    false,
			},
		},
		&lpb.LogRecord{
			RemoteMetadata: &lpb.RemoteMetadata{
				CacheHit: false,
			},
			LocalMetadata: &lpb.LocalMetadata{
				ValidCacheHit:   false,
				ExecutedLocally: true,
				UpdatedCache:    false,
			},
		},
		&lpb.LogRecord{
			RemoteMetadata: &lpb.RemoteMetadata{
				CacheHit: false,
			},
			LocalMetadata: &lpb.LocalMetadata{
				ValidCacheHit:   false,
				ExecutedLocally: true,
				UpdatedCache:    true,
			},
		},
		&lpb.LogRecord{
			RemoteMetadata: &lpb.RemoteMetadata{
				CacheHit: true,
			},
			LocalMetadata: &lpb.LocalMetadata{
				ValidCacheHit:   false,
				ExecutedLocally: true,
				UpdatedCache:    false,
			},
		},
	}
	s := NewFromRecords(recs, nil)
	wantStats := &spb.Stats{
		NumRecords: 5,
		Stats: []*stpb.Stat{
			&stpb.Stat{
				Name: "CompletionStatus",
				CountsByValue: []*stpb.Stat_Value{
					{
						Name:  lpb.CompletionStatus_STATUS_UNKNOWN.String(),
						Count: 5,
					},
				},
			},
			&stpb.Stat{
				Name:  "LocalMetadata.ExecutedLocally",
				Count: 3,
			},
			&stpb.Stat{
				Name:  "LocalMetadata.UpdatedCache",
				Count: 1,
			},
			&stpb.Stat{
				Name:  "LocalMetadata.ValidCacheHit",
				Count: 2,
			},
			&stpb.Stat{
				Name:  "RemoteMetadata.CacheHit",
				Count: 3,
			},
		},
		MachineInfo: minfo,
		ProxyInfo:   []*lpb.ProxyInfo{},
	}
	if diff := cmp.Diff(wantStats, s.ToProto(), append(cmpStatsOpts, cmpStatsIgnoreBuildLatency)...); diff != "" {
		t.Errorf("Boolean stat returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestEnumStats(t *testing.T) {
	recs := []*lpb.LogRecord{
		&lpb.LogRecord{
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_REMOTE_ERROR},
			CompletionStatus: lpb.CompletionStatus_STATUS_REMOTE_FAILURE,
		},
		&lpb.LogRecord{
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_REMOTE_EXECUTION,
		},
		&lpb.LogRecord{
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
			CompletionStatus: lpb.CompletionStatus_STATUS_CACHE_HIT,
		},
		&lpb.LogRecord{
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_REMOTE_ERROR},
			CompletionStatus: lpb.CompletionStatus_STATUS_REMOTE_FAILURE,
		},
		&lpb.LogRecord{
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_LOCAL_EXECUTION,
		},
		&lpb.LogRecord{
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_RACING_LOCAL,
		},
		&lpb.LogRecord{
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_TIMEOUT},
			CompletionStatus: lpb.CompletionStatus_STATUS_TIMEOUT,
		},
		&lpb.LogRecord{
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_SUCCESS},
			CompletionStatus: lpb.CompletionStatus_STATUS_RACING_REMOTE,
		},
		&lpb.LogRecord{
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_CACHE_HIT},
			CompletionStatus: lpb.CompletionStatus_STATUS_CACHE_HIT,
		},
		&lpb.LogRecord{
			Result:           &cpb.CommandResult{Status: cpb.CommandResultStatus_NON_ZERO_EXIT},
			CompletionStatus: lpb.CompletionStatus_STATUS_NON_ZERO_EXIT,
		},
	}
	s := NewFromRecords(recs, nil)
	wantStats := &spb.Stats{
		NumRecords: 10,
		Stats: []*stpb.Stat{
			&stpb.Stat{
				Name: "CompletionStatus",
				CountsByValue: []*stpb.Stat_Value{
					&stpb.Stat_Value{Name: lpb.CompletionStatus_STATUS_CACHE_HIT.String(), Count: 2},
					&stpb.Stat_Value{Name: lpb.CompletionStatus_STATUS_NON_ZERO_EXIT.String(), Count: 1},
					&stpb.Stat_Value{Name: lpb.CompletionStatus_STATUS_REMOTE_FAILURE.String(), Count: 2},
					&stpb.Stat_Value{Name: lpb.CompletionStatus_STATUS_REMOTE_EXECUTION.String(), Count: 1},
					&stpb.Stat_Value{Name: lpb.CompletionStatus_STATUS_LOCAL_EXECUTION.String(), Count: 1},
					&stpb.Stat_Value{Name: lpb.CompletionStatus_STATUS_RACING_LOCAL.String(), Count: 1},
					&stpb.Stat_Value{Name: lpb.CompletionStatus_STATUS_RACING_REMOTE.String(), Count: 1},
					&stpb.Stat_Value{Name: lpb.CompletionStatus_STATUS_TIMEOUT.String(), Count: 1},
				},
			},
			&stpb.Stat{
				Name: "Result.Status",
				CountsByValue: []*stpb.Stat_Value{
					&stpb.Stat_Value{Name: "CACHE_HIT", Count: 2},
					&stpb.Stat_Value{Name: "NON_ZERO_EXIT", Count: 1},
					&stpb.Stat_Value{Name: "REMOTE_ERROR", Count: 2},
					&stpb.Stat_Value{Name: "SUCCESS", Count: 4},
					&stpb.Stat_Value{Name: "TIMEOUT", Count: 1},
				},
			},
		},
		MachineInfo:        minfo,
		ProxyInfo:          []*lpb.ProxyInfo{},
		BuildCacheHitRatio: 0.2,
	}
	if diff := cmp.Diff(wantStats, s.ToProto(), append(cmpStatsOpts, cmpStatsIgnoreBuildLatency)...); diff != "" {
		t.Errorf("Enum stat returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestInvocationIDs(t *testing.T) {
	// Maps in Go have random iteration order, which is a good test for our stats.
	invIDs := []string{"inv1", "inv2"}
	var recs []*lpb.LogRecord
	for i := 0; i < 5; i++ {
		recs = append(recs, &lpb.LogRecord{
			Command: &cpb.Command{Identifiers: &cpb.Identifiers{
				InvocationId: invIDs[i%2],
			}},
		})
	}
	s := NewFromRecords(recs, nil)
	wantStats := &spb.Stats{
		NumRecords:    5,
		InvocationIds: invIDs,
		MachineInfo:   minfo,
		ProxyInfo:     []*lpb.ProxyInfo{},
		Stats: []*stpb.Stat{
			{
				Name: "CompletionStatus",
				CountsByValue: []*stpb.Stat_Value{
					{
						Name:  lpb.CompletionStatus_STATUS_UNKNOWN.String(),
						Count: 5,
					},
				},
			},
		},
	}
	strSliceCmp := cmpopts.SortSlices(func(a, b string) bool { return a < b })
	if diff := cmp.Diff(wantStats, s.ToProto(), protocmp.Transform(), strSliceCmp, cmpStatsIgnoreBuildLatency); diff != "" {
		t.Errorf("Int stat returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestIntStats(t *testing.T) {
	// Maps in Go have random iteration order, which is a good test for our stats.
	m := make(map[string]int)
	for i := 1; i <= 100; i++ {
		m[fmt.Sprint(i)] = i
	}
	var recs []*lpb.LogRecord
	for n, v := range m {
		recs = append(recs, &lpb.LogRecord{
			Command:        &cpb.Command{Identifiers: &cpb.Identifiers{CommandId: n}},
			RemoteMetadata: &lpb.RemoteMetadata{NumInputFiles: int32(v)},
		})
	}
	s := NewFromRecords(recs, nil)
	wantStats := &spb.Stats{
		NumRecords: 100,
		Stats: []*stpb.Stat{
			{
				Name: "CompletionStatus",
				CountsByValue: []*stpb.Stat_Value{
					{
						Name:  lpb.CompletionStatus_STATUS_UNKNOWN.String(),
						Count: 100,
					},
				},
			},
			&stpb.Stat{
				Name:         "RemoteMetadata.NumInputFiles",
				Average:      50.5,
				Count:        5050,
				Median:       51,
				Percentile75: 76,
				Percentile85: 86,
				Percentile95: 96,
				Outliers: []*stpb.Outlier{
					&stpb.Outlier{CommandId: "100", Value: 100},
					&stpb.Outlier{CommandId: "99", Value: 99},
				},
			},
		},
		MachineInfo: minfo,
		ProxyInfo:   []*lpb.ProxyInfo{},
	}
	if diff := cmp.Diff(wantStats, s.ToProto(), protocmp.Transform(), cmpStatsIgnoreBuildLatency); diff != "" {
		t.Errorf("Int stat returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestLabelsAggregation(t *testing.T) {
	var recs []*lpb.LogRecord
	for i := 1; i <= 10; i++ {
		recs = append(recs, &lpb.LogRecord{
			Command: &cpb.Command{Identifiers: &cpb.Identifiers{CommandId: "c_" + fmt.Sprint(i)}},
			LocalMetadata: &lpb.LocalMetadata{
				Labels: map[string]string{
					"lang":   "c++",
					"action": "compile",
				},
			},
			RemoteMetadata: &lpb.RemoteMetadata{NumInputFiles: 2},
		})
	}
	for i := 1; i <= 20; i++ {
		recs = append(recs, &lpb.LogRecord{
			Command: &cpb.Command{Identifiers: &cpb.Identifiers{CommandId: "l_" + fmt.Sprint(i)}},
			LocalMetadata: &lpb.LocalMetadata{
				Labels: map[string]string{
					"lang":   "c++",
					"action": "link",
				},
			},
			RemoteMetadata: &lpb.RemoteMetadata{NumInputFiles: 5},
		})
	}
	s := NewFromRecords(recs, nil)
	wantStats := &spb.Stats{
		NumRecords: 30,
		Stats: []*stpb.Stat{
			&stpb.Stat{
				Name:         "RemoteMetadata.NumInputFiles",
				Average:      4,
				Count:        120,
				Median:       5,
				Percentile75: 5,
				Percentile85: 5,
				Percentile95: 5,
				Outliers: []*stpb.Outlier{
					&stpb.Outlier{CommandId: "l_1", Value: 5},
					&stpb.Outlier{CommandId: "l_2", Value: 5},
				},
			},
			&stpb.Stat{
				Name:         "[action=compile,lang=c++].RemoteMetadata.NumInputFiles",
				Average:      2,
				Count:        20,
				Median:       2,
				Percentile75: 2,
				Percentile85: 2,
				Percentile95: 2,
				Outliers: []*stpb.Outlier{
					&stpb.Outlier{CommandId: "c_1", Value: 2},
					&stpb.Outlier{CommandId: "c_2", Value: 2},
				},
			},
			&stpb.Stat{
				Name:         "[action=link,lang=c++].RemoteMetadata.NumInputFiles",
				Average:      5,
				Count:        100,
				Median:       5,
				Percentile75: 5,
				Percentile85: 5,
				Percentile95: 5,
				Outliers: []*stpb.Outlier{
					&stpb.Outlier{CommandId: "l_1", Value: 5},
					&stpb.Outlier{CommandId: "l_2", Value: 5},
				},
			},
			{
				Name: "CompletionStatus",
				CountsByValue: []*stpb.Stat_Value{
					{
						Name:  lpb.CompletionStatus_STATUS_UNKNOWN.String(),
						Count: 30,
					},
				},
			},
			{
				Name: "[action=compile,lang=c++].CompletionStatus",
				CountsByValue: []*stpb.Stat_Value{
					{
						Name:  lpb.CompletionStatus_STATUS_UNKNOWN.String(),
						Count: 10,
					},
				},
			},
			{
				Name: "[action=link,lang=c++].CompletionStatus",
				CountsByValue: []*stpb.Stat_Value{
					{
						Name:  lpb.CompletionStatus_STATUS_UNKNOWN.String(),
						Count: 20,
					},
				},
			},
		},
		MachineInfo: minfo,
		ProxyInfo:   []*lpb.ProxyInfo{},
	}
	if diff := cmp.Diff(wantStats, s.ToProto(), append(cmpStatsOpts, cmpStatsIgnoreBuildLatency)...); diff != "" {
		t.Errorf("Stat returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestVerification(t *testing.T) {
	recs := []*lpb.LogRecord{
		&lpb.LogRecord{
			LocalMetadata: &lpb.LocalMetadata{
				Verification: &lpb.Verification{
					Mismatches: []*lpb.Verification_Mismatch{
						&lpb.Verification_Mismatch{
							Path:          "foo.o",
							RemoteDigests: []string{"aaa/1"},
							LocalDigest:   "bbb/1",
						},
						&lpb.Verification_Mismatch{
							Path:          "foo.d",
							RemoteDigests: []string{"aaa/2"},
							LocalDigest:   "bbb/2",
						},
					},
					TotalMismatches: 2,
					TotalVerified:   2,
				},
			},
		},
		&lpb.LogRecord{
			LocalMetadata: &lpb.LocalMetadata{
				Verification: &lpb.Verification{
					Mismatches: []*lpb.Verification_Mismatch{
						&lpb.Verification_Mismatch{
							Path:          "bar.o",
							RemoteDigests: []string{"aaa/3"},
							LocalDigest:   "bbb/3",
						},
						&lpb.Verification_Mismatch{
							Path:          "bar.d",
							RemoteDigests: []string{"aaa/4"},
							LocalDigest:   "bbb/4",
						},
					},
					TotalMismatches: 2,
					TotalVerified:   2,
				},
			},
		},
		&lpb.LogRecord{
			LocalMetadata: &lpb.LocalMetadata{
				Verification: &lpb.Verification{
					Mismatches: []*lpb.Verification_Mismatch{
						&lpb.Verification_Mismatch{
							Path:          "bla.txt",
							RemoteDigests: []string{"aaa/5"},
							LocalDigest:   "bbb/5",
						},
					},
					TotalMismatches: 1,
					TotalVerified:   2,
				},
			},
		},
	}
	s := NewFromRecords(recs, nil)
	wantStats := &spb.Stats{
		NumRecords: 3,
		Stats: []*stpb.Stat{
			{
				Name: "CompletionStatus",
				CountsByValue: []*stpb.Stat_Value{
					{
						Name:  "STATUS_UNKNOWN",
						Count: 3,
					},
				},
			},
			&stpb.Stat{
				Name:         "LocalMetadata.Verification.TotalMismatches",
				Count:        5,
				Median:       2,
				Percentile75: 2,
				Percentile85: 2,
				Percentile95: 2,
				Average:      5.0 / 3.0,
				Outliers: []*stpb.Outlier{
					&stpb.Outlier{Value: 2},
					&stpb.Outlier{Value: 2},
				},
			},
			&stpb.Stat{
				Name:         "LocalMetadata.Verification.TotalVerified",
				Count:        6,
				Median:       2,
				Percentile75: 2,
				Percentile85: 2,
				Percentile95: 2,
				Average:      6.0 / 3.0,
				Outliers: []*stpb.Outlier{
					&stpb.Outlier{Value: 2},
					&stpb.Outlier{Value: 2},
				},
			},
		},
		Verification: &lpb.Verification{
			Mismatches: []*lpb.Verification_Mismatch{
				&lpb.Verification_Mismatch{
					Path:          "bar.d",
					RemoteDigests: []string{"aaa/4"},
					LocalDigest:   "bbb/4",
				},
				&lpb.Verification_Mismatch{
					Path:          "bar.o",
					RemoteDigests: []string{"aaa/3"},
					LocalDigest:   "bbb/3",
				},
				&lpb.Verification_Mismatch{
					Path:          "bla.txt",
					RemoteDigests: []string{"aaa/5"},
					LocalDigest:   "bbb/5",
				},
				&lpb.Verification_Mismatch{
					Path:          "foo.d",
					RemoteDigests: []string{"aaa/2"},
					LocalDigest:   "bbb/2",
				},
				&lpb.Verification_Mismatch{
					Path:          "foo.o",
					RemoteDigests: []string{"aaa/1"},
					LocalDigest:   "bbb/1",
				},
			},
			TotalMismatches: 5,
			TotalVerified:   6,
		},
		MachineInfo: minfo,
		ProxyInfo:   []*lpb.ProxyInfo{},
	}
	if diff := cmp.Diff(wantStats, s.ToProto(), append(cmpStatsOpts, cmpStatsIgnoreBuildLatency)...); diff != "" {
		t.Errorf("Int stat returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestTimeStats(t *testing.T) {
	// Maps in Go have random iteration order, which is a good test for our stats.
	m := make(map[string]int)
	for i := 0; i < 100; i++ {
		m[fmt.Sprint(i)] = i
	}
	from, _ := time.Parse(time.RFC3339, "2006-01-02T15:04:05Z")
	fromPb := command.TimeToProto(from)
	var recs []*lpb.LogRecord
	for n, v := range m {
		recs = append(recs, &lpb.LogRecord{
			Command: &cpb.Command{Identifiers: &cpb.Identifiers{CommandId: n}},
			RemoteMetadata: &lpb.RemoteMetadata{
				EventTimes: map[string]*cpb.TimeInterval{
					"Foo": &cpb.TimeInterval{
						From: fromPb,
						To:   command.TimeToProto(from.Add(time.Duration(v) * time.Millisecond)),
					},
				},
			},
		})
	}
	s := NewFromRecords(recs, nil)
	wantStats := &spb.Stats{
		NumRecords: 100,
		Stats: []*stpb.Stat{
			{
				Name: "CompletionStatus",
				CountsByValue: []*stpb.Stat_Value{
					{
						Name:  lpb.CompletionStatus_STATUS_UNKNOWN.String(),
						Count: 100,
					},
				},
			},
			&stpb.Stat{
				Name:         "RemoteMetadata.EventTimes.FooMillis",
				Count:        100,
				Average:      49.5,
				Median:       50,
				Percentile75: 75,
				Percentile85: 85,
				Percentile95: 95,
				Outliers: []*stpb.Outlier{
					&stpb.Outlier{CommandId: "99", Value: 99},
					&stpb.Outlier{CommandId: "98", Value: 98},
				},
			},
		},
		MachineInfo: minfo,
		ProxyInfo:   []*lpb.ProxyInfo{},
	}
	if diff := cmp.Diff(wantStats, s.ToProto(), append(cmpStatsOpts, cmpStatsIgnoreBuildLatency)...); diff != "" {
		t.Errorf("Time stat returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestTimeStatsDoNotAggregatePartial(t *testing.T) {
	from, _ := time.Parse(time.RFC3339, "2006-01-02T15:04:05Z")
	fromPb := command.TimeToProto(from)
	recs := []*lpb.LogRecord{
		&lpb.LogRecord{
			RemoteMetadata: &lpb.RemoteMetadata{
				EventTimes: map[string]*cpb.TimeInterval{"Foo": &cpb.TimeInterval{From: fromPb}},
			},
		},
		&lpb.LogRecord{
			RemoteMetadata: &lpb.RemoteMetadata{
				EventTimes: map[string]*cpb.TimeInterval{"Foo": &cpb.TimeInterval{From: fromPb}},
			},
		},
		&lpb.LogRecord{
			RemoteMetadata: &lpb.RemoteMetadata{
				EventTimes: map[string]*cpb.TimeInterval{
					"Foo": &cpb.TimeInterval{From: fromPb, To: fromPb},
				},
			},
		},
		&lpb.LogRecord{
			RemoteMetadata: &lpb.RemoteMetadata{
				EventTimes: map[string]*cpb.TimeInterval{
					"Foo": &cpb.TimeInterval{
						From: fromPb,
						To:   command.TimeToProto(from.Add(2 * time.Microsecond)),
					},
				},
			},
		},
		&lpb.LogRecord{
			RemoteMetadata: &lpb.RemoteMetadata{
				EventTimes: map[string]*cpb.TimeInterval{
					"Foo": &cpb.TimeInterval{
						From: fromPb,
						To:   command.TimeToProto(from.Add(2 * time.Millisecond)),
					},
				},
			},
		},
	}
	s := NewFromRecords(recs, nil)

	wantStats := &spb.Stats{
		NumRecords: 5,
		Stats: []*stpb.Stat{
			{
				Name: "CompletionStatus",
				CountsByValue: []*stpb.Stat_Value{
					{
						Name:  "STATUS_UNKNOWN",
						Count: 5,
					},
				},
			},
			&stpb.Stat{
				Name:         "RemoteMetadata.EventTimes.FooMillis",
				Count:        3,
				Average:      2.0 / 3.0,
				Median:       0,
				Percentile75: 2,
				Percentile85: 2,
				Percentile95: 2,
				Outliers: []*stpb.Outlier{
					&stpb.Outlier{Value: 2},
				},
			},
		},
		MachineInfo: minfo,
		ProxyInfo:   []*lpb.ProxyInfo{},
	}
	if diff := cmp.Diff(wantStats, s.ToProto(), append(cmpStatsOpts, cmpStatsIgnoreBuildLatency)...); diff != "" {
		t.Errorf("Time stat returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestBandwidthStats(t *testing.T) {
	testCases := []struct {
		name     string
		stat     *Stats
		wantDown string
		wantUp   string
	}{
		{
			name: "Test bytes",
			stat: &Stats{
				Stats: map[string]*Stat{
					"RemoteMetadata.RealBytesDownloaded": &Stat{Count: 999},
					"RemoteMetadata.RealBytesUploaded":   &Stat{Count: 999},
				},
			},
			wantDown: "999 B",
			wantUp:   "999 B",
		},
		{
			name: "Test kilo-bytes",
			stat: &Stats{
				Stats: map[string]*Stat{
					"RemoteMetadata.RealBytesDownloaded": &Stat{Count: 2520},
					"RemoteMetadata.RealBytesUploaded":   &Stat{Count: 2520},
				},
			},
			wantDown: "2.52 KB",
			wantUp:   "2.52 KB",
		},
		{
			name: "Test mega-bytes",
			stat: &Stats{
				Stats: map[string]*Stat{
					"RemoteMetadata.RealBytesDownloaded": &Stat{Count: 1000 * 1100},
					"RemoteMetadata.RealBytesUploaded":   &Stat{Count: 1000 * 1100},
				},
			},
			wantDown: "1.10 MB",
			wantUp:   "1.10 MB",
		},
		{
			name: "Test giga-bytes",
			stat: &Stats{
				Stats: map[string]*Stat{
					"RemoteMetadata.RealBytesDownloaded": &Stat{Count: 1000 * 1100 * 2000},
					"RemoteMetadata.RealBytesUploaded":   &Stat{Count: 1000 * 1100 * 2000},
				},
			},
			wantDown: "2.20 GB",
			wantUp:   "2.20 GB",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotDown, gotUp := BandwidthStats(tc.stat.ToProto())
			if gotDown != tc.wantDown || gotUp != tc.wantUp {
				t.Fatalf("BandwidthStats() returned incorrect response, gotDown=%v, wantDown=%v, gotUp=%v, wantUp=%v", gotDown, tc.wantDown, gotUp, tc.wantUp)
			}
		})
	}
}

func TestStatsProtoSaver(t *testing.T) {
	original := &spb.Stats{
		NumRecords: 5,
		Stats: []*stpb.Stat{
			&stpb.Stat{
				Name:         "a",
				Count:        6,
				Median:       10,
				Percentile75: 11,
				Percentile85: 12,
				Percentile95: 13,
				Average:      9.8,
				Outliers: []*stpb.Outlier{
					&stpb.Outlier{CommandId: "foo", Value: 15},
					&stpb.Outlier{CommandId: "foo", Value: 14},
				},
			},
			&stpb.Stat{
				Name:  "b",
				Count: 3,
				CountsByValue: []*stpb.Stat_Value{
					&stpb.Stat_Value{Name: "v1", Count: 4},
					&stpb.Stat_Value{Name: "v2", Count: 5},
				},
			},
		},
		MachineInfo: minfo,
		ProxyInfo: []*lpb.ProxyInfo{&lpb.ProxyInfo{
			EventTimes: map[string]*cpb.TimeInterval{
				"Event": &cpb.TimeInterval{
					From: timestamppb.New(time.Unix(1676388339, 0)),
				},
			},
		}},
	}
	vs := &ProtoSaver{original}
	got, id, err := vs.Save()
	if err != nil {
		t.Errorf("Save() returned error: %v", err)
	}
	want := map[string]bigquery.Value{
		"machine_info": map[string]interface{}{
			"arch":      minfo.Arch,
			"num_cpu":   strconv.FormatInt(minfo.NumCpu, 10),
			"os_family": minfo.OsFamily,
			"ram_mbs":   strconv.FormatInt(minfo.RamMbs, 10),
		},
		"num_records": string("5"),
		"proxy_info": []interface{}{
			map[string]interface{}{
				"event_times": []interface{}{
					map[string]interface{}{
						"key":   string("Event"),
						"value": map[string]interface{}{"from": string("2023-02-14T15:25:39Z")},
					},
				},
			},
		},
		"stats": []interface{}{
			map[string]interface{}{
				"average": float64(9.8),
				"count":   string("6"),
				"median":  string("10"),
				"name":    string("a"),
				"outliers": []interface{}{
					map[string]interface{}{"command_id": string("foo"), "value": string("15")},
					map[string]interface{}{"command_id": string("foo"), "value": string("14")},
				},
				"percentile75": string("11"),
				"percentile85": string("12"),
				"percentile95": string("13"),
			},
			map[string]interface{}{"count": string("3"), "counts_by_value": []interface{}{
				map[string]interface{}{"count": string("4"), "name": string("v1")},
				map[string]interface{}{"count": string("5"), "name": string("v2")},
			}, "name": string("b")},
		},
	}
	if len(id) == 0 {
		t.Errorf("Save() returned zero length id")
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Save() returned diff in result: (-want +got)\n%s", diff)
	}
}

// BenchmarkProtoSaver measures the runtime of saving a large stats proto as a bq compatible map
func BenchmarkProtoSaver(b *testing.B) {
	testNow := time.Unix(1676388339, 0)
	proxyEvents := map[string]*cpb.TimeInterval{}
	flags := map[string]string{}
	metrics := map[string]*lpb.Metric{}
	statArr := []*stpb.Stat{}
	for i := 0; i < 1000; i++ {
		proxyEvents[fmt.Sprintf("Event%d", i)] = &cpb.TimeInterval{
			From: timestamppb.New(testNow.Add(time.Duration(i) * time.Minute)),
			To:   timestamppb.New(testNow.Add(time.Duration(i+1) * time.Minute)),
		}
		flags[fmt.Sprintf("flag_%d", i)] = fmt.Sprintf("value_%d", i)
		metrics[fmt.Sprintf("flag_%d_bool", i)] = &lpb.Metric{Value: &lpb.Metric_BoolValue{i%2 == 0}}
		metrics[fmt.Sprintf("flag_%d_double", i)] = &lpb.Metric{Value: &lpb.Metric_DoubleValue{float64(i) + 0.5}}
		metrics[fmt.Sprintf("flag_%d_int64", i)] = &lpb.Metric{Value: &lpb.Metric_Int64Value{int64(i)}}
		statArr = append(statArr, &stpb.Stat{
			Name:         "a",
			Count:        int64(6 * i),
			Median:       int64(10 + i),
			Percentile75: int64(11 + i),
			Percentile85: int64(12 + i),
			Percentile95: int64(13 + i),
			Average:      9.8 + float64(i),
			Outliers: []*stpb.Outlier{
				&stpb.Outlier{CommandId: "foo", Value: int64(15 + i)},
				&stpb.Outlier{CommandId: "foo", Value: int64(14 + i)},
			},
		})
	}
	original := &spb.Stats{
		NumRecords:  5,
		Stats:       statArr,
		MachineInfo: minfo,
		ProxyInfo: []*lpb.ProxyInfo{&lpb.ProxyInfo{
			EventTimes: proxyEvents,
			Metrics:    metrics,
			Flags:      flags,
		}},
	}
	for n := 0; n < b.N; n++ {
		(&ProtoSaver{original}).Save()
	}
}

func TestCompletionStats(t *testing.T) {
	tests := []struct {
		name  string
		stats *spb.Stats
		want  string
	}{
		{
			name: "OneStatus",
			stats: &spb.Stats{
				Stats: []*stpb.Stat{
					&stpb.Stat{
						Name: "CompletionStatus",
						CountsByValue: []*stpb.Stat_Value{
							&stpb.Stat_Value{Name: lpb.CompletionStatus_STATUS_REMOTE_EXECUTION.String(), Count: 4},
						},
					},
				},
			},
			want: "4 remote executions",
		},
		{
			name: "MultipleStatuses",
			stats: &spb.Stats{
				Stats: []*stpb.Stat{
					&stpb.Stat{
						Name: "CompletionStatus",
						CountsByValue: []*stpb.Stat_Value{
							&stpb.Stat_Value{Name: lpb.CompletionStatus_STATUS_REMOTE_EXECUTION.String(), Count: 4},
							&stpb.Stat_Value{Name: lpb.CompletionStatus_STATUS_LOCAL_EXECUTION.String(), Count: 3},
							&stpb.Stat_Value{Name: lpb.CompletionStatus_STATUS_LOCAL_FALLBACK.String(), Count: 1},
						},
					},
				},
			},
			want: "4 remote executions, 1 local fallback, 3 local executions",
		},
		{
			name:  "NoStatuses",
			stats: &spb.Stats{},
			want:  "",
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := CompletionStats(tc.stats)

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("CompletionStats(%v) returned diff in result: (-want +got)\n%s", tc.stats, diff)
			}
		})
	}
}
