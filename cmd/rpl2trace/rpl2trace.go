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

// Binary rpl2trace converts *.rpl into trace format. The json file can be used
// for chrome://tracing and https://ui.perfetto.dev
//
//	$ rpl2trace --log_path /tmp/reproxy_log.rpl --output trace.json
//
// This binary also works with *.rrpl file.
//
//	$ bazelisk run //cmd/rpl2trace --config=remotelinux -- \
//	 	--log_path /tmp/reproxy_log.rrpl --output /tmp/trace.json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	lpb "github.com/bazelbuild/reclient/api/log"
	"github.com/bazelbuild/reclient/internal/pkg/logger"
	"github.com/bazelbuild/reclient/internal/pkg/rbeflag"

	cpb "github.com/bazelbuild/remote-apis-sdks/go/api/command"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/moreflag"
	log "github.com/golang/glog"
)

var (
	proxyLogDir    []string
	logPath        = flag.String("log_path", "", "Path to reproxy_log.txt file. E.g., text:///tmp/reproxy_log.txt")
	logFormat      = flag.String("log_format", "text", "Format of proxy log. Currently only text is supported.")
	outputFilename = flag.String("output", "trace.json", "output filename")
	traceLevel     = flag.Int("trace_level", 3, "trace level. 0=cmd req. 1=local only. 2=local+remote, 3=local+remote+worker")
)

// https://docs.google.com/document/d/1CvAClvFfyA5R-PhYUmn5OOQtYMH4h6I0nSsKchNAySU/preview

const (
	localTID  = 1
	remoteTID = 2
	workerTID = 3
)

type event struct {
	Name      string                 `json:"name"`
	Cat       string                 `json:"cat"`
	Phase     string                 `json:"ph"`
	Timestamp int64                  `json:"ts"`  // micro seconds
	Dur       int64                  `json:"dur"` // micro seconds
	Pid       int                    `json:"pid"`
	Tid       int                    `json:"tid"`
	Args      map[string]interface{} `json:"args"`
}

func convertEventTime(cat string, pid, tid int, ets map[string]*cpb.TimeInterval, args map[string]interface{}, level int) (events []event, start, end time.Time) {
	name := args["target"]
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
		if dur < 1*time.Microsecond {
			continue
		}
		etid := tid
		if strings.HasPrefix(key, "Server") {
			etid++
		}
		if etid <= level {
			events = append(events, event{
				Name:      fmt.Sprintf("%s:%s", cat, key),
				Cat:       key,
				Phase:     "X",
				Timestamp: from.UnixNano() / 1e3,
				Dur:       int64(dur / time.Microsecond),
				Pid:       pid,
				Tid:       etid,
				Args:      args,
			})
			log.Infof("%s - %s:%s %s %s %s", name, cat, key, from, to, dur)
		}
	}
	dur := end.Sub(start)
	if tid <= level {
		events = append(events, event{
			Name:      fmt.Sprintf("%s - %s", name, cat),
			Cat:       cat,
			Phase:     "X",
			Timestamp: start.UnixNano() / 1e3,
			Dur:       int64(dur / time.Microsecond),
			Pid:       pid,
			Tid:       tid,
			Args:      args,
		})
	}
	log.Infof("%s - %s %s %s %s", name, cat, start, end, dur)
	return events, start, end
}

