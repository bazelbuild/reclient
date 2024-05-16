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

// Package logger provides functionality for logging command execution records.
package logger

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	lpb "github.com/bazelbuild/reclient/api/log"
	ppb "github.com/bazelbuild/reclient/api/proxy"
	stpb "github.com/bazelbuild/reclient/api/stat"
	spb "github.com/bazelbuild/reclient/api/stats"
	"github.com/bazelbuild/reclient/internal/pkg/auxiliary"
	"github.com/bazelbuild/reclient/internal/pkg/bigquery"
	"github.com/bazelbuild/reclient/internal/pkg/bigquerytranslator"
	"github.com/bazelbuild/reclient/internal/pkg/ignoremismatch"
	"github.com/bazelbuild/reclient/internal/pkg/localresources/usage"
	"github.com/bazelbuild/reclient/internal/pkg/monitoring"
	"github.com/bazelbuild/reclient/internal/pkg/protoencoding"
	"github.com/bazelbuild/reclient/internal/pkg/stats"
	cpb "github.com/bazelbuild/remote-apis-sdks/go/api/command"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/digest"
	log "github.com/golang/glog"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

// Format specifies how the Logger serializes its records.
type Format int

const (
	// TextFormat means records marshalled as proto-ASCII.
	TextFormat Format = iota

	// JSONFormat means records marshalled as JSON.
	JSONFormat

	// BinaryFormat means records marshalled as binary delimited by record size.
	BinaryFormat

	// ReducedTextFormat means records are marshalled as proto-ASCII without the
	// command inputs and args.
	ReducedTextFormat
)

const (
	textDelimiter  string = "\n\n\n"
	peakNumActions string = "PEAK_NUM_ACTIONS"
	unixTime       string = "UNIX_TIME"
)

// Logger logs Records asynchronously into a file.
type Logger struct {
	Format         Format
	ch             chan logEvent
	wg             sync.WaitGroup
	recsFile       *os.File
	infoFile       *os.File
	info           *lpb.ProxyInfo
	remoteDisabled bool
	stats          stats.StatCollector
	mi             *ignoremismatch.MismatchIgnorer
	e              monitoring.StatExporter

	runningActions     int32
	peakRunningActions int32
	completedActions   map[lpb.CompletionStatus]int32

	mu               sync.RWMutex
	open             bool
	resourceUsage    map[string][]int64
	u                *usage.PsutilSampler
	cancelSamplerCtx context.CancelFunc

	// qps indicates the rate of completed actions.
	// The formula is: number of completed actions / total duration in which the number of running actions was greater than zero.
	qps             int32
	qpsStartTime    time.Time
	qpsLastDuration time.Duration
	qpsCount        int32

	// fields used for upload data to bigquery
	items  chan *bigquerytranslator.Item
	bqSpec *bigquery.BQSpec
}

type logEvent interface {
	apply(l *Logger)
}

type startActionEvent struct {
	lr *LogRecord
}

func (s *startActionEvent) apply(l *Logger) {
	if s.lr.open {
		return
	}
	s.lr.open = true
	if l.runningActions == 0 {
		// Reset qps start time to exclude the period in which there were no running actions.
		l.qpsStartTime = time.Now()
	}
	l.runningActions++
	atomic.StoreInt32(&l.peakRunningActions, max(l.runningActions, l.peakRunningActions))
}

