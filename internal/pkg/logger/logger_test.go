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

package logger

import (
	"context"
	"flag"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bazelbuild/reclient/internal/pkg/event"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/digest"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	lpb "github.com/bazelbuild/reclient/api/log"
	ppb "github.com/bazelbuild/reclient/api/proxy"
	spb "github.com/bazelbuild/reclient/api/stats"

	cpb "github.com/bazelbuild/remote-apis-sdks/go/api/command"
)

var (
	pInfo = &lpb.ProxyInfo{
		EventTimes: map[string]*cpb.TimeInterval{
			"ProxyUptime": &cpb.TimeInterval{},
		},
	}
)

func TestCommandRemoteMetadataToFromProto(t *testing.T) {
	meta := &command.Metadata{
		InputFiles:       2,
		InputDirectories: 3,
		TotalInputBytes:  4,
		CommandDigest:    digest.NewFromBlob([]byte("foo")),
		ActionDigest:     digest.NewFromBlob([]byte("bar")),
		EventTimes: map[string]*command.TimeInterval{
			"Foo": &command.TimeInterval{From: time.Now(), To: time.Now()},
			"Bar": &command.TimeInterval{From: time.Now(), To: time.Now()},
		},
	}
	gotMeta := CommandRemoteMetadataFromProto(CommandRemoteMetadataToProto(meta))
	if diff := cmp.Diff(meta, gotMeta); diff != "" {
		t.Errorf("CommandRemoteMetadataFromProto(CommandRemoteMetadataToProto()) returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestNew(t *testing.T) {
	execRoot := t.TempDir()
	logger, err := New(TextFormat, execRoot, &stubStats{}, nil, nil, nil)
	if err != nil {
		t.Errorf("Failed to create new logger: %v", err)
	}
	logger.CloseAndAggregate()
}

func TestProxyInfo(t *testing.T) {
	execRoot := t.TempDir()
	logger, err := New(TextFormat, execRoot, &stubStats{}, nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to initialize logger: %v", err)
	}
	st := time.Now().Add(-1 * time.Second)
	et := st.Add(time.Second)
	logger.AddEventTimeToProxyInfo(event.ProxyUptime, st, et)
	logger.AddMetricIntToProxyInfo(event.DepsCacheLoadCount, 1000)
	logger.AddMetricIntToProxyInfo(event.DepsCacheWriteCount, 2000)
	logger.IncrementMetricIntToProxyInfo(event.GomaInputProcessorRestart, 1)
	logger.IncrementMetricIntToProxyInfo(event.GomaInputProcessorRestart, 5)
	logger.IncrementMetricIntToProxyInfo(event.GomaInputProcessorRestart, 3)
	testFlagSet := flag.NewFlagSet("TestFlagSet", flag.ContinueOnError)
	testFlagSet.String("key1", "val1", "test")
	testFlagSet.String("key2", "val2", "test")
	logger.AddFlags(testFlagSet)
	logger.CloseAndAggregate()
	gotRecs, gotPInfo, err := ParseFromLogDirs(TextFormat, []string{execRoot})
	if err != nil {
		t.Errorf("failed to parse from log dir %s: %v", execRoot, err)
	}
	var recs []*lpb.LogRecord
	if diff := cmp.Diff(recs, gotRecs, protocmp.Transform()); diff != "" {
		t.Errorf("Parse logged records returned diff in result: (-want +got)\n%s", diff)
	}
	pInfos := []*lpb.ProxyInfo{
		&lpb.ProxyInfo{
			EventTimes: map[string]*cpb.TimeInterval{
				event.ProxyUptime: command.TimeIntervalToProto(&command.TimeInterval{
					From: st,
					To:   et,
				}),
			},
			Metrics: map[string]*lpb.Metric{
				event.DepsCacheLoadCount: &lpb.Metric{
					Value: &lpb.Metric_Int64Value{1000},
				},
				event.DepsCacheWriteCount: &lpb.Metric{
					Value: &lpb.Metric_Int64Value{2000},
				},
				event.GomaInputProcessorRestart: &lpb.Metric{
					Value: &lpb.Metric_Int64Value{9},
				},
			},
			Flags: map[string]string{
				"key1": "val1",
				"key2": "val2",
			},
		},
	}
	if diff := cmp.Diff(pInfos, gotPInfo, protocmp.Transform()); diff != "" {
		t.Errorf("Parse logged ProxyInfo returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestReducedLogging(t *testing.T) {
	recs := []*lpb.LogRecord{
		&lpb.LogRecord{
			Command: &cpb.Command{
				Identifiers: &cpb.Identifiers{
					CommandId:    "a",
					InvocationId: "b",
					ToolName:     "c",
				},
				Args:     []string{"a", "b", "c"},
				ExecRoot: "/exec/root",
				Input: &cpb.InputSpec{
					Inputs: []string{"foo.h", "bar.h"},
					EnvironmentVariables: map[string]string{
						"k":  "v",
						"k1": "v1",
					},
				},
				Output: &cpb.OutputSpec{
					OutputFiles: []string{"a/b/out"},
				},
			},
			Result: &cpb.CommandResult{
				Status:   cpb.CommandResultStatus_CACHE_HIT,
				ExitCode: 42,
				Msg:      "message",
			},
		},
	}
	execRoot := t.TempDir()
	logger, err := New(ReducedTextFormat, execRoot, &stubStats{}, nil, nil, nil)
	if err != nil {
		t.Fatalf("Failed to initialize logger: %v", err)
	}
	for _, rr := range recs {
		r := logger.LogActionStart()
		r.LogRecord = rr
		logger.Log(r)
	}
	logger.CloseAndAggregate()
	gotRecs, _, err := ParseFromLogDirs(ReducedTextFormat, []string{execRoot})
	if err != nil {
		t.Errorf("Failed to parse from logdir %s: %v", execRoot, err)
	}
	wantRecs := []*lpb.LogRecord{
		&lpb.LogRecord{
			Command: &cpb.Command{
				Identifiers: &cpb.Identifiers{
					CommandId:    "a",
					InvocationId: "b",
					ToolName:     "c",
				},
				ExecRoot: "/exec/root",
				Output: &cpb.OutputSpec{
					OutputFiles: []string{"a/b/out"},
				},
			},
			Result: &cpb.CommandResult{
				Status:   cpb.CommandResultStatus_CACHE_HIT,
				ExitCode: 42,
				Msg:      "message",
			},
		},
	}
	if diff := cmp.Diff(wantRecs, gotRecs, protocmp.Transform()); diff != "" {
		t.Errorf("Parse logged records returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestLogging(t *testing.T) {
	recs := []*lpb.LogRecord{
		&lpb.LogRecord{
			Command: &cpb.Command{
				Identifiers: &cpb.Identifiers{
					CommandId:    "a",
					InvocationId: "b",
					ToolName:     "c",
				},
				Args:     []string{"a", "b", "c"},
				ExecRoot: "/exec/root",
				Input: &cpb.InputSpec{
					Inputs: []string{"foo.h", "bar.h"},
					ExcludeInputs: []*cpb.ExcludeInput{
						&cpb.ExcludeInput{
							Regex: "*.bla",
							Type:  cpb.InputType_DIRECTORY,
						},
						&cpb.ExcludeInput{
							Regex: "*.blo",
							Type:  cpb.InputType_FILE,
						},
					},
					EnvironmentVariables: map[string]string{
						"k":  "v",
						"k1": "v1",
					},
				},
				Output: &cpb.OutputSpec{
					OutputFiles: []string{"a/b/out"},
				},
			},
			Result: &cpb.CommandResult{
				Status:   cpb.CommandResultStatus_CACHE_HIT,
				ExitCode: 42,
				Msg:      "message",
			},
			RemoteMetadata: &lpb.RemoteMetadata{
				Result: &cpb.CommandResult{
					Status:   cpb.CommandResultStatus_CACHE_HIT,
					ExitCode: 42,
					Msg:      "message",
				},
				CacheHit:            true,
				NumInputFiles:       2,
				NumInputDirectories: 3,
				TotalInputBytes:     4,
				CommandDigest:       "abc/10",
				ActionDigest:        "def/2",
			},
			LocalMetadata: &lpb.LocalMetadata{
				ValidCacheHit:   true,
				ExecutedLocally: false,
				UpdatedCache:    true,
			},
		},
		&lpb.LogRecord{
			Command: &cpb.Command{
				Identifiers: &cpb.Identifiers{
					CommandId:    "1",
					InvocationId: "2",
					ToolName:     "3",
				},
				Args:     []string{"foo", "bar"},
				ExecRoot: "/exec/root",
				Input: &cpb.InputSpec{
					Inputs:               []string{"foo.txt"},
					EnvironmentVariables: map[string]string{"a": "1"},
				},
				Output: &cpb.OutputSpec{
					OutputFiles: []string{"a/b/out"},
				},
			},
			Result: &cpb.CommandResult{
				Status:   cpb.CommandResultStatus_REMOTE_ERROR,
				ExitCode: 42,
			},
			RemoteMetadata: &lpb.RemoteMetadata{
				Result: &cpb.CommandResult{
					Status:   cpb.CommandResultStatus_REMOTE_ERROR,
					ExitCode: 42,
				},
				CacheHit:            false,
				NumInputFiles:       2,
				NumInputDirectories: 3,
				TotalInputBytes:     4,
				CommandDigest:       "abc/10",
			},
			LocalMetadata: &lpb.LocalMetadata{
				ValidCacheHit:   false,
				ExecutedLocally: true,
				UpdatedCache:    true,
			},
		},
	}
	execRoot := t.TempDir()
	formatfile := "text://" + filepath.Join(execRoot, "reproxy_log.rpl")
	logger, err := NewFromFormatFile(formatfile, &stubStats{}, nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to initialize logger: %v", err)
	}
	for _, rr := range recs {
		r := logger.LogActionStart()
		r.LogRecord = rr
		logger.Log(r)
	}
	logger.CloseAndAggregate()
	gotRecs, _, err := ParseFromLogDirs(TextFormat, []string{execRoot})
	if err != nil {
		t.Errorf("failed to parse from logfile %s: %v", formatfile, err)
	}
	if diff := cmp.Diff(recs, gotRecs, protocmp.Transform()); diff != "" {
		t.Errorf("Parse logged records returned diff in result: (-want +got)\n%s", diff)
	}
	gotRPLContent := fileContent(t, filepath.Join(execRoot, "reproxy_log.rpl"))

	if len(strings.Fields(gotRPLContent)) <= 15 {
		t.Errorf("reproxy_log.rpl did not have multiline output, got: \n%s", gotRPLContent)
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

func TestParseFileFormat(t *testing.T) {
	tests := []struct {
		name         string
		fullPath     string
		wantFormat   Format
		wantFilepath string
	}{
		{
			name:         "text",
			fullPath:     "text:///foo/bar.rpl",
			wantFormat:   TextFormat,
			wantFilepath: "/foo/bar.rpl",
		}, {
			name:         "reduced text",
			fullPath:     "reducedtext:///foo/bar.rpl",
			wantFormat:   ReducedTextFormat,
			wantFilepath: "/foo/bar.rpl",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotFormat, gotFilepath, err := ParseFilepath(test.fullPath)
			if err != nil {
				t.Errorf("ParseFilepath(%v) returned error: %v", test.fullPath, err)
			}
			if gotFormat != test.wantFormat {
				t.Errorf("ParseFilepath(%v) returned format = %v, want %v", test.fullPath, gotFormat, test.wantFormat)
			}
			if gotFilepath != test.wantFilepath {
				t.Errorf("ParseFilepath(%v) returned filepath = %v, want %v", test.fullPath, gotFilepath, test.wantFilepath)
			}
		})
	}
}

func TestParseFileFormat_Error(t *testing.T) {
	fp := "text:/foo/bar.rpl"
	if _, _, err := ParseFilepath(fp); err == nil {
		t.Errorf("ParseFilepath(%v) returned no error, want error", fp)
	}
}

func TestSingleAction(t *testing.T) {
	execRoot := t.TempDir()
	logger, _ := New(TextFormat, execRoot, &stubStats{}, nil, nil, nil)
	defer logger.CloseAndAggregate()
	rec := logger.LogActionStart()
	summaryDuringExec, _ := logger.GetStatusSummary(context.Background(), &ppb.GetStatusSummaryRequest{})
	rec.CompletionStatus = lpb.CompletionStatus_STATUS_REMOTE_EXECUTION
	logger.Log(rec)
	summaryAfterExec, _ := logger.GetStatusSummary(context.Background(), &ppb.GetStatusSummaryRequest{})

	wantSummaryDuringExec := &ppb.GetStatusSummaryResponse{
		CompletedActionStats: map[string]int32{},
		RunningActions:       1,
	}
	if diff := cmp.Diff(wantSummaryDuringExec, summaryDuringExec, protocmp.Transform()); diff != "" {
		t.Fatalf("GetStatusSummary() during exec had diff in result: (-want +got)\n%s", diff)
	}
	wantSummaryAfterExec := &ppb.GetStatusSummaryResponse{
		CompletedActionStats: map[string]int32{
			lpb.CompletionStatus_STATUS_REMOTE_EXECUTION.String(): 1,
		},
		RunningActions: 0,
	}
	if diff := cmp.Diff(wantSummaryAfterExec, summaryAfterExec, protocmp.Transform()); diff != "" {
		t.Fatalf("GetStatusSummary() after exec had diff in result: (-want +got)\n%s", diff)
	}
}

func TestEndActionTwice(t *testing.T) {
	execRoot := t.TempDir()
	logger, _ := New(TextFormat, execRoot, &stubStats{}, nil, nil, nil)
	defer logger.CloseAndAggregate()
	rec := logger.LogActionStart()
	rec.CompletionStatus = lpb.CompletionStatus_STATUS_REMOTE_EXECUTION
	summaryDuringExec, _ := logger.GetStatusSummary(context.Background(), &ppb.GetStatusSummaryRequest{})
	logger.Log(rec)
	logger.Log(rec)
	summaryAfterExec, _ := logger.GetStatusSummary(context.Background(), &ppb.GetStatusSummaryRequest{})

	wantSummaryDuringExec := &ppb.GetStatusSummaryResponse{
		CompletedActionStats: map[string]int32{},
		RunningActions:       1,
	}
	if diff := cmp.Diff(wantSummaryDuringExec, summaryDuringExec, protocmp.Transform()); diff != "" {
		t.Fatalf("GetStatusSummary() during exec had diff in result: (-want +got)\n%s", diff)
	}
	wantSummaryAfterExec := &ppb.GetStatusSummaryResponse{
		CompletedActionStats: map[string]int32{
			lpb.CompletionStatus_STATUS_REMOTE_EXECUTION.String(): 1,
		},
		RunningActions: 0,
	}
	if diff := cmp.Diff(wantSummaryAfterExec, summaryAfterExec, protocmp.Transform()); diff != "" {
		t.Fatalf("GetStatusSummary() after exec had diff in result: (-want +got)\n%s", diff)
	}
}

func TestConcurrentActions(t *testing.T) {
	execRoot := t.TempDir()
	logger, _ := New(TextFormat, execRoot, &stubStats{}, nil, nil, nil)
	defer logger.CloseAndAggregate()
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			randSleep()
			rec := logger.LogActionStart()
			rec.CompletionStatus = lpb.CompletionStatus_STATUS_REMOTE_EXECUTION
			randSleep()
			logger.Log(rec)
		}()
	}
	wg.Wait()
	summaryAfterExec, _ := logger.GetStatusSummary(context.Background(), &ppb.GetStatusSummaryRequest{})

	wantSummaryAfterExec := &ppb.GetStatusSummaryResponse{
		CompletedActionStats: map[string]int32{
			lpb.CompletionStatus_STATUS_REMOTE_EXECUTION.String(): 1000,
		},
		RunningActions: 0,
	}
	if diff := cmp.Diff(wantSummaryAfterExec, summaryAfterExec, protocmp.Transform()); diff != "" {
		t.Fatalf("GetStatusSummary() after exec had diff in result: (-want +got)\n%s", diff)
	}
}

func randSleep() {
	rand.Seed(time.Now().UnixNano())
	r := rand.Intn(100)
	time.Sleep(time.Duration(r) * time.Millisecond)
}

// Tests if RecordEventTime creates EventTimes if nil, calculates the "now" time, adds the event
// with its time interval to LocalMetadata, and returns the "to" time correctly.
func TestRecordEventTime(t *testing.T) {
	lr := &LogRecord{LogRecord: &lpb.LogRecord{}}
	to := lr.RecordEventTime("foo", time.Now())
	if lr.GetLocalMetadata() == nil {
		t.Fatalf("RecordEventTime did not instantiate LocalMetadata")
	}
	if lr.GetLocalMetadata().GetEventTimes() == nil {
		t.Fatalf("RecordEventTime did not instantiate EventTimes")
	}
	ti, ok := lr.GetLocalMetadata().GetEventTimes()["foo"]
	if !ok {
		t.Errorf("RecordEventTime did not add the key %s to EventTimes", "foo")
	} else if !to.Equal(command.TimeFromProto(ti.To)) {
		t.Errorf("RecordEventTime return time is different from To time in EventTimes for key %s", "foo")
	}
	from := time.Now()
	lr.RecordEventTime("bar", from)
	ti, ok = lr.GetLocalMetadata().GetEventTimes()["bar"]
	if !ok {
		t.Fatalf("RecordEventTime did not add the key %s to the EventTimes", "bar")
	}
	time.Sleep(5 * time.Second)
	lr.RecordEventTime("bar", from)
	tiNew, ok := lr.GetLocalMetadata().GetEventTimes()["bar"]
	if !ok {
		t.Fatalf("RecordEventTime was not able to update the Event Time for %s correctly", "bar")
	}
	newFrom := command.TimeFromProto(tiNew.From)
	to = command.TimeFromProto(ti.To)
	newTo := command.TimeFromProto(tiNew.To)
	if !from.Equal(newFrom) {
		t.Errorf("RecordEventTime did not record the From time correctly for %s. Given: %s, Recorded: %s",
			"bar", from, newFrom)
	}
	if to.Equal(newTo) || newTo.After(newFrom.Add(6*time.Second)) ||
		newTo.Before(newFrom.Add(5*time.Second)) {
		t.Errorf("RecordEventTime did not update the To time correctly for %s. From Time: %s, To Time: %s",
			"bar", newFrom, newTo)
	}
}

// TestStatManagement tests if Logger is handling stats collection and management
// properly. This uses a stub for the Stats struct. The testing for actual stats
// package functions is done in stats_test.go, here we just make sure they are
// called when appropriate.
func TestStatManagement(t *testing.T) {
	recs := []*lpb.LogRecord{
		&lpb.LogRecord{
			Command: &cpb.Command{
				Identifiers: &cpb.Identifiers{
					CommandId:    "a",
					InvocationId: "b",
					ToolName:     "c",
				},
				Args:     []string{"a", "b", "c"},
				ExecRoot: "/exec/root",
				Input: &cpb.InputSpec{
					Inputs: []string{"foo.h", "bar.h"},
					EnvironmentVariables: map[string]string{
						"k":  "v",
						"k1": "v1",
					},
				},
				Output: &cpb.OutputSpec{
					OutputFiles: []string{"a/b/out"},
				},
			},
			Result: &cpb.CommandResult{
				Status:   cpb.CommandResultStatus_CACHE_HIT,
				ExitCode: 42,
				Msg:      "message",
			},
		},
	}
	execRoot := t.TempDir()
	s := &stubStats{
		aggregateFinal: false,
		proto: &spb.Stats{
			NumRecords:  4,
			ToolVersion: "stub",
		},
	}
	logger, err := New(TextFormat, execRoot, s, nil, nil, nil)
	if err != nil {
		t.Errorf("Failed to create new Logger: %v", err)
	}
	for _, lr := range recs {
		r := logger.LogActionStart()
		r.LogRecord = lr
		logger.Log(r)
	}
	sp := logger.CloseAndAggregate()
	if diff := cmp.Diff(recs, s.recs, protocmp.Transform()); diff != "" {
		t.Errorf("Log records added to stats returned diff in result: (-want +got)\n%s", diff)
	}
	if !s.aggregateFinal {
		t.Errorf("Logger closed but aggregate not finalized. Stub stats: %v", s)
	}
	if diff := cmp.Diff(sp, s.proto, protocmp.Transform()); diff != "" {
		t.Errorf("logger.CloseAndAggregate() returned diff in result: (-want +got)\n%s", diff)
	}
}

// TestExportMetric tests if Logger is calling the correct metrics handling
// functions when appropriate. This uses a stub struct for Exporter.
func TestExportMetrics(t *testing.T) {
	recs := []ExportActionMetricsCall{
		{
			Rec: &lpb.LogRecord{
				Command: &cpb.Command{
					Identifiers: &cpb.Identifiers{
						CommandId:    "a",
						InvocationId: "b",
						ToolName:     "c",
					},
					Args:     []string{"a", "b", "c"},
					ExecRoot: "/exec/root",
					Input: &cpb.InputSpec{
						Inputs: []string{"foo.h", "bar.h"},
						EnvironmentVariables: map[string]string{
							"k":  "v",
							"k1": "v1",
						},
					},
					Output: &cpb.OutputSpec{
						OutputFiles: []string{"a/b/out"},
					},
				},
				Result: &cpb.CommandResult{
					Status:   cpb.CommandResultStatus_CACHE_HIT,
					ExitCode: 42,
					Msg:      "message",
				},
			},
		},
	}
	execRoot := t.TempDir()

	// Test nil exporter function.
	logger, err := New(TextFormat, execRoot, &stubStats{}, nil, nil, nil)
	if err != nil {
		t.Errorf("Failed to create new Logger: %v", err)
	}
	for _, lr := range recs {
		r := logger.LogActionStart()
		r.LogRecord = lr.Rec
		logger.Log(r)
	}
	logger.CloseAndAggregate()

	// Test valid exporter.
	e := &stubExporter{}
	logger, err = New(TextFormat, execRoot, &stubStats{}, nil, e, nil)
	logger.remoteDisabled = false
	if err != nil {
		t.Errorf("Failed to create new Logger: %v", err)
	}
	for _, lr := range recs {
		r := logger.LogActionStart()
		r.LogRecord = lr.Rec
		logger.Log(r)
	}
	logger.CloseAndAggregate()
	if diff := cmp.Diff(recs, e.exportedRecs, protocmp.Transform()); diff != "" {
		t.Errorf("Log records sent to exporter returned diff in result: (-want +got)\n%s", diff)
	}

	// Test valid exporter with remoteDisabled=true.
	e = &stubExporter{}
	logger, err = New(TextFormat, execRoot, &stubStats{}, nil, e, nil)
	logger.remoteDisabled = true
	if err != nil {
		t.Errorf("Failed to create new Logger: %v", err)
	}
	var recsRemoteDisabled []ExportActionMetricsCall
	for _, rec := range recs {
		recsRemoteDisabled = append(recsRemoteDisabled, ExportActionMetricsCall{Rec: rec.Rec, RemoteDisabled: true})
	}
	for _, lr := range recsRemoteDisabled {
		r := logger.LogActionStart()
		r.LogRecord = lr.Rec
		logger.Log(r)
	}
	logger.CloseAndAggregate()
	if diff := cmp.Diff(recsRemoteDisabled, e.exportedRecs, protocmp.Transform()); diff != "" {
		t.Errorf("Log records sent to exporter returned diff in result: (-want +got)\n%s", diff)
	}
}

type stubStats struct {
	recs           []*lpb.LogRecord
	aggregateFinal bool
	proto          *spb.Stats
}

func (s *stubStats) AddRecord(rec *lpb.LogRecord) {
	s.recs = append(s.recs, rec)
}

func (s *stubStats) FinalizeAggregate(pInfos []*lpb.ProxyInfo) {
	s.aggregateFinal = true
}

func (s *stubStats) ToProto() *spb.Stats {
	return s.proto
}

type ExportActionMetricsCall struct {
	Rec            *lpb.LogRecord
	RemoteDisabled bool
}

type stubExporter struct {
	exportedRecs []ExportActionMetricsCall
}

func (e *stubExporter) ExportActionMetrics(ctx context.Context, rec *lpb.LogRecord, remoteDisabled bool) {
	e.exportedRecs = append(e.exportedRecs, ExportActionMetricsCall{Rec: rec, RemoteDisabled: remoteDisabled})
}

func (e *stubExporter) ExportBuildMetrics(ctx context.Context, sp *spb.Stats) {}
func (e *stubExporter) Close()                                                {}

func TestLogger_CollectResourceUsageSamples(t *testing.T) {
	tests := []struct {
		name    string
		log     Logger
		samples map[string]int64
		want    map[string][]int64
	}{
		{name: "input is nil", log: Logger{resourceUsage: nil}, samples: nil, want: map[string][]int64{peakNumActions: {0}}},
		{name: "logger with nil resourceUsage field", log: Logger{resourceUsage: nil}, samples: map[string]int64{"cpu": 1, "mem": 2}, want: map[string][]int64{peakNumActions: {0}, "cpu": {1}, "mem": {2}}},
		{name: "logger with non-nil resourceUsage field", log: Logger{resourceUsage: map[string][]int64{"virt": {3}}}, samples: map[string]int64{"cpu": 1, "mem": 2}, want: map[string][]int64{peakNumActions: {0}, "cpu": {1}, "mem": {2}, "virt": {3}}},
		{name: "logger with same resourceUsage field", log: Logger{resourceUsage: map[string][]int64{"cpu": {3}, "mem": {4}}}, samples: map[string]int64{"cpu": 1, "mem": 2}, want: map[string][]int64{peakNumActions: {0}, "cpu": {3, 1}, "mem": {4, 2}}},
		{name: "logger with running actions and resourceUsage field", log: Logger{resourceUsage: map[string][]int64{"cpu": {3}, "mem": {4}}, runningActions: 100}, samples: map[string]int64{"cpu": 1, "mem": 2}, want: map[string][]int64{peakNumActions: {0}, "cpu": {3, 1}, "mem": {4, 2}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.log.collectResourceUsageSamples(tt.samples)
			if diff := cmp.Diff(tt.want, tt.log.resourceUsage); diff != "" {
				t.Errorf("collectResourceUsageSamples() generate diff : (-want +got)\n%s", diff)
			}
		})
	}
}
