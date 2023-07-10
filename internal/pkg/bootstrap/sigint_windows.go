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

package bootstrap

import (
	"syscall"

	"golang.org/x/sys/windows"

	log "github.com/golang/glog"
)

// Sigint sends a SIGINT signal to the process with a given pid
// Sending SIGINT on Windows isn't implemented by golang and not generally
// as simple as on Unix. Particularlly, since golang doesn't implement it,
// we have to use Windows syscalls directly.
//
// Windows only allows SIGINTs (or, in Windows terms, a CTRL-C event) to be
// broadcast to processes within the same process group, so we have to first
// attach bootstrap's "console" to reproxy's "console", and then make it
// so bootstrap ignores CLTR-C, before we can broadcast a CTRL-C event to the
// entire process group.
func Sigint(pid int) error {
	dll := windows.NewLazySystemDLL("kernel32.dll")

	attachConsole := dll.NewProc("AttachConsole")
	if r1, _, err := attachConsole.Call(uintptr(pid)); r1 == 0 && err != syscall.ERROR_ACCESS_DENIED {
		log.Warningf("Failed to attach to process %v's console: %v", pid, err)
		return err
	}

	setConsoleCtrlHandler := dll.NewProc("SetConsoleCtrlHandler")
	if r1, _, err := setConsoleCtrlHandler.Call(0, 1 /* true */); r1 == 0 {
		log.Warningf("Failed to set caller process' to ignore CTRL-C events: %v", err)
		return err
	}

	if err := windows.GenerateConsoleCtrlEvent(windows.CTRL_C_EVENT, 0); err != nil {
		log.Warningf("Failed to send Windows CTRL-C to %v: %v", pid, err)
		return err
	}
	return nil
}

// Terminates the process with a given process id.
// The function also stops all the threads of the specified process and
// requests cancellation of all pending I/O
func terminate(pid int) error {
	dll := windows.NewLazySystemDLL("kernel32.dll")
	openProcess := dll.NewProc("OpenProcess")
	var handle uintptr
	var err error
	if handle, _, err = openProcess.Call(windows.PROCESS_TERMINATE, 0, uintptr(pid)); handle == 0 {
		log.Warningf("Failed to open process %v: %v", pid, err)
		return err
	}
	terminateProcess := dll.NewProc("TerminateProcess")
	if r1, _, err := terminateProcess.Call(handle, 0); r1 == 0 {
		log.Warningf("Failed to terminate process %v: %v", pid, err)
	}
	closeHandle := dll.NewProc("CloseHandle")
	if r1, _, err := closeHandle.Call(handle); r1 == 0 {
		log.Warningf("Failed to close handle for process %v: %v", pid, err)
		return err
	}
	return nil
}
