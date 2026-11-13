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
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
)

var umaskMu sync.Mutex

// Listen announces on serverAddr to accept gRPC connections.
// Only unix:// addresses are supported on POSIX, and the socket permissions
// are restricted to 0600.
func Listen(serverAddr string) (net.Listener, error) {
	address, ok := strings.CutPrefix(serverAddr, "unix://")
	if !ok || address == "" {
		return nil, fmt.Errorf("invalid server address %q: only unix:///path addresses are supported", serverAddr)
	}

	if err := os.Remove(address); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("failed to remove existing socket %q: %w", address, err)
	}

	umaskMu.Lock()
	oldMask := syscall.Umask(0o177)
	l, err := net.Listen("unix", address)
	syscall.Umask(oldMask)
	umaskMu.Unlock()
	if err != nil {
		if len(address) >= 104 { // 104 bytes on macOS/BSD, 108 on Linux
			return nil, fmt.Errorf("socket path %q (%d bytes) likely exceeds OS Unix socket length limit; try a shorter path under /tmp: %w", address, len(address), err)
		}
		return nil, err
	}

	if err := os.Chmod(address, 0o600); err != nil {
		l.Close()
		return nil, fmt.Errorf("failed to chmod socket %q: %w", address, err)
	}
	return l, nil
}
