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

// Binary rpl2cloudtrace converts *.rpl into cloud trace.
//
//	$ rpl2cloudtrace --log_path text:///tmp/reproxy_log.rpl \
//	    --project_id rbe-chromium-trusted
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	lpb "github.com/bazelbuild/reclient/api/log"
	"github.com/bazelbuild/reclient/internal/pkg/labels"
	"github.com/bazelbuild/reclient/internal/pkg/logger"
	"github.com/bazelbuild/reclient/internal/pkg/rbeflag"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/moreflag"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	trace "cloud.google.com/go/trace/apiv2"
	cpb "github.com/bazelbuild/remote-apis-sdks/go/api/command"
	log "github.com/golang/glog"
	tpb "google.golang.org/genproto/googleapis/devtools/cloudtrace/v2"
	spb "google.golang.org/genproto/googleapis/rpc/status"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
)

var (
	proxyLogDir []string
	logPath     = flag.String("log_path", "", "Path to reproxy_log.rpl file. E.g., text:///tmp/reproxy_log.rpl")
	logFormat   = flag.String("log_format", "text", "Format of proxy log. Currently only text is supported.")
	projectID   = flag.String("project_id", os.Getenv("GOOGLE_CLOUD_PROJECT"), "project id for cloud trace")

	traceThreshold = flag.Duration("trace_threshold", 1*time.Second, "threshold to emit trace")
	spanThreshold  = flag.Duration("span_threshold", 1*time.Microsecond, "threshold to emit span")
	batchSize      = flag.Int("batch_size", 1500, "batch size for BatchWriteSpans")

	traceAttributes []string
)

func init() {
	flag.Var((*moreflag.StringListValue)(&proxyLogDir), "proxy_log_dir", "If provided, the directory path to a proxy log file of executed records.")
	flag.Var((*moreflag.StringListValue)(&traceAttributes), "attributes", "comma separated key=value for attributes")
}

func truncatableString(s string, limit int) *tpb.TruncatableString {
	n := len(s)
	if n < limit {
		return &tpb.TruncatableString{Value: s}
	}
	return &tpb.TruncatableString{
		Value:              s[:limit],
		TruncatedByteCount: int32(n - limit),
	}
}

func convertEventTime(cat string, tctx *traceContext, parent string, ets map[string]*cpb.TimeInterval, attrs *tpb.Span_Attributes, st *spb.Status) (spans []*tpb.Span, start, end time.Time) {
	for key, et := range ets {
		// ignore events that doesn't have timestamp
		// e.g. ServerWorkerExecution when RBE execution failed.
		if et.From == nil || et.To == nil {
			continue
		}
		from := et.GetFrom().AsTime()
		to := et.GetTo().AsTime()
		if start.IsZero() || start.After(from) {
			start = from
		}
		if end.IsZero() || end.Before(to) {
			end = to
		}
		dur := to.Sub(from)
		if dur < *spanThreshold {
			log.V(1).Infof("drop short span: %s.%s %s", cat, key, dur)
			continue
		}
		sctx := tctx.newSpanContext(fmt.Sprintf("%s.%s", cat, key))
		spans = append(spans, &tpb.Span{
			Name:         sctx.Name(),
			SpanId:       sctx.SpanID(),
			ParentSpanId: parent,
			DisplayName:  truncatableString(fmt.Sprintf("reproxy.%s.%s", cat, key), 128),
			StartTime:    tspb.New(from),
			EndTime:      tspb.New(to),
			Attributes:   attrs,
			Status:       st,
			SpanKind:     tpb.Span_INTERNAL,
		})
		log.V(1).Infof("%s %s %s.%s %s %s %s", sctx.TraceID(), sctx.SpanID(), cat, key, start, end, dur)
	}
	return spans, start, end
}

type traceContext struct {
	projectID string
	traceID   [16]byte
	res       map[string]*tpb.AttributeValue
}

func newTraceContext(projectID string, execID string, res map[string]*tpb.AttributeValue) (*traceContext, error) {
	var traceID [16]byte
	_, err := rand.Read(traceID[:])
	if err != nil {
		return nil, err
	}
	if execID != "" {
		b, err := hex.DecodeString(strings.ReplaceAll(execID, "-", ""))
		if err != nil {
			log.Warningf("Failed to parse execID %q: %v", execID, err)
		} else {
			copy(traceID[:], b)
		}
	} else {
		log.Warningf("execution_id is empty. Use %s as traceID %s", hex.EncodeToString(traceID[:]))
	}
	// can we use execution id or so as trace id?
	return &traceContext{
		projectID: projectID,
		traceID:   traceID,
		res:       res,
	}, nil
}

func (tc *traceContext) newSpanContext(name string) spanContext {
	var spanID [8]byte
	s := sha256.Sum256([]byte(name))
	copy(spanID[:], s[:])
	return spanContext{
		t:      tc,
		spanID: spanID,
	}
}

func (tc *traceContext) TraceID() string {
	return hex.EncodeToString(tc.traceID[:])
}

