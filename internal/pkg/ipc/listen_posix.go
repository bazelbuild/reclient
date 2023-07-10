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
	"fmt"
	"net"
	"os"
	"strings"
)

// Listen announces on the serverAddr to accept grpc connection.
func Listen(serverAddr string) (net.Listener, error) {
	network := "tcp"
	address := serverAddr
	if strings.HasPrefix(address, "unix://") {
		network = "unix"
		address = strings.TrimPrefix(address, "unix://")
		if _, err := os.Stat(address); err == nil {
			if err := os.RemoveAll(address); err != nil {
				return nil, fmt.Errorf("failed to remove socket file %q: %v", address, err)
			}
		}
	}
	return net.Listen(network, address)
}