func max(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

type endActionEvent struct {
	lr             *LogRecord
	remoteDisabled bool
}

func (e *endActionEvent) apply(l *Logger) {
	if !e.lr.open {
		return
	}
	if l.e != nil {
		l.e.ExportActionMetrics(context.Background(), e.lr.LogRecord, l.remoteDisabled)
	}
	// Process any mismatches to be ignored for this log record.
	l.mi.ProcessLogRecord(e.lr.LogRecord)
	l.stats.AddRecord(e.lr.LogRecord)
	if l.bqSpec != nil {
		l.items <- &bigquerytranslator.Item{LogRecord: toBigQueryRecords(e.lr.LogRecord)}
	}
	e.lr.open = false
	l.completedActions[e.lr.CompletionStatus]++
	l.runningActions--

	l.qpsCount++
	totalDuration := time.Since(l.qpsStartTime) + l.qpsLastDuration
	if seconds := int32(totalDuration.Seconds()); seconds > 0 {
		l.qps = l.qpsCount / seconds
	}
	if l.runningActions == 0 {
		l.qpsLastDuration = totalDuration
		// Reset start time here just in case it doesn't get set properly in a start event.
		l.qpsStartTime = time.Now()
	}

	blob, err := toBytes(l.Format, e.lr.LogRecord)
	if err != nil {
		log.Errorf("Error serializing %v: %v", e.lr.LogRecord, err)
		return
	}
	if _, err := l.recsFile.Write(blob); err != nil {
		log.Errorf("Write error: %v", err)
	}
}

type summarizeActionsEvent struct {
	out chan<- *ppb.GetStatusSummaryResponse
}

func (s *summarizeActionsEvent) apply(l *Logger) {
	completedActions := make(map[string]int32)
	for status, cnt := range l.completedActions {
		completedActions[status.String()] = cnt
	}
	s.out <- &ppb.GetStatusSummaryResponse{
		CompletedActionStats: completedActions,
		RunningActions:       l.runningActions,
		Qps:                  l.qps,
	}
}

// LogRecord wraps proxy.LogRecord while tracking if the command has been ended yet for logging purposes.
type LogRecord struct {
	*lpb.LogRecord

	mu   sync.RWMutex
	open bool
}

// NewLogRecord creates a new LogRecord without logging the start of an action.
// Use Logger.LogActionStart to log the start of an action.
func NewLogRecord() *LogRecord {
	return &LogRecord{
		LogRecord: &lpb.LogRecord{},
		open:      false,
	}
}

// AddCompletionStatus adds the correct CommandResultStatus that summarizes the given LogRecord and ExecutionStrategy.
func AddCompletionStatus(rec *LogRecord, execStrategy ppb.ExecutionStrategy_Value) {
	rec.CompletionStatus = getCompletionStatus(rec, execStrategy)
}

func getCompletionStatus(rec *LogRecord, execStrategy ppb.ExecutionStrategy_Value) lpb.CompletionStatus {
	switch rec.Result.Status {
	case cpb.CommandResultStatus_NON_ZERO_EXIT:
		return lpb.CompletionStatus_STATUS_NON_ZERO_EXIT
	case cpb.CommandResultStatus_CACHE_HIT:
		return lpb.CompletionStatus_STATUS_CACHE_HIT
	case cpb.CommandResultStatus_TIMEOUT:
		return lpb.CompletionStatus_STATUS_TIMEOUT
	case cpb.CommandResultStatus_INTERRUPTED:
		return lpb.CompletionStatus_STATUS_INTERRUPTED
	case cpb.CommandResultStatus_REMOTE_ERROR:
		return lpb.CompletionStatus_STATUS_REMOTE_FAILURE
	case cpb.CommandResultStatus_LOCAL_ERROR:
		return lpb.CompletionStatus_STATUS_LOCAL_FAILURE
	case cpb.CommandResultStatus_SUCCESS:
		switch execStrategy {
		case ppb.ExecutionStrategy_LOCAL:
			return lpb.CompletionStatus_STATUS_LOCAL_EXECUTION
		case ppb.ExecutionStrategy_REMOTE:
			// This means that remote exec is disabled.
			if rec.GetLocalMetadata().GetExecutedLocally() {
				return lpb.CompletionStatus_STATUS_LOCAL_EXECUTION
			}
			return lpb.CompletionStatus_STATUS_REMOTE_EXECUTION
		case ppb.ExecutionStrategy_REMOTE_LOCAL_FALLBACK:
			if rec.GetLocalMetadata().GetExecutedLocally() {
				return lpb.CompletionStatus_STATUS_LOCAL_FALLBACK
			}
			return lpb.CompletionStatus_STATUS_REMOTE_EXECUTION
		case ppb.ExecutionStrategy_RACING:
			if rec.GetLocalMetadata().GetExecutedLocally() {
				return lpb.CompletionStatus_STATUS_RACING_LOCAL
			}
			return lpb.CompletionStatus_STATUS_RACING_REMOTE
		}
	}
	return lpb.CompletionStatus_STATUS_UNKNOWN
}

func (f Format) String() string {
	switch f {
	case TextFormat:
		return "text"
	case JSONFormat:
		return "json"
	case BinaryFormat:
		return "binary"
	case ReducedTextFormat:
		return "reducedtext"
	default:
		return "unknown"
	}
}

// ParseFormat parses a string log file format into the enum.
func ParseFormat(fs string) (Format, error) {
	switch fs {
	case "text":
		return TextFormat, nil
	case "json":
		return JSONFormat, nil
	case "binary":
		return BinaryFormat, nil
	case "reducedtext":
		return ReducedTextFormat, nil
	default:
		return Format(-1), fmt.Errorf("unknown format: %v", fs)
	}
}

// ParseFilepath parses the given formatfile path and returns the format and filename
// of the given log file.
func ParseFilepath(formatfile string) (Format, string, error) {
	i := strings.Index(formatfile, "://")
	if i < 0 {
		return Format(-1), "", fmt.Errorf("unable to parse file format from %v", formatfile)
	}
	format, err := ParseFormat(formatfile[:i])
	if err != nil {
		return Format(-1), "", err
	}
	return format, formatfile[i+3:], nil
}

// NewFromFormatFile instantiates a new Logger.
// TODO(b/279057640): this is deprecated, remove and use New instead when --log_path flag is gone.
func NewFromFormatFile(formatfile string, s stats.StatCollector, mi *ignoremismatch.MismatchIgnorer, e monitoring.StatExporter, u *usage.PsutilSampler, bqSpec *bigquery.BQSpec) (*Logger, error) {
	format, filepath, err := ParseFilepath(formatfile)
	if err != nil {
		return nil, err
	}
	if format != TextFormat && format != ReducedTextFormat {
		return nil, fmt.Errorf("only text:// or reducedtext:// formats are currently supported, received %v", formatfile)
	}
	f, err := os.Create(filepath)
	if err != nil {
		return nil, err
	}
	log.Infof("Created log file %s", filepath)
	return newLogger(format, f, nil, s, mi, e, u, bqSpec), nil
}

func logFileSuffix(format Format) string {
	switch format {
	case TextFormat:
		return "rpl"
	case ReducedTextFormat:
		return "rrpl"
	case JSONFormat:
		return "rpljs"
	case BinaryFormat:
		return "rplpb"
	default:
		return ""
	}
}

func newLogger(format Format, recs, info *os.File, s stats.StatCollector, mi *ignoremismatch.MismatchIgnorer, e monitoring.StatExporter, u *usage.PsutilSampler, bqSpec *bigquery.BQSpec) *Logger {
	l := &Logger{
		Format:   format,
		ch:       make(chan logEvent),
		recsFile: recs,
		infoFile: info,
		info: &lpb.ProxyInfo{
			EventTimes: make(map[string]*cpb.TimeInterval),
			Metrics:    make(map[string]*lpb.Metric),
			Flags:      make(map[string]string),
		},
		stats:            s,
		mi:               mi,
		e:                e,
		open:             true,
		completedActions: make(map[lpb.CompletionStatus]int32),
		u:                u,
		items:            make(chan *bigquerytranslator.Item),
		bqSpec:           bqSpec,
	}
	l.startBackgroundProcess()
	return l
}

// startBackgroundProcess starts go routine to run tasks in the background.
func (l *Logger) startBackgroundProcess() {
	l.wg.Add(1)
	go l.processEvents()
	if l.u != nil {
		ctx, cancel := context.WithCancel(context.Background())
		l.cancelSamplerCtx = cancel
		l.wg.Add(1)
		go l.processUsageData(ctx)
	}
	if l.bqSpec != nil {
		l.wg.Add(1)
		go func() {
			l.processLogRecords()
			l.wg.Done()
		}()
	}
}

// New instantiates a new Logger.
func New(format Format, dir string, s stats.StatCollector, mi *ignoremismatch.MismatchIgnorer, e monitoring.StatExporter, u *usage.PsutilSampler, bqSpec *bigquery.BQSpec) (*Logger, error) {
	if format != TextFormat && format != ReducedTextFormat {
		return nil, fmt.Errorf("only text:// or reducedtext:// formats are currently supported, received %v", format)
	}
	ts := time.Now().Format("2006-01-02_15_04_05")
	filename := filepath.Join(dir, fmt.Sprintf("reproxy_%s.%s", ts, logFileSuffix(format)))
	recs, err := os.Create(filename)
	if err != nil {
		return nil, err
	}
	log.Infof("Created log file %s", filename)
	filename = filepath.Join(dir, fmt.Sprintf("reproxy_%s.rpi", ts))
	info, err := os.Create(filename)
	if err != nil {
		return nil, err
	}
	log.Infof("Created log file %s", filename)
	return newLogger(format, recs, info, s, mi, e, u, bqSpec), nil
}

// AddEventTimeToProxyInfo will add an reproxy level event to the ProxyInfo object.
func (l *Logger) AddEventTimeToProxyInfo(key string, from, to time.Time) {
	if l == nil {
		return
	}
	// A call to this function should be very rare so locking should be ok.
	l.mu.Lock()
	defer l.mu.Unlock()
	l.info.EventTimes[key] = command.TimeIntervalToProto(&command.TimeInterval{
		From: from,
		To:   to,
	})
}

// AddEventTimesToProxyInfo will add a map of reproxy level events to the ProxyInfo object.
func (l *Logger) AddEventTimesToProxyInfo(m map[string]*cpb.TimeInterval) {
	if l == nil {
		return
	}
	// A call to this function should be very rare so locking should be ok.
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, val := range m {
		l.info.EventTimes[key] = val
	}
}

// AddMetricIntToProxyInfo will add an reproxy level event to the ProxyInfo object.
func (l *Logger) AddMetricIntToProxyInfo(key string, value int64) {
	if l == nil {
		return
	}
	// A call to this function should be very rare so locking should be ok.
	l.mu.Lock()
	defer l.mu.Unlock()
	l.info.Metrics[key] = &lpb.Metric{Value: &lpb.Metric_Int64Value{value}}
}

// summarize the latest OS resource usage stats as protobuf messages.
func summarize(usage map[string][]int64) []*stpb.Stat {
	sPb := make([]*stpb.Stat, 0, 4)
	for name, value := range usage {
		sPb = append(sPb, stats.FromSeriesToProto(name, value))
	}
	return sPb
}

// collectResourceUsageSamples collects resource usage samples into logger's resourceUsage field.
func (l *Logger) collectResourceUsageSamples(samples map[string]int64) {
	if l == nil {
		return
	}
	if l.resourceUsage == nil {
		l.resourceUsage = make(map[string][]int64)
	}
	// Add current peak running actions num into samples map, so that it get
	// recorded into reproxy.INFO together with cpu/mem usage data.
	if samples == nil {
		samples = make(map[string]int64)
	}
	samples[unixTime] = time.Now().Unix()
	samples[peakNumActions] = int64(atomic.SwapInt32(&l.peakRunningActions, 0))
	// These log messages in reproxy.INFO are used for plotting the time series
	// of resource usage by a plotter.
	log.Infof("Resource Usage: %v", samples)
	delete(samples, unixTime)
	for k, v := range samples {
		if _, ok := l.resourceUsage[k]; ok {
			l.resourceUsage[k] = append(l.resourceUsage[k], v)
		} else {
			l.resourceUsage[k] = []int64{v}
		}
	}
}

// IncrementMetricIntToProxyInfo will increment a reproxy level event to the ProxyInfo object.
func (l *Logger) IncrementMetricIntToProxyInfo(key string, delta int64) {
	if l == nil {
		return
	}
	// A call to this function should be very rare so locking should be ok.
	l.mu.Lock()
	defer l.mu.Unlock()
	if m, ok := l.info.Metrics[key]; ok {
		im, ok := m.Value.(*lpb.Metric_Int64Value)
		if !ok {
			log.Warningf("Attempted to increment non int64 metric %s", key)
			return
		}
		im.Int64Value += delta
	} else {
		l.info.Metrics[key] = &lpb.Metric{Value: &lpb.Metric_Int64Value{delta}}
	}
}

// AddMetricDoubleToProxyInfo will add an reproxy level event to the ProxyInfo object.
func (l *Logger) AddMetricDoubleToProxyInfo(key string, value float64) {
	if l == nil {
		return
	}
	// A call to this function should be very rare so locking should be ok.
	l.mu.Lock()
	defer l.mu.Unlock()
	l.info.Metrics[key] = &lpb.Metric{Value: &lpb.Metric_DoubleValue{value}}
}

// AddMetricBoolToProxyInfo will add an reproxy level event to the ProxyInfo object.
func (l *Logger) AddMetricBoolToProxyInfo(key string, value bool) {
	if l == nil {
		return
	}
	// A call to this function should be very rare so locking should be ok.
	l.mu.Lock()
	defer l.mu.Unlock()
	l.info.Metrics[key] = &lpb.Metric{Value: &lpb.Metric_BoolValue{value}}
}

// AddFlagStringToProxyInfo will add an reproxy flag to the ProxyInfo object.
func (l *Logger) AddFlagStringToProxyInfo(key string, value string) {
	if l == nil {
		return
	}
	// A call to this function could be very frequent, so it's required to lock the
	// entire section of adding flags.
	l.mu.Lock()
	defer l.mu.Unlock()
	l.info.Flags[key] = value
	if key == "remote_disabled" {
		v, err := strconv.ParseBool(value)
		if err == nil {
			l.remoteDisabled = v
		}
	}
}

// AddFlags will add all reproxy flags to the ProxyInfo object.
func (l *Logger) AddFlags(flagSet *flag.FlagSet) {
	if l == nil {
		return
	}
	if flagSet == nil {
		log.Warningf("nil FlagSet pointer")
		return
	}
	flagSet.VisitAll(func(f *flag.Flag) {
		l.AddFlagStringToProxyInfo(f.Name, f.Value.String())
	})
}

// GetStatusSummary returns a snapshot for currently running and completed actions.
func (l *Logger) GetStatusSummary(ctx context.Context, _ *ppb.GetStatusSummaryRequest) (*ppb.GetStatusSummaryResponse, error) {
	if l == nil {
		return nil, errors.New("not running")
	}

	l.mu.RLock()
	defer l.mu.RUnlock()
	if !l.open {
		return nil, errors.New("not running")
	}

	out := make(chan *ppb.GetStatusSummaryResponse, 1)
	defer close(out)
	l.ch <- &summarizeActionsEvent{out}

	for {
		select {
		case <-ctx.Done():
			return nil, errors.New("timed out")
		case resp := <-out:
			return resp, nil
		}
	}
}

// LogActionStart logs start of an action. Use the returned LogRecord to track log events then pass to Log at the end of the action.
func (l *Logger) LogActionStart() *LogRecord {
	lr := NewLogRecord()
	if l != nil {
		l.mu.RLock()
		defer l.mu.RUnlock()
		if l.open {
			l.ch <- &startActionEvent{
				lr: lr,
			}
		}
	}
	return lr
}

// Log will add the record to be logged asynchronously.
func (l *Logger) Log(rec *LogRecord) {
	if l == nil {
		return
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.open {
		l.ch <- &endActionEvent{
			lr: rec,
		}
	}
}

// CloseAndAggregate deactivates the logger and waits for pending records to finish logging.
// The log file is then closed. Any subsequent Log calls will be discarded.
// Finally, aggregated build stats are generated and returned.
func (l *Logger) CloseAndAggregate() *spb.Stats {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	opened := l.open
	l.open = false
	l.mu.Unlock()
	if opened {
		close(l.ch)
	}
	if l.cancelSamplerCtx != nil {
		l.cancelSamplerCtx()
	}
	close(l.items)
	l.wg.Wait()
	l.info.Stats = append(l.info.Stats, summarize(l.resourceUsage)...)
	l.writeProxyInfo()
	l.stats.FinalizeAggregate([]*lpb.ProxyInfo{l.info})
	return l.stats.ToProto()
}

func (l *Logger) processEvents() {
	for event := range l.ch {
		event.apply(l)
	}
	if err := l.recsFile.Close(); err != nil {
		log.Errorf("Close error: %v", err)
	}
	l.wg.Done()
}

func (l *Logger) processUsageData(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			l.wg.Done()
			return
		case <-ticker.C:
			l.collectResourceUsageSamples(usage.Sample(l.u))
		}
	}
}

