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

package localresources

import (
	"syscall"

	log "github.com/golang/glog"
)

// TotalRAMMBs returns the amount of system memory in MegaBytes.
func TotalRAMMBs() int64 {
	var info syscall.Sysinfo_t
	err := syscall.Sysinfo(&info)
	if err != nil {
		log.Errorf("Failed to get system memory size: %v", err)
		return 0
	}
	return int64(uint64(info.Totalram) * uint64(info.Unit) / 1024 / 1024)
}