func (tc *traceContext) attrs(attrs map[string]*tpb.AttributeValue) *tpb.Span_Attributes {
	m := make(map[string]*tpb.AttributeValue)
	for k, v := range tc.res {
		m[k] = v
	}
	for k, v := range attrs {
		m[k] = v
	}
	return &tpb.Span_Attributes{
		AttributeMap: m,
	}
}

type spanContext struct {
	t      *traceContext
	spanID [8]byte
}

func (sc spanContext) Name() string {
	return path.Join("projects", sc.t.projectID,
		"traces", hex.EncodeToString(sc.t.traceID[:]),
		"spans", hex.EncodeToString(sc.spanID[:]))
}

func (sc spanContext) TraceID() string {
	return sc.t.TraceID()
}

func (sc spanContext) SpanID() string {
	return hex.EncodeToString(sc.spanID[:])
}

func attrValue(v interface{}) *tpb.AttributeValue {
	var iv int64
	switch v := v.(type) {
	case string:
		return &tpb.AttributeValue{
			Value: &tpb.AttributeValue_StringValue{StringValue: truncatableString(v, 256)},
		}
	case bool:
		return &tpb.AttributeValue{
			Value: &tpb.AttributeValue_BoolValue{BoolValue: v},
		}
	case int:
		iv = int64(v)
	case uint:
		iv = int64(v)
	case int8:
		iv = int64(v)
	case uint8:
		iv = int64(v)
	case int16:
		iv = int64(v)
	case uint16:
		iv = int64(v)
	case int32:
		iv = int64(v)
	case uint32:
		iv = int64(v)
	case int64:
		iv = v
	case uint64:
		iv = int64(v)
	}
	return &tpb.AttributeValue{
		Value: &tpb.AttributeValue_IntValue{IntValue: iv},
	}
}

func statusCode(s cpb.CommandResultStatus_Value) *spb.Status {
	switch s {
	case cpb.CommandResultStatus_SUCCESS, cpb.CommandResultStatus_CACHE_HIT:
		return status.New(codes.OK, s.String()).Proto()
	case cpb.CommandResultStatus_NON_ZERO_EXIT,
		cpb.CommandResultStatus_REMOTE_ERROR,
		cpb.CommandResultStatus_LOCAL_ERROR:
		return status.New(codes.Internal, s.String()).Proto()

	case cpb.CommandResultStatus_TIMEOUT:
		return status.New(codes.DeadlineExceeded, s.String()).Proto()
	case cpb.CommandResultStatus_INTERRUPTED:
		return status.New(codes.Canceled, s.String()).Proto()
	default:
		return status.New(codes.Unknown, s.String()).Proto()
	}
}

func convertLogRecord(ctx context.Context, logrec *lpb.LogRecord, tctx *traceContext) []*tpb.Span {
	var spans []*tpb.Span
	var pfrom, pto time.Time

	sctx := tctx.newSpanContext("RunCommand")
	parent := sctx.SpanID()

	output := "???"
	if outputs := logrec.GetCommand().GetOutput().GetOutputFiles(); len(outputs) > 0 {
		output = outputs[0]
	}
	wd := logrec.GetCommand().GetWorkingDirectory()
	output = strings.TrimPrefix(output, wd)

	identifiers := logrec.GetCommand().GetIdentifiers()
	rm := logrec.GetRemoteMetadata()
	if rm != nil {
		sps, rfrom, rto := convertEventTime("Remote", tctx, parent, rm.GetEventTimes(), tctx.attrs(map[string]*tpb.AttributeValue{
			"action_digest":          attrValue(rm.GetActionDigest()),
			"status":                 attrValue(rm.GetResult().GetStatus().String()),
			"exit_code":              attrValue(rm.GetResult().GetExitCode()),
			"cache_hit":              attrValue(rm.GetCacheHit()),
			"num_input_files":        attrValue(rm.GetNumInputFiles()),
			"num_input_directories":  attrValue(rm.GetNumInputDirectories()),
			"total_input_bytes":      attrValue(rm.GetTotalInputBytes()),
			"num_output_files":       attrValue(rm.GetNumOutputFiles()),
			"num_output_directories": attrValue(rm.GetNumOutputDirectories()),
			"total_output_bytes":     attrValue(rm.GetTotalOutputBytes()),
		}), statusCode(rm.GetResult().GetStatus()))
		spans = append(spans, sps...)
		if pfrom.IsZero() || pfrom.After(rfrom) {
			pfrom = rfrom
		}
		if pto.IsZero() || pto.Before(rto) {
			pto = rto
		}
	}
	lm := logrec.GetLocalMetadata()
	if lm != nil {
		sps, lfrom, lto := convertEventTime("Local", tctx, parent, lm.GetEventTimes(), tctx.attrs(map[string]*tpb.AttributeValue{
			"status":           attrValue(lm.GetResult().GetStatus().String()),
			"exit_code":        attrValue(lm.GetResult().GetExitCode()),
			"executed_locally": attrValue(lm.GetExecutedLocally()),

			"valid_cache_hit": attrValue(lm.GetValidCacheHit()),
			"updated_cache":   attrValue(lm.GetUpdatedCache()),
			"labels":          attrValue(labels.ToKey(lm.GetLabels())),
		}), statusCode(lm.GetResult().GetStatus()))
		spans = append(spans, sps...)
		if pfrom.IsZero() || pfrom.After(lfrom) {
			pfrom = lfrom
		}
		if pto.IsZero() || pto.Before(lto) {
			pto = lto
		}
	}
	st := statusCode(logrec.GetResult().GetStatus())
	attrs := tctx.attrs(map[string]*tpb.AttributeValue{
		"output":                    attrValue(output),
		"command_id":                attrValue(identifiers.GetCommandId()),
		"invocation_id":             attrValue(identifiers.GetInvocationId()),
		"correlated_invocations_id": attrValue(identifiers.GetCorrelatedInvocationsId()),
		"tool_name":                 attrValue(identifiers.GetToolName()),
		"tool_version":              attrValue(identifiers.GetToolVersion()),
		"execution_id":              attrValue(identifiers.GetExecutionId()),
		"exit_code":                 attrValue(logrec.GetResult().GetExitCode()),
	})
	if msg := logrec.GetResult().GetMsg(); msg != "" {
		st.Message = fmt.Sprintf("%s: %s", st.Message, msg)
	}
	spans = append(spans, &tpb.Span{
		Name:         sctx.Name(),
		SpanId:       sctx.SpanID(),
		ParentSpanId: "",
		DisplayName:  truncatableString("reproxy.RunCommand", 128),
		StartTime:    tspb.New(pfrom),
		EndTime:      tspb.New(pto),
		Attributes:   attrs,
		Status:       st,
	})
	dur := pto.Sub(pfrom)
	if dur < *traceThreshold {
		log.V(1).Infof("drop trace %s dur=%s", identifiers, dur)
		return nil
	}
	log.Infof("%s %s reproxy.RunCommand %s %s %s", sctx.TraceID(), sctx.SpanID(), pfrom, pto, dur)
	return spans
}

