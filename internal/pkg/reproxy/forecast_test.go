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

package reproxy

import (
	"context"
	"testing"
	"time"

	lpb "github.com/bazelbuild/reclient/api/log"
	"github.com/bazelbuild/reclient/internal/pkg/logger"

	cpb "github.com/bazelbuild/remote-apis-sdks/go/api/command"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
)

var (
	lbls  = map[string]string{"type": "foo", "compiler": "bar"}
	lbls2 = map[string]string{"type": "baz", "compiler": "bar"}
)

func actionsWithLatencies(t *testing.T, lbls map[string]string, lat []int) []*action {
	t.Helper()
	a := make([]*action, 0)
	ts := time.Now()
	for _, l := range lat {
		ti := command.TimeIntervalToProto(&command.TimeInterval{
			From: ts,
			To:   ts.Add(time.Duration(l) * time.Millisecond),
		})
		a = append(a, &action{
			lbls: lbls,
			rec: &logger.LogRecord{LogRecord: &lpb.LogRecord{
				RemoteMetadata: &lpb.RemoteMetadata{
					EventTimes: map[string]*cpb.TimeInterval{downloadResultMetricKey: ti},
				},
			}},
		})
	}
	return a
}

func TestMedian(t *testing.T) {
	t.Parallel()
	f := &Forecast{minSizeForStats: 1}
	actions := actionsWithLatencies(t, lbls, []int{1, 2, 3, 4, 5})
	for _, a := range actions {
		f.RecordSample(a)
	}
	cCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go f.Run(cCtx)
	time.Sleep(2 * time.Second)

	m, err := f.PercentileDownloadLatency(actions[0], 50)
	if err != nil {
		t.Errorf("PercentileDownloadLatency(50) returned error: %v", err)
	}
	if m != 3*time.Millisecond {
		t.Errorf("PercentileDownloadLatency(50) = %v, want 3ms", m)
	}

	actions = actionsWithLatencies(t, lbls, []int{6, 7, 8})
	for _, a := range actions {
		f.RecordSample(a)
	}
	time.Sleep(2 * time.Second)

	m, err = f.PercentileDownloadLatency(actions[0], 50)
	if err != nil {
		t.Errorf("PercentileDownloadLatency(50) returned error: %v", err)
	}
	if m != 5*time.Millisecond {
		t.Errorf("PercentileDownloadLatency(50) = %v, want 5ms", m)
	}
}

func TestCircular(t *testing.T) {
	t.Parallel()
	f := &Forecast{minSizeForStats: 1}
	latencies := make([]int, 0)
	for i := 0; i < datasetSize; i++ {
		latencies = append(latencies, i)
	}
	actions := actionsWithLatencies(t, lbls, latencies)
	for _, a := range actions {
		f.RecordSample(a)
	}
	cCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go f.Run(cCtx)
	time.Sleep(2 * time.Second)

	m, err := f.PercentileDownloadLatency(actions[0], 50)
	if err != nil {
		t.Errorf("PercentileDownloadLatency(50) returned error: %v", err)
	}
	if m != time.Duration(datasetSize/2)*time.Millisecond {
		t.Errorf("PercentileDownloadLatency(50) = %v, want %vms", m, datasetSize/2)
	}
	latencies = make([]int, 0)
	for i := datasetSize; i < datasetSize*1.5; i++ {
		latencies = append(latencies, i)
	}
	actions = actionsWithLatencies(t, lbls, latencies)
	for _, a := range actions {
		f.RecordSample(a)
	}
	time.Sleep(2 * time.Second)

	m, err = f.PercentileDownloadLatency(actions[0], 50)
	if err != nil {
		t.Errorf("PercentileDownloadLatency(50) returned error: %v", err)
	}
	if m != time.Duration(datasetSize)*time.Millisecond {
		t.Errorf("PercentileDownloadLatency(50) = %v, want %vms", m, datasetSize)
	}
}

func TestMultipleLabels(t *testing.T) {
	t.Parallel()
	f := &Forecast{minSizeForStats: 1}
	actions := actionsWithLatencies(t, lbls, []int{1, 2, 3, 4, 5})
	for _, a := range actions {
		f.RecordSample(a)
	}
	actions2 := actionsWithLatencies(t, lbls2, []int{6, 7, 8, 9, 10})
	for _, a := range actions2 {
		f.RecordSample(a)
	}
	cCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go f.Run(cCtx)
	time.Sleep(2 * time.Second)

	m, err := f.PercentileDownloadLatency(actions[0], 50)
	if err != nil {
		t.Errorf("PercentileDownloadLatency(%v,50) returned error: %v", err)
	}
	if m != 3*time.Millisecond {
		t.Errorf("PercentileDownloadLatency(%v,50) = %v, want 3ms", m)
	}

	m, err = f.PercentileDownloadLatency(actions2[0], 50)
	if err != nil {
		t.Errorf("PercentileDownloadLatency(%v,50) returned error: %v", lbls2, err)
	}
	if m != 8*time.Millisecond {
		t.Errorf("PercentileDownloadLatency(%v,50) = %v, want 8ms", lbls2, m)
	}
}
