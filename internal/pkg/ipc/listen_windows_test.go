// Copyright 2026 Google LLC
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

//go:build windows

package ipc

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestListen_RejectsNonPipeAddress(t *testing.T) {
	tests := []struct {
		name string
		addr string
	}{
		{"empty", ""},
		{"empty_pipe_name", "pipe://"},
		{"loopback_ip", "127.0.0.1:0"},
		{"localhost", "localhost:8000"},
		{"unix_socket", "unix:///tmp/reproxy.sock"}, // unix:// is only supported on POSIX (listen_posix.go), not Windows.
	}

	const wantErr = "only pipe://<name> addresses are supported"
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l, err := Listen(tc.addr)
			if err == nil {
				l.Close()
				t.Fatalf("Listen(%q) succeeded, want error containing %q", tc.addr, wantErr)
			}
			if !strings.Contains(err.Error(), wantErr) {
				t.Errorf("Listen(%q) error = %q, want substring %q", tc.addr, err.Error(), wantErr)
			}
		})
	}
}

func TestListen_NamedPipeAcceptsConnections(t *testing.T) {
	addr := fmt.Sprintf("pipe://reclient-ipc-test-%d.ipc", time.Now().UnixNano())
	l, err := Listen(addr)
	if err != nil {
		t.Fatalf("Listen(%q) unexpected error: %v", addr, err)
	}
	t.Cleanup(func() { l.Close() })
}