func (l *Logger) processLogRecords() {
	batchSize := l.bqSpec.BatchSize
	batch := make([]*lpb.LogRecord, 0, batchSize)
	for {
		r, ok := <-l.items
		if !ok {
			ctx, cancel := context.WithTimeout(context.Background(), l.bqSpec.Timeout)
			defer cancel()
			bigquery.InsertRows(ctx, batch, *l.bqSpec, false)
			return
		}
		batch = append(batch, r.LogRecord)
		if len(batch) == batchSize {
			l.wg.Add(1)
			go func(batch []*lpb.LogRecord) {
				ctx, cancel := context.WithTimeout(context.Background(), l.bqSpec.Timeout)
				defer cancel()
				bigquery.InsertRows(ctx, batch, *l.bqSpec, false)
				l.wg.Done()
			}(batch)
			batch = make([]*lpb.LogRecord, 0, batchSize)
		}
	}
}

func (l *Logger) writeProxyInfo() {
	if l.infoFile == nil {
		return
	}
	textb, err := protoencoding.TextWithIndent.Marshal(l.info)
	if err != nil {
		log.Errorf("Marshal error: %v", err)
		return
	}
	if _, err := l.infoFile.Write(textb); err != nil {
		log.Errorf("Write error: %v", err)
	}
	if err := l.infoFile.Close(); err != nil {
		log.Errorf("Close error: %v", err)
	}
}

