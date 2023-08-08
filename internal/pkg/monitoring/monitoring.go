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

// Package monitoring is responsible for uploading metrics to a monitoring service that supports OpenCensus.
package monitoring

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	cpb "github.com/bazelbuild/remote-apis-sdks/go/api/command"
	lpb "team/foundry-x/re-client/api/log"
	spb "team/foundry-x/re-client/api/stats"
	"team/foundry-x/re-client/internal/pkg/auth"
	"team/foundry-x/re-client/internal/pkg/labels"
	"team/foundry-x/re-client/internal/pkg/logger"
	"team/foundry-x/re-client/pkg/version"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	log "github.com/golang/glog"
	"github.com/google/uuid"

	"contrib.go.opencensus.io/exporter/stackdriver"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	defaultZone = "us-central1-a"

	osFamilyKey       = tag.MustNewKey("os_family")
	versionKey        = tag.MustNewKey("reclient_version")
	labelsKey         = tag.MustNewKey("action_labels")
	statusKey         = tag.MustNewKey("status")
	remoteStatusKey   = tag.MustNewKey("remote_status")
	exitCodeKey       = tag.MustNewKey("exit_code")
	remoteExitCodeKey = tag.MustNewKey("remote_exit_code")
	remoteDisabledKey = tag.MustNewKey("remote_disabled")

	mu         = sync.Mutex{}
	staticKeys = make(map[tag.Key]string, 0)

	failureFiles = []string{"reproxy.FATAL", "bootstrap.FATAL", "rewrapper.FATAL", "reproxy.exe.FATAL", "bootstrap.exe.FATAL", "rewrapper.exe.FATAL"}

	// ActionCount is a metric for tracking the number of actions in reproxy.
	ActionCount = stats.Int64("rbe/action/count", "Number of actions processed by reproxy", stats.UnitDimensionless)
	// ActionLatency is a metric for tracking the e2e latency of an action in reproxy.
	ActionLatency = stats.Float64("rbe/action/latency", "Time spent processing an action e2e in reproxy", stats.UnitMilliseconds)
	// BuildCacheHitRatio is a metric of the ratio of cache hits in a build.
	BuildCacheHitRatio = stats.Float64("rbe/build/cache_hit_ratio", "Ratio of cache hits in a build", stats.UnitDimensionless)
	// BuildLatency is a metric for tracking the e2e latency of a build in reproxy.
	BuildLatency = stats.Float64("rbe/build/latency", "Time spent between reproxy receiving the first and last actions of the build", stats.UnitSeconds)
	// BuildCount is a metric for tracking the number of builds.
	BuildCount = stats.Int64("rbe/build/count", "Counter for builds", stats.UnitDimensionless)
	// BootstrapStartupLatency is a metric for tracing the critical path time of starting reproxy with bootstrap.
	BootstrapStartupLatency = stats.Int64("rbe/build/bootstrap_startup_latency", "Time spent starting reproxy with bootstrap", stats.UnitMilliseconds)
	// BootstrapShutdownLatency is a metric for tracing the critical path time of shutting down reproxy with bootstrap.
	BootstrapShutdownLatency = stats.Int64("rbe/build/bootstrap_shutdown_latency", "Time spent shutting down reproxy with bootstrap", stats.UnitMilliseconds)
)

type recorder interface {
	initialize(o stackdriver.Options) error
	close()
	tagsContext(ctx context.Context, labels map[tag.Key]string) context.Context
	recordWithTags(ctx context.Context, labels map[tag.Key]string, val stats.Measurement)
}

// Exporter is a type used to construct a monitored resource.
type Exporter struct {
	// project is the name of the project to export metrics to.
	project string
	// prefix is the prefix of the exported metrics.
	prefix string
	// MetricNamespace is the namespace of the exported metrics.
	namespace string
	// remoteDisabled indicates if this build ran without rbe.
	remoteDisabled bool
	// logDir is the directory where reclient log files are stored.
	logDir string
	// recorder is responsible for recording metrics.
	recorder recorder
	// authCredentials is a token to use for authenticating to the monitoring service.
	authCredentials *auth.Credentials
}

// NewExporter returns a new Cloud monitoring metrics exporter.
func NewExporter(ctx context.Context, project, prefix, namespace string, remoteDisabled bool, logDir string, creds *auth.Credentials) (*Exporter, error) {
	e := &Exporter{
		project:         project,
		prefix:          prefix,
		namespace:       namespace,
		logDir:          logDir,
		recorder:        &stackDriverRecorder{},
		authCredentials: creds,
		remoteDisabled:  remoteDisabled,
	}
	if err := e.initCloudMonitoring(ctx); err != nil {
		return nil, err
	}
	return e, nil
}

// MonitoredResource returns resource type and resource labels for the build.
func (e *Exporter) MonitoredResource() (resType string, labels map[string]string) {
	hn, err := os.Hostname()
	if err != nil || hn == "" {
		log.Warningf("Failed to get hostname: %v", err)
		hn = "unknown-" + uuid.New().String()
	}
	return "generic_node", map[string]string{
		"project_id": e.project,
		"namespace":  e.namespace,
		"location":   defaultZone,
		"node_id":    hn,
	}
}