func convert(ctx context.Context, logs []*lpb.LogRecord, projectID string, res map[string]*tpb.AttributeValue, client *trace.Client) ([]*tpb.Span, error) {
	var spans []*tpb.Span
	var batch []*tpb.Span
	for _, logrec := range logs {
		identifiers := logrec.GetCommand().GetIdentifiers()
		tctx, err := newTraceContext(projectID, identifiers.GetExecutionId(), res)
		if err != nil {
			return spans, err
		}
		sps := convertLogRecord(ctx, logrec, tctx)
		batch = append(batch, sps...)
		if len(batch) > *batchSize {
			t := time.Now()
			err = client.BatchWriteSpans(ctx, &tpb.BatchWriteSpansRequest{
				Name:  path.Join("projects", projectID),
				Spans: batch,
			})
			if err != nil {
				return spans, fmt.Errorf("failed to export spans: %v", err)
			}
			log.V(1).Infof("exported %d spans: %s", len(batch), time.Since(t))
			spans = append(spans, batch...)
			batch = nil
		}
	}
	if len(batch) > 0 {
		t := time.Now()
		err := client.BatchWriteSpans(ctx, &tpb.BatchWriteSpansRequest{
			Name:  path.Join("projects", projectID),
			Spans: batch,
		})
		if err != nil {
			return spans, fmt.Errorf("failed to export spans: %v", err)
		}
		log.V(1).Infof("exported %d spans: %s", len(batch), time.Since(t))
		spans = append(spans, batch...)
		batch = nil
	}
	return spans, nil
}

func main() {
	defer log.Flush()
	ctx := context.Background()
	rbeflag.Parse()
	var logRecords []*lpb.LogRecord
	var err error
	switch {
	case *logPath != "":
		log.Infof("Loading log from %v...", *logPath)
		logRecords, err = logger.ParseFromFormatFile(*logPath)
		if err != nil {
			log.Fatalf("Failed reading proxy log: %v", err)
		}
	case len(proxyLogDir) > 0:
		format, err := logger.ParseFormat(*logFormat)
		if err != nil {
			log.Fatal(err)
		}
		log.Infof("Loading log from %v %q...", format, proxyLogDir)
		logRecords, _, err = logger.ParseFromLogDirs(format, proxyLogDir)
		if err != nil {
			log.Fatalf("Failed reading proxy log: %v", err)
		}
	default:
		log.Fatal("Must provide proxy log path or proxy log dir.")
	}

	log.Infof("export to project=%s", *projectID)
	client, err := trace.NewClient(ctx)
	if err != nil {
		log.Fatalf("Failed to create trace client: %v", err)
	}
	defer client.Close()
	res := map[string]*tpb.AttributeValue{}
	for _, attr := range traceAttributes {
		i := strings.Index(attr, "=")
		if i < 0 {
			log.Fatalf("bad attributes: %q", attr)
		}
		key := attr[:i]
		value := attr[i+1:]
		res[key] = attrValue(value)
	}

	spans, err := convert(ctx, logRecords, *projectID, res, client)
	if err != nil {
		log.Fatalf("Failed to convert: %v", err)
	}
	log.Infof("%d requests %d spans", len(logRecords), len(spans))
}