// CommandRemoteMetadataToProto converts the sdk Metadata to RemoteMetadata proto.
func CommandRemoteMetadataToProto(r *command.Metadata) *lpb.RemoteMetadata {
	if r == nil {
		return &lpb.RemoteMetadata{}
	}
	res := &lpb.RemoteMetadata{
		NumInputFiles:          int32(r.InputFiles),
		NumInputDirectories:    int32(r.InputDirectories),
		TotalInputBytes:        r.TotalInputBytes,
		NumOutputFiles:         int32(r.OutputFiles),
		NumOutputDirectories:   int32(r.OutputDirectories),
		TotalOutputBytes:       r.TotalOutputBytes,
		CommandDigest:          r.CommandDigest.String(),
		ActionDigest:           r.ActionDigest.String(),
		LogicalBytesUploaded:   int64(r.LogicalBytesUploaded),
		RealBytesUploaded:      int64(r.RealBytesUploaded),
		LogicalBytesDownloaded: int64(r.LogicalBytesDownloaded),
		RealBytesDownloaded:    int64(r.RealBytesDownloaded),
		StderrDigest:           r.StderrDigest.String(),
		StdoutDigest:           r.StdoutDigest.String(),
	}
	res.EventTimes = make(map[string]*cpb.TimeInterval)
	for name, t := range r.EventTimes {
		res.EventTimes[name] = command.TimeIntervalToProto(t)
	}

	if auxiliary.AuxMsgDescriptor != nil && len(r.AuxiliaryMetadata) > 0 {
		res.AuxiliaryMetadata = auxiliary.FlatRawMsg(r.AuxiliaryMetadata[0].GetValue())
	}
	res.OutputFileDigests = make(map[string]string)
	for path, d := range r.OutputFileDigests {
		res.OutputFileDigests[path] = d.String()
	}
	res.OutputDirectoryDigests = make(map[string]string)
	for path, d := range r.OutputDirectoryDigests {
		res.OutputDirectoryDigests[path] = d.String()
	}
	return res
}