// SetupViews sets up monitoring views. This can only be run once.
func SetupViews(labels map[string]string) error {
	if len(staticKeys) != 0 {
		return errors.New("views were already setup, cannot overwrite")
	}
	mu.Lock()
	defer mu.Unlock()
	keys := make([]tag.Key, 0)
	for lbl, val := range labels {
		k, err := tag.NewKey(lbl)
		if err != nil {
			return err
		}
		staticKeys[k] = val
		keys = append(keys, k)
	}
	views := []*view.View{
		{
			Measure:     ActionLatency,
			TagKeys:     append(keys, labelsKey, osFamilyKey, versionKey, statusKey, remoteStatusKey, exitCodeKey, remoteExitCodeKey, remoteDisabledKey),
			Aggregation: view.Distribution(1, 2, 3, 4, 5, 6, 8, 10, 13, 16, 20, 25, 30, 40, 50, 65, 80, 100, 130, 160, 200, 250, 300, 400, 500, 650, 800, 1000, 2000, 5000, 10000, 20000, 50000, 100000, 200000, 500000),
		},
		{
			Measure:     ActionCount,
			TagKeys:     append(keys, labelsKey, osFamilyKey, versionKey, statusKey, remoteStatusKey, exitCodeKey, remoteExitCodeKey, remoteDisabledKey),
			Aggregation: view.Sum(),
		},
		{
			Measure:     BuildCacheHitRatio,
			TagKeys:     append(keys, osFamilyKey, versionKey, remoteDisabledKey),
			Aggregation: view.Distribution(0.05, 0.1, 0.15, 0.20, 0.25, 0.3, 0.35, 0.4, 0.45, 0.5, 0.55, 0.6, 0.65, 0.7, 0.75, 0.8, 0.85, 0.9, 0.95, 1),
		},
		{
			Measure:     BuildLatency,
			TagKeys:     append(keys, osFamilyKey, versionKey, remoteDisabledKey),
			Aggregation: view.Distribution(1, 10, 60, 120, 300, 600, 1200, 2400, 3000, 3600, 4200, 4800, 5400, 6000, 6600, 7200, 9000, 10800, 12600, 14400),
		},
		{
			Measure:     BuildCount,
			TagKeys:     append(keys, osFamilyKey, versionKey, statusKey, remoteDisabledKey),
			Aggregation: view.Sum(),
		},
		{
			Measure:     BootstrapStartupLatency,
			TagKeys:     append(keys, osFamilyKey, versionKey, statusKey, remoteDisabledKey),
			Aggregation: view.Distribution(1, 10, 60, 120, 300, 600, 1200, 2400, 3000, 3600, 4200, 4800, 5400, 6000, 6600, 7200, 9000, 10800, 12600, 14400),
		},
		{
			Measure:     BootstrapShutdownLatency,
			TagKeys:     append(keys, osFamilyKey, versionKey, statusKey, remoteDisabledKey),
			Aggregation: view.Distribution(1, 10, 60, 120, 300, 600, 1200, 2400, 3000, 3600, 4200, 4800, 5400, 6000, 6600, 7200, 9000, 10800, 12600, 14400),
		},
	}
	return view.Register(views...)
}

// initCloudMonitoring creates a new metrics exporter and starts it.
func (e *Exporter) initCloudMonitoring(ctx context.Context) error {
	opts := stackdriver.Options{
		ProjectID: e.project,
		OnError: func(err error) {
			switch status.Code(err) {
			case codes.Unavailable:
				log.Warningf("Failed to export to Stackdriver: %v", err)
			default:
				log.Errorf("Failed to export to Stackdriver: %v %v", status.Code(err), err)
			}
		},
		MetricPrefix:            e.prefix,
		ReportingInterval:       time.Minute,
		MonitoredResource:       e,
		DefaultMonitoringLabels: &stackdriver.Labels{},
	}
	if ts := e.authCredentials.TokenSource(); ts != nil {
		clientOpt := option.WithTokenSource(ts)
		opts.MonitoringClientOptions = []option.ClientOption{clientOpt}
		opts.TraceClientOptions = []option.ClientOption{clientOpt}
	}
	err := e.recorder.initialize(opts)
	return err
}

// ExportActionMetrics exports metrics for one log record to opencensus.
func (e *Exporter) ExportActionMetrics(ctx context.Context, r *lpb.LogRecord) {
	aCtx := e.recorder.tagsContext(ctx, staticKeys)
	aCtx = e.recorder.tagsContext(aCtx, map[tag.Key]string{
		osFamilyKey: runtime.GOOS,
		versionKey:  version.CurrentVersion(),
	})
	times := r.GetLocalMetadata().GetEventTimes()
	var latency float64
	if tPb, ok := times[logger.EventProxyExecution]; ok {
		ti := command.TimeIntervalFromProto(tPb)
		if !ti.From.IsZero() && !ti.To.IsZero() {
			latency = float64(ti.To.Sub(ti.From).Milliseconds())
		}
	}
	e.recorder.recordWithTags(aCtx, e.makeActionTags(r), ActionCount.M(1))
	e.recorder.recordWithTags(aCtx, e.makeActionTags(r), ActionLatency.M(latency))
}

