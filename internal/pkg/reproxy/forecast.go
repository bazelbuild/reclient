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
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/bazelbuild/reclient/internal/pkg/labels"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"

	log "github.com/golang/glog"
)

const (
	datasetSize             = 500
	defaultMinSize          = 50
	downloadResultMetricKey = "DownloadResults"
)

// Forecast is responsible for forecasting specific parameters of an action based on historical
// data.
type Forecast struct {
	downloadLatencies sync.Map
	minSizeForStats   int
}

// Run starts the background computation of parameter forecasts. Should be run in a goroutine.
func (f *Forecast) Run(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	counter := 0
	for {
		select {
		case <-ctx.Done():
			ticker.Stop()
			return
		case <-ticker.C:
			f.downloadLatencies.Range(func(k, v interface{}) bool {
				ds, ok := v.(*dataset)
				if !ok {
					log.Warningf("Unexpected value found in downloadLatencies map")
					return false
				}
				ds.sort()
				p, _ := ds.percentile(downloadPercentileCutoff)
				if counter%60 == 0 {
					log.Infof("p%v download latency for %+v is %v", downloadPercentileCutoff, k, p)
				}
				return true
			})
			counter++
		}
	}
}

// RecordSample stores a sample from an action to be used to forecast future behavior.
func (f *Forecast) RecordSample(a *action) {
	if a.rec == nil || a.rec.RemoteMetadata == nil {
		return
	}
	v, ok := a.rec.RemoteMetadata.EventTimes[downloadResultMetricKey]
	if !ok {
		return
	}
	l := labels.ToKey(a.lbls)
	if f.minSizeForStats == 0 {
		f.minSizeForStats = defaultMinSize
	}
	d, _ := f.downloadLatencies.LoadOrStore(l, &dataset{dataPoints: make([]int, datasetSize), minSizeForStats: f.minSizeForStats})
	ds, ok := d.(*dataset)
	if !ok {
		log.Warningf("Unexpected type found in the downloadLatencies map")
		return
	}
	ti := command.TimeIntervalFromProto(v)
	ds.insert(int(ti.To.Sub(ti.From).Milliseconds()))
}

// PercentileDownloadLatency returns the expected pth percentile download latency of the given
// action.
func (f *Forecast) PercentileDownloadLatency(a *action, p int) (time.Duration, error) {
	l := labels.ToKey(a.lbls)
	d, loaded := f.downloadLatencies.Load(l)
	if !loaded {
		return 0, fmt.Errorf("couldn't find a dataset for labels: %v", a.lbls)
	}
	ds, ok := d.(*dataset)
	if !ok {
		return 0, fmt.Errorf("unexpected type found in the downloadLatencies map")
	}
	dlp, err := ds.percentile(p)
	return time.Duration(dlp) * time.Millisecond, err
}

type dataset struct {
	dataPoints      []int
	dMu             sync.RWMutex
	head            int
	currSize        int
	minSizeForStats int
	sortedData      []int
	sMu             sync.RWMutex
}

func (d *dataset) insert(v int) {
	d.dMu.Lock()
	defer d.dMu.Unlock()
	d.dataPoints[d.head] = v
	d.head = (d.head + 1) % len(d.dataPoints)
	d.currSize = d.currSize + 1
	if d.currSize > len(d.dataPoints) {
		d.currSize = len(d.dataPoints)
	}
}

func (d *dataset) sort() {
	d.dMu.RLock()
	d.sMu.Lock()
	defer d.dMu.RUnlock()
	defer d.sMu.Unlock()
	if d.currSize < d.minSizeForStats {
		return
	}
	d.sortedData = make([]int, d.currSize)
	copy(d.sortedData, d.dataPoints[:d.currSize])
	sort.Ints(d.sortedData)
}

func (d *dataset) percentile(p int) (int, error) {
	if p < 0 || p > 100 {
		return 0, fmt.Errorf("percentile must be between 0 and 100")
	}
	d.sMu.RLock()
	defer d.sMu.RUnlock()
	if len(d.sortedData) == 0 {
		return 0, fmt.Errorf("no computation ran on dataset yet")
	}
	return d.sortedData[len(d.sortedData)*p/100], nil
}
