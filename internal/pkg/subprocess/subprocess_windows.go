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

package subprocess

import "os"

// Exists returns true if a pid is assigned to a process that is actively running.
// In the windows case, a call to FindProcess should be sufficient, as it will return an
// error if there is no process assigned to that pid.
func Exists(pid int) (bool, error) {
	p, err := os.FindProcess(pid)
	if err != nil || p == nil {
		return false, nil
	}
	p.Release()
	return true, nil
}
