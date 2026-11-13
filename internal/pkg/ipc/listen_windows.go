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

//go:build windows

package ipc

import (
	"fmt"
	"net"
	"strings"

	winio "github.com/Microsoft/go-winio"
)

// Listen announces on serverAddr to accept gRPC connections.
// Only pipe:// addresses are supported on Windows.
func Listen(serverAddr string) (net.Listener, error) {
	address, ok := strings.CutPrefix(serverAddr, "pipe://")
	if !ok || address == "" {
		return nil, fmt.Errorf("invalid server address %q: only pipe://<name> addresses are supported", serverAddr)
	}
	return winio.ListenPipe(`\\.\pipe\`+address, &winio.PipeConfig{
		InputBufferSize:  64 * 1024,
		OutputBufferSize: 128 * 1024,
	})
}
