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
	"context"
	"net"
	"strings"
	"syscall"
	"unsafe"

	winio "github.com/Microsoft/go-winio"
	log "github.com/golang/glog"
	"google.golang.org/grpc"
)

const (
	pipePrefix   = `\\.\pipe\`
	pipeProtocol = "pipe://"
)

var (
	kernel32                    = syscall.NewLazyDLL("kernel32.dll")
	getNamedPipeServerProcessID = kernel32.NewProc("GetNamedPipeServerProcessId")
)

// DialContext connects to the serverAddress for grpc.
// if serverAddr is `pipe://<addr>`, it connects to named pipe (`\\.\\pipe\<addr>`).
func DialContext(ctx context.Context, serverAddr string) (*grpc.ClientConn, error) {
	if strings.HasPrefix(serverAddr, pipeProtocol) {
		return dialPipe(ctx, strings.TrimPrefix(serverAddr, pipeProtocol))
	}
	return grpc.DialContext(ctx, serverAddr, grpc.WithInsecure(), grpc.WithBlock())
}

func dialPipe(ctx context.Context, pipe string) (*grpc.ClientConn, error) {
	addr := pipePrefix + pipe
	return grpc.DialContext(ctx, addr, grpc.WithInsecure(), grpc.WithBlock(), grpc.WithMaxMsgSize(GrpcMaxMsgSize),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return winio.DialPipeContext(ctx, addr)
		}))
}

// DialAllContexts searches for and connects to all reproxy.exe pipes
func DialAllContexts(ctx context.Context) (map[string]*grpc.ClientConn, error) {
	pidToName, err := getProcessNameMap()
	if err != nil {
		return nil, err
	}
	conns := make(map[string]*grpc.ClientConn)
	for _, pipe := range filterReproxyPipes(allPipes(), pidToName) {
		if conn, err := dialPipe(ctx, pipe); err != nil {
			log.Warningf("Error connecting to %s%s: %s", pipePrefix, pipe, err)
		} else {
			conns[pipeProtocol+pipe] = conn
		}
	}
	return conns, nil
}

func filterReproxyPipes(pipes []string, pidToName map[uint32]string) []string {
	var out []string
	for _, pipe := range pipes {
		if pid, err := getServerPIDForPipe(pipe); err == nil && pidToName[pid] == "reproxy.exe" {
			out = append(out, pipe)
		}
	}
	return out
}

func allPipes() []string {
	var out []string
	var data syscall.Win32finddata
	h, err := syscall.FindFirstFile(
		syscall.StringToUTF16Ptr(pipePrefix+"*"),
		&data,
	)
	if err != nil {
		return out
	}
	defer syscall.FindClose(h)
	for {
		out = append(out, syscall.UTF16ToString(data.FileName[:]))
		if err := syscall.FindNextFile(h, &data); err != nil {
			return out
		}
	}
}

func getServerPIDForPipe(pipe string) (uint32, error) {
	var (
		pid     uint32
		fHandle syscall.Handle
	)
	fullName, err := syscall.UTF16PtrFromString(pipePrefix + pipe)
	if err != nil {
		return 0, err
	}
	fHandle, err = syscall.CreateFile(fullName, syscall.GENERIC_READ, 0, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		return 0, err
	}
	defer syscall.CloseHandle(fHandle)
	getNamedPipeServerProcessID.Call(uintptr(fHandle), uintptr(unsafe.Pointer(&pid)))
	return pid, err
}

func getProcessNameMap() (map[uint32]string, error) {
	snapshot, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer syscall.CloseHandle(snapshot)
	var procEntry syscall.ProcessEntry32
	procEntry.Size = uint32(unsafe.Sizeof(procEntry))
	if err = syscall.Process32First(snapshot, &procEntry); err != nil {
		return nil, err
	}
	processNameMap := make(map[uint32]string)
	for {
		processNameMap[procEntry.ProcessID] = syscall.UTF16ToString(procEntry.ExeFile[:])
		if err = syscall.Process32Next(snapshot, &procEntry); err != nil {
			if err == syscall.ERROR_NO_MORE_FILES {
				return processNameMap, nil
			}
			return nil, err
		}
	}
}
