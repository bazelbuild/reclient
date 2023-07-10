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
	"github.com/fatih/color"
	"google.golang.org/grpc"
	"strings"
	ppb "team/foundry-x/re-client/api/proxy"
	"team/foundry-x/re-client/internal/pkg/ipc"
	"team/foundry-x/re-client/internal/pkg/printer"
	"team/foundry-x/re-client/internal/pkg/rbeflag"
	"team/foundry-x/re-client/internal/pkg/reproxystatus"
	"time"
)

var (
	serverAddr = flag.String("server_address", "",
		"The server address in the format of host:port for network, or unix:///file for unix domain sockets. "+
			"If empty (default) then all reproxy instances will be dialed")
	colorize = flag.String("color", "auto", "Control the output color mode; one of (off, on, auto)")
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

	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()
	clients, err := dialReproxy(ctx)
	if err != nil {
		printer.Fatal(fmt.Sprintf("Failed to dial reproxy: %v", err))
	}

	summaries := reproxystatus.FetchAllStatusSummaries(ctx, clients)
	if len(summaries) == 0 {
		if *serverAddr == "" {
			fmt.Fprintf(color.Output, "Reproxy is not running\n")
		} else {
			fmt.Fprintf(color.Output, "Reproxy(%s) is not running\n", *serverAddr)
		}
		return
	}

	for _, s := range summaries {
		fmt.Fprintln(color.Output, s.HumanReadable())
	}
}

func dialReproxy(ctx context.Context) (map[string]ppb.StatusClient, error) {
	var conns map[string]*grpc.ClientConn
	var err error
	if *serverAddr == "" {
		conns, err = ipc.DialAllContexts(ctx)
	} else {
		var conn *grpc.ClientConn
		conn, err = ipc.DialContext(ctx, *serverAddr)
		conns = map[string]*grpc.ClientConn{
			*serverAddr: conn,
		}
	}
	if err != nil {
		return nil, err
	}
	clients := make(map[string]ppb.StatusClient, len(conns))
	for name, conn := range conns {
		clients[name] = ppb.NewStatusClient(conn)
	}
	return clients, nil
}