// CommandRemoteMetadataFromProto parses a RemoteMetadata proto into an sdk Metadata.
func CommandRemoteMetadataFromProto(rPb *lpb.RemoteMetadata) *command.Metadata {
	parseDigestString := func(digestStr string) digest.Digest {
		parsedDigest, err := digest.NewFromString(digestStr)
		if err != nil {
			log.Errorf("Unexpected digest string parse error: %s -> %v", digestStr, err)
			return digest.Digest{}
		}
		return parsedDigest
	}

	rm := &command.Metadata{
		InputFiles:             int(rPb.NumInputFiles),
		InputDirectories:       int(rPb.NumInputDirectories),
		TotalInputBytes:        rPb.TotalInputBytes,
		LogicalBytesUploaded:   rPb.LogicalBytesUploaded,
		RealBytesUploaded:      rPb.RealBytesUploaded,
		LogicalBytesDownloaded: rPb.LogicalBytesDownloaded,
		RealBytesDownloaded:    rPb.RealBytesDownloaded,
	}
	rm.CommandDigest = parseDigestString(rPb.CommandDigest)
	rm.ActionDigest = parseDigestString(rPb.ActionDigest)
	rm.StderrDigest = parseDigestString(rPb.StderrDigest)
	rm.StdoutDigest = parseDigestString(rPb.StdoutDigest)
	rm.EventTimes = make(map[string]*command.TimeInterval)
	for name, t := range rPb.EventTimes {
		rm.EventTimes[name] = command.TimeIntervalFromProto(t)
	}
	return rm
}