func convertLogRecord(ctx context.Context, log *lpb.LogRecord, level int) ([]event, time.Time, time.Time) {
	var events []event
	pid := 0
	var pfrom, pto time.Time
	output := "???"
	if outputs := log.GetCommand().GetOutput().GetOutputFiles(); len(outputs) > 0 {
		output = outputs[0]
	}
	wd := log.GetCommand().GetWorkingDirectory()
	output = strings.TrimPrefix(output, wd)
	identifiers := log.GetCommand().GetIdentifiers()
	rm := log.GetRemoteMetadata()
	if rm != nil && level >= remoteTID {
		evs, rfrom, rto := convertEventTime("Remote", pid, remoteTID, rm.GetEventTimes(), map[string]interface{}{
			"target":        output,
			"identifiers":   identifiers,
			"action_digest": rm.GetActionDigest(),
			"result":        rm.GetResult(),
		}, level)
		events = append(events, evs...)
		pfrom = rfrom
		pto = rto
	}
	lm := log.GetLocalMetadata()
	if lm != nil {
		evs, lfrom, lto := convertEventTime("Local", pid, localTID, lm.GetEventTimes(), map[string]interface{}{
			"target":      output,
			"identifiers": identifiers,
			"result":      lm.GetResult(),
		}, level)
		events = append(events, evs...)
		if pfrom.IsZero() || pfrom.After(lfrom) {
			pfrom = lfrom
		}
		if pto.IsZero() || pto.Before(lto) {
			pto = lto
		}
	}
	if level == 0 {
		dur := pto.Sub(pfrom)
		events = append(events, event{
			Name:      output,
			Cat:       "Command Request",
			Phase:     "X",
			Timestamp: pfrom.UnixNano() / 1e3,
			Dur:       int64(dur / time.Microsecond),
			Pid:       pid,
			Tid:       0,
			Args: map[string]interface{}{
				"target":      output,
				"identifiers": identifiers,
				"result":      log.GetResult(),
			},
		})
	}
	return events, pfrom, pto
}

func convertLogRecords(ctx context.Context, logs []*lpb.LogRecord, level int) []event {
	var events []event
	var running []time.Time
	for _, log := range logs {
		evs, from, to := convertLogRecord(ctx, log, level)
		pid := 0
		for i, r := range running {
			if r.Before(from) {
				running[i] = to
				pid = i + 1
				break
			}
		}
		if pid == 0 {
			running = append(running, to)
			pid = len(running)
		}
		for i := range evs {
			evs[i].Pid = pid
		}
		events = append(events, evs...)
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].Timestamp != events[i].Timestamp {
			return events[i].Timestamp < events[j].Timestamp
		}
		return events[i].Dur >= events[j].Dur
	})
	return events
}

func convert(ctx context.Context, logs []*lpb.LogRecord, level int, fname string) ([]event, error) {
	events := convertLogRecords(ctx, logs, level)
	buf, err := json.Marshal(events)
	if err != nil {
		return events, err
	}
	return events, os.WriteFile(fname, buf, 0644)
}

func main() {
	defer log.Flush()
	ctx := context.Background()
	flag.Var((*moreflag.StringListValue)(&proxyLogDir), "proxy_log_dir", "If provided, the directory path to a proxy log file of executed records.")
	rbeflag.Parse()
	var logRecords []*lpb.LogRecord
	var err error

	format, err := logger.ParseFormat(*logFormat)
	if err != nil {
		log.Fatal(err)
	}

	switch {
	case *logPath != "":
		fmt.Printf("Loading log from %v %v...\n", format, *logPath)
		log.Infof("Loading log from %v %v...", format, *logPath)
		logRecords, err = logger.ParseFromFile(format, *logPath)
		if err != nil {
			log.Fatalf("Failed reading proxy log: %v", err)
		}
	case len(proxyLogDir) > 0:
		fmt.Printf("Loading log from %v %q...\n", format, proxyLogDir)
		log.Infof("Loading log from %v %q...", format, proxyLogDir)
		logRecords, _, err = logger.ParseFromLogDirs(format, proxyLogDir)
		if err != nil {
			log.Fatalf("Failed reading proxy log: %v", err)
		}
	default:
		log.Fatal("Must provide proxy log path or proxy log dir.")
	}

	absOutput, err := filepath.Abs(*outputFilename)
	if err != nil {
		log.Fatalf("Abs path for %q: %v", *outputFilename, err)
	}
	log.Infof("Writing to %s...", absOutput)
	events, err := convert(ctx, logRecords, *traceLevel, *outputFilename)
	if err != nil {
		log.Fatalf("Unable to convert into trace json: %v", err)
	}
	log.Infof("%d requests %d events", len(logRecords), len(events))
	fmt.Printf("%d requests %d events\n", len(logRecords), len(events))
}
