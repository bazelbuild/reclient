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
	"sync"
	"syscall"
	"unsafe"
)

// This function is copied over from an internal package in Go
// https://github.com/golang/go/blob/aa97a012b4be393c1725c16a78b92dea81632378/src/internal/syscall/windows/version_windows.go#L95
var supportUnixSocket = sync.OnceValue(func() bool {
	var size uint32
	// First call to get the required buffer size in bytes.
	// Ignore the error, it will always fail.
	_, _ = syscall.WSAEnumProtocols(nil, nil, &size)
	n := int32(size) / int32(unsafe.Sizeof(syscall.WSAProtocolInfo{}))
	// Second call to get the actual protocols.
	buf := make([]syscall.WSAProtocolInfo, n)
	n, err := syscall.WSAEnumProtocols(nil, &buf[0], &size)
	if err != nil {
		return false
	}
	for i := int32(0); i < n; i++ {
		if buf[i].AddressFamily == syscall.AF_UNIX {
			return true
		}
	}
	return false
})

var (
	GrpcCxxSupportsUDS = supportUnixSocket()
)