func toBytes(format Format, rec *lpb.LogRecord) ([]byte, error) {
	// This only supports TextFormat for now.
	// TODO(b/279056853): support other formats.
	if format != TextFormat && format != ReducedTextFormat {
		return nil, fmt.Errorf("only text or reducedtext formats are currently supported, received %s", format)
	}
	tmpRec, _ := proto.Clone(rec).(*lpb.LogRecord)
	if format == ReducedTextFormat && tmpRec.Command != nil {
		tmpRec.Command.Input = nil
		tmpRec.Command.Args = nil
	}
	if input := tmpRec.GetCommand().GetInput(); input != nil {
		// truncate large virtual inputs contents
		// http://b/171842303
		for _, vi := range input.VirtualInputs {
			if len(vi.Contents) > 1024 {
				vi.Contents = vi.Contents[:1024]
			}
		}
	}
	textb, err := protoencoding.TextWithIndent.Marshal(tmpRec)
	if err != nil {
		return nil, err
	}
	return append(textb, []byte(textDelimiter)...), nil
}

func toBigQueryRecords(rec *lpb.LogRecord) *lpb.LogRecord {
	tmpRec, _ := proto.Clone(rec).(*lpb.LogRecord)
	// We want to Save the reduced text format into bigquery for cache hit actions to save storage cost.
	if tmpRec.GetResult().GetStatus() == cpb.CommandResultStatus_CACHE_HIT {
		if tmpRec.Command != nil {
			tmpRec.Command.Input = nil
			tmpRec.Command.Args = nil
		}
	}
	if input := tmpRec.GetCommand().GetInput(); input != nil {
		// truncate large virtual inputs contents
		// http://b/171842303
		for _, vi := range input.VirtualInputs {
			if len(vi.Contents) > 1024 {
				vi.Contents = vi.Contents[:1024]
			}
		}
	}
	return tmpRec
}

