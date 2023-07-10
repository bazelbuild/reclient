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

//go:build !windows

package ipc

import (
	"context"
	"os/exec"
	"strings"

	log "github.com/golang/glog"
	"google.golang.org/grpc"
)

const lsofOutputPrefix = "n/"

// DialContext connects to the serverAddress for grpc.
func DialContext(ctx context.Context, serverAddr string) (*grpc.ClientConn, error) {
	return grpc.DialContext(ctx, serverAddr, grpc.WithInsecure(), grpc.WithBlock(), grpc.WithMaxMsgSize(GrpcMaxMsgSize))
}

// DialAllContexts searches for and connects to all reproxy sockets
func DialAllContexts(ctx context.Context) (map[string]*grpc.ClientConn, error) {
	conns := make(map[string]*grpc.ClientConn)
	lsofOutput, err := execLsof("-U", "-c", "reproxy", "-a", "-Fn")

	if err != nil {
		// Lsof completes with exit code 1 when there is no output
		if err.(*exec.ExitError).ExitCode() == 1 {
			return map[string]*grpc.ClientConn{}, nil
		}
		return nil, err
	}
	for _, serverAddr := range parseSockets(lsofOutput) {
		if conn, err := DialContext(ctx, serverAddr); err != nil {
			log.Warningf("Error connecting to %s: %s", serverAddr, err)
		} else {
			conns[serverAddr] = conn
		}
	}
	return conns, nil
}

func parseSockets(lsofOutput string) []string {
	var sockets []string
	for _, line := range strings.Split(lsofOutput, "\n") {
		if socket := parseSocketLine(line); socket != "" {
			sockets = append(sockets, "unix://"+socket)
		}
	}
	return sockets
}

func parseSocketLine(line string) string {
	for _, s := range strings.Split(line, " ") {
		if strings.HasPrefix(s, lsofOutputPrefix) {
			return strings.TrimLeft(s, "n")
		}
	}
	return ""
}

func execLsof(args ...string) (string, error) {
	command := "lsof"
	args = append([]string{"-w"}, args...)
	output, err := exec.Command(command, args...).Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}
