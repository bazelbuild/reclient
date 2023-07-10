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

package bootstrap

import (
	"os"

	log "github.com/golang/glog"
)

// Sigint sends a SIGINT signal to the process with a given pid
func Sigint(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		log.Warningf("Failed to find process %v: %v", pid, err)
		return err
	}
	if err := process.Signal(os.Interrupt); err != nil {
		log.Warningf("Failed to kill process %v: %v", pid, err)
		return err
	}
	return nil
}

// Doesn't do anything. We need this to match Windows function definitions
// as sigint implementation on Windows is not always reliable
func terminate(pid int) error {
	return nil
}