// ParseFromFormatFile reads Records from a log file created by a Logger.
// Deprecated: TODO(b/279058022): remove this when format file is no longer used.
func ParseFromFormatFile(formatfile string) ([]*lpb.LogRecord, error) {
	format, filepath, err := ParseFilepath(formatfile)
	if err != nil {
		return nil, err
	}
	return ParseFromFile(format, filepath)
}

// ParseFromFile reads Records from a log file created by a Logger.
func ParseFromFile(format Format, filepath string) ([]*lpb.LogRecord, error) {
	// TODO(b/279056853): support other formats.
	if format != TextFormat && format != ReducedTextFormat {
		return nil, fmt.Errorf("only text or reducedtext formats are currently supported, received %s", format)
	}
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, err
	}
	rst := strings.Split(string(data), textDelimiter)
	var recs []*lpb.LogRecord
	for _, s := range rst[:len(rst)-1] {
		recPb := &lpb.LogRecord{}
		if err := prototext.Unmarshal([]byte(s), recPb); err != nil {
			return nil, err
		}
		recs = append(recs, recPb)
	}
	return recs, nil
}

// ParseFromLogDirs reads Records from log files created by a Logger.
func ParseFromLogDirs(format Format, logDirs []string) ([]*lpb.LogRecord, []*lpb.ProxyInfo, error) {
	var recs []*lpb.LogRecord
	var infoPbs []*lpb.ProxyInfo
	found := false
	logFileRegex, err := regexp.Compile("reproxy.*" + logFileSuffix(format))
	if err != nil {
		return nil, nil, err
	}
	infoFileRegex, err := regexp.Compile("reproxy.*rpi")
	if err != nil {
		return nil, nil, err
	}
	for _, dir := range logDirs {
		if dir == "" {
			dir = "."
		}
		children, err := os.ReadDir(dir)
		if err != nil {
			return nil, nil, fmt.Errorf("error reading %s: %v", dir, err)
		}
		for _, c := range children {
			if logFileRegex.MatchString(c.Name()) {
				found = true
				rs, err := ParseFromFile(format, filepath.Join(dir, c.Name()))
				if err != nil {
					return nil, nil, err
				}
				recs = append(recs, rs...)
				continue
			}
			if infoFileRegex.MatchString(c.Name()) {
				infoPb := &lpb.ProxyInfo{}
				data, err := os.ReadFile(filepath.Join(dir, c.Name()))
				if err != nil {
					return nil, nil, err
				}
				if err := prototext.Unmarshal(data, infoPb); err != nil {
					return nil, nil, err
				}
				infoPbs = append(infoPbs, infoPb)
				continue
			}
			log.Warningf("ignore log file %s/%s", dir, c.Name())
		}
	}
	if !found {
		return nil, nil, fmt.Errorf("found no %s proxy log files under %v", format, logDirs)
	}
	return recs, infoPbs, nil
}

