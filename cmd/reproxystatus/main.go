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

// Binary to get status for running reproxy instances
//
// $ reproxystatus --color=
package main

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/gosuri/uilive"

	"github.com/bazelbuild/reclient/internal/pkg/printer"
	"github.com/bazelbuild/reclient/internal/pkg/rbeflag"
	"github.com/bazelbuild/reclient/internal/pkg/reproxystatus"
)

var (
	serverAddr = flag.String("server_address", "",
		"The server address in the format of host:port for network, or unix:///file for unix domain sockets. "+
			"If empty (default) then all reproxy instances will be dialed")
	colorize = flag.String("color", "auto", "Control the output color mode; one of (off, on, auto)")
	watch    = flag.Duration("watch", 0, "If greater than 0, every interval the ternimal will be cleared and the output will be printed.")
)

var (
	dialTimeout = 30 * time.Second
)

func main() {
	rbeflag.Parse()
	switch strings.ToLower(*colorize) {
	case "off", "false":
		color.NoColor = true
	case "on", "true":
		color.NoColor = false
	case "auto":
	default:
		fmt.Fprintf(color.Error, "ERROR: invalid --color mode: %v", *colorize)
	}
	if *watch < 0 {
		printer.Fatal("ERROR: --watch needs to be >= 0")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var tracker reproxystatus.ReproxyTracker
	if *serverAddr != "" {
		tracker = &reproxystatus.SingleReproxyTracker{ServerAddress: *serverAddr}
	} else {
		tracker = &reproxystatus.SocketReproxyTracker{}
	}
	if *watch == 0 {
		tctx, cancel := context.WithTimeout(ctx, dialTimeout)
		defer cancel()
		reproxystatus.PrintSummaries(tctx, color.Output, tracker)
	} else {
		writer := uilive.New()
		writer.Out = color.Output
		ticker := time.NewTicker(*watch)
		for ; true; <-ticker.C {
			tctx, cancel := context.WithTimeout(ctx, dialTimeout)
			reproxystatus.PrintSummaries(tctx, writer, tracker)
			cancel()
			writer.Flush()
		}
	}
}