func (e *Exporter) makeActionTags(r *lpb.LogRecord) map[tag.Key]string {
	return map[tag.Key]string{
		labelsKey:         labels.ToKey(r.GetLocalMetadata().GetLabels()),
		statusKey:         r.GetResult().GetStatus().String(),
		remoteDisabledKey: strconv.FormatBool(e.remoteDisabled),
		remoteStatusKey:   r.GetRemoteMetadata().GetResult().GetStatus().String(),
		exitCodeKey:       strconv.FormatInt(int64(r.GetResult().GetExitCode()), 10),
		remoteExitCodeKey: strconv.FormatInt(int64(r.GetRemoteMetadata().GetResult().GetExitCode()), 10),
	}
}

// ExportBuildMetrics exports overall build metrics to opencensus.
func (e *Exporter) ExportBuildMetrics(ctx context.Context, sp *spb.Stats, shutdownTimeInterval *cpb.TimeInterval) {
	numRecs := sp.NumRecords
	aCtx := e.recorder.tagsContext(ctx, staticKeys)
	aCtx = e.recorder.tagsContext(aCtx, map[tag.Key]string{
		osFamilyKey:       runtime.GOOS,
		versionKey:        version.CurrentVersion(),
		remoteDisabledKey: strconv.FormatBool(e.remoteDisabled),
	})
	if numRecs == 0 {
		return
	}
	e.recorder.recordWithTags(aCtx, nil, BuildCacheHitRatio.M(sp.BuildCacheHitRatio))
	e.recorder.recordWithTags(aCtx, nil, BuildLatency.M(sp.BuildLatency))
	status := "SUCCESS"
	if e.checkBuildFailure(aCtx) {
		status = "FAILURE"
	}
	e.recorder.recordWithTags(aCtx, map[tag.Key]string{statusKey: status}, BuildCount.M(1))
	for _, pi := range sp.ProxyInfo {
		if pi.EventTimes == nil {
			continue
		}
		if ti, ok := pi.EventTimes[logger.EventBootstrapStartup]; ok {
			millis := command.TimeFromProto(ti.To).Sub(command.TimeFromProto(ti.From)).Milliseconds()
			e.recorder.recordWithTags(aCtx, nil, BootstrapStartupLatency.M(millis))
		}
	}
	millis := command.TimeFromProto(shutdownTimeInterval.To).Sub(command.TimeFromProto(shutdownTimeInterval.From)).Milliseconds()
	e.recorder.recordWithTags(aCtx, nil, BootstrapShutdownLatency.M(millis))
}

// Close stops the metrics exporter and waits for the exported data to be uploaded.
func (e *Exporter) Close() {
	e.recorder.close()
}

func (e *Exporter) checkBuildFailure(ctx context.Context) bool {
	for _, f := range failureFiles {
		fp := filepath.Join(e.logDir, f)
		s, err := os.Stat(fp)
		if err != nil {
			continue
		}
		if s.Size() == 0 {
			continue
		}
		return true
	}
	return false
}

// stackDriverRecorder is a recorder for stack driver metrics.
type stackDriverRecorder struct {
	e *stackdriver.Exporter
}

func (s *stackDriverRecorder) initialize(o stackdriver.Options) error {
	var err error
	s.e, err = stackdriver.NewExporter(o)
	if err != nil {
		return err
	}
	return s.e.StartMetricsExporter()
}

func (s *stackDriverRecorder) close() {
	s.e.StopMetricsExporter()
	s.e.Flush()
}

func (s *stackDriverRecorder) tagsContext(ctx context.Context, labels map[tag.Key]string) context.Context {
	m := []tag.Mutator{}
	kvs := ""
	for k, v := range labels {
		m = append(m, tag.Insert(k, v))
		kvs += fmt.Sprintf("%v=%v,", k.Name(), v)
	}
	aCtx, err := tag.New(ctx, m...)
	if err != nil {
		log.Warningf("Failed to set labels %v: %v", kvs, err)
	}
	return aCtx
}

func (s *stackDriverRecorder) recordWithTags(ctx context.Context, labels map[tag.Key]string, val stats.Measurement) {
	aCtx := s.tagsContext(ctx, labels)
	stats.Record(aCtx, val)
}

// CleanLogDir removes stray log files which may cause confusion when bootstrap starts
func CleanLogDir(logDir string) {
	for _, f := range failureFiles {
		fp := filepath.Join(logDir, f)
		if err := os.Remove(fp); err != nil && !os.IsNotExist(err) {
			log.Errorf("Failed to remove %v: %v", fp, err)
		}
	}
}
