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

package reproxypid

import (
	"os"
	"syscall"
)

// exists returns true if a pid is assigned to a process that is actively running.
// Based on comment from: https://github.com/golang/go/issues/34396
func exists(pid int) (bool, error) {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, err
	}
	// If sig is 0, then no signal is sent, but existence and permission
	// checks are still performed; this can be used to check for the
	// existence of a process ID or process group ID that the caller is
	// permitted to signal. As per https://man7.org/linux/man-pages/man2/kill.2.html
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true, nil
	}
	return false, nil
}
