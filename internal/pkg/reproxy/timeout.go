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

package reproxy

import (
	"context"
	"os"
	"time"

	"github.com/bazelbuild/reclient/internal/pkg/interceptors"

	log "github.com/golang/glog"
)

// IdleTimeout checks the latest request received timestamp and sends a SIGINT signal
// if last request received is greater than the expected idle timeout.
func IdleTimeout(ctx context.Context, timeout time.Duration) {
	if timeout == 0 {
		log.Infof("No reproxy idle timeout set.")
		return
	}

	log.Infof("Idle timeout set to %v", timeout)
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(interceptors.LatestRequestTimestamp().Add(timeout))):
			if time.Since(interceptors.LatestRequestTimestamp()) >= timeout {
				log.Infof("idle timeout reached, exiting...")
				pid := os.Getpid()
				p, err := os.FindProcess(pid)
				if err != nil {
					log.Warningf("unable to find process with pid %v, graceful exit failed: %v", pid, err)
					os.Exit(0)
				}
				if err := p.Signal(os.Interrupt); err != nil {
					log.Warningf("unable to send SIGINT to process %+v, graceful exit failed: %v", p, err)
					os.Exit(0)
				}
			}
		}
	}
}