// RecordEventTime ensures LocalMetedata.EventTimes is instantiated, calculates the time
// interval between now and from, adds it to EventTimes with the given event String as key and
// returns the now time. This method is thread safe.
func (lr *LogRecord) RecordEventTime(event string, from time.Time) time.Time {
	now := time.Now()
	lr.mu.Lock()
	defer lr.mu.Unlock()
	if lr.GetLocalMetadata() == nil {
		lr.LocalMetadata = &lpb.LocalMetadata{}
	}
	if lr.LocalMetadata.GetEventTimes() == nil {
		lr.LocalMetadata.EventTimes = make(map[string]*cpb.TimeInterval)
	}
	lr.LocalMetadata.EventTimes[event] = command.TimeIntervalToProto(&command.TimeInterval{From: from, To: now})
	return now
}

// CopyEventTimesFrom copies all entries from other.LocalMetadata.EventTimes to
// this LogRecord's LocalMetadata.EventTimes. This method is thread safe.
func (lr *LogRecord) CopyEventTimesFrom(other *LogRecord) {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	other.mu.RLock()
	defer other.mu.RUnlock()
	for k, v := range other.GetLocalMetadata().GetEventTimes() {
		lr.LocalMetadata.EventTimes[k] = v
	}
}

// EndAllEventTimes adds time.Now() as the end time for all entries in LocalMetadata.EventTimes.
// This method is thread safe.
func (lr *LogRecord) EndAllEventTimes() {
	end := time.Now()
	lr.mu.Lock()
	defer lr.mu.Unlock()
	et := lr.GetLocalMetadata().GetEventTimes()
	if et == nil {
		return
	}
	for k, t := range et {
		if t.To == nil {
			t.To = command.TimeToProto(end)
		}
		lr.LocalMetadata.EventTimes[k] = t
	}
}
