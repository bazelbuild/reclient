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

package reproxypid

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	log "github.com/golang/glog"
)

const (
	// Proxyname is the name of the RE proxy process.
	Proxyname = "reproxy"
)

var (
	tmpDir = os.TempDir()
)

// File represents a file that stores the pid of a running reproxy process.
type File struct {
	Pid int
	fp  string
}

// WriteFile writes the pid file for the reproxy process running at serverAddr.
func WriteFile(serverAddr string, pid int) error {
	fp, err := pathForServerAddr(serverAddr)
	if err != nil {
		return err
	}
	if err := os.WriteFile(fp, []byte(strconv.Itoa(pid)), 0644); err != nil {
		return fmt.Errorf("failed to persist the pid file %v: %w", fp, err)
	}
	log.Infof("Wrote PID file %v with contents: %v", fp, pid)
	return nil
}

// ReadFile reads the pid file for the reproxy process running at serverAddr.
func ReadFile(serverAddr string) (*File, error) {
	fp, err := pathForServerAddr(serverAddr)
	contents, err := os.ReadFile(fp)
	if err != nil {
		return nil, fmt.Errorf("failed to read pid file: %w", err)
	}
	pid, err := strconv.Atoi(string(contents))
	if err != nil {
		return nil, fmt.Errorf("cannot parse pid from contents of %v (%v): %w", fp, string(contents), err)
	}
	return &File{
		Pid: pid,
		fp:  fp,
	}, nil
}

// Delete this pid file.
// Supresses and logs errors as it is ok if the file as already been cleaned up.
func (f *File) Delete() {
	if f == nil {
		return
	}
	err := os.Remove(f.fp)
	if err != nil {
		log.Infof("Failed to delete pid file %s, this is normal if reproxy has already shutdown.", f.fp)
	}
}

// IsAlive retuns true if the reproxy process is still running.
func (f *File) IsAlive() (bool, error) {
	return exists(f.Pid)
}

func pathForServerAddr(serverAddr string) (string, error) {
	address := ""
	if strings.HasPrefix(serverAddr, "unix://") {
		address = strings.TrimPrefix(serverAddr, "unix://") + ".pid"
	} else {
		_, port, err := net.SplitHostPort(serverAddr)
		if err != nil {
			return "", err
		}
		address = filepath.Join(tmpDir, Proxyname+"_"+port+".pid")
		err = os.MkdirAll(filepath.Dir(address), 0755)
		if err != nil {
			return "", fmt.Errorf("failed to create dir for pid file %q: %w", address, err)
		}
	}
	return address, nil
}

// PollForDeath will poll pid from f every pollDelay and send nil to deadCh when
// that pid is no longer alive, the pid file will then be deleted.
// If an error is encountered in checking the process then it will be send to
// deadCh and this function will end and the pid file will not be deleted.
// Meant to be called in a go routine.
func (f *File) PollForDeath(ctx context.Context, pollDelay time.Duration, deadCh chan error) {
	for {
		alive, err := f.IsAlive()
		if err != nil {
			deadCh <- err
			return
		}
		if !alive {
			f.Delete() // Reproxy should have deleted this already, but if it crashed then the pid file could be left behind
			deadCh <- nil
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(pollDelay):
		}
	}
}
