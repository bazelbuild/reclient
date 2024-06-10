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
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

func TestFile(t *testing.T) {
	td := t.TempDir()
	tmpDir = td
	t.Cleanup(func() {
		tmpDir = os.TempDir()
	})
	tests := []struct {
		name       string
		serverAddr string
		pid        int
	}{
		{
			name:       "UnixSocket",
			serverAddr: fmt.Sprintf("unix://%s/somesocket.sock", td),
			pid:        1234567,
		},
		{
			name:       "WindowsPipe",
			serverAddr: "pipe://pipename.pipe",
			pid:        2345678,
		},
		{
			name:       "TCP",
			serverAddr: "127.0.0.1:8000",
			pid:        3456789,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := WriteFile(tc.serverAddr, tc.pid)
			if err != nil {
				t.Errorf("WriteFile(%v,%v) returned unexpected error: %v", tc.serverAddr, tc.pid, err)
			}
			pf, err := ReadFile(tc.serverAddr)
			if err != nil {
				t.Errorf("ReadFile(%v) returned unexpected error: %v", tc.serverAddr, err)
			}
			if pf.Pid != tc.pid {
				t.Errorf("ReadFile(%v) returned wrong PID, wanted %v, got %v", tc.serverAddr, tc.pid, pf.Pid)
			}
		})
	}
}

func TestPollForDeath(t *testing.T) {
	td := t.TempDir()
	tmpDir = td
	t.Cleanup(func() {
		tmpDir = os.TempDir()
	})
	serverAddr := fmt.Sprintf("unix://%s/somesocket.sock", td)
	var args []string
	switch runtime.GOOS {
	case "windows":
		args = append(args,
			"cmd.exe",
			"/C",
			"ping -n 5 127.0.0.1",
		)
	default:
		args = append(args,
			"/bin/bash",
			"-c",
			"sleep 5",
		)
	}
	cmd := exec.Command(args[0], args[1:]...)
	err := cmd.Start()
	if err != nil {
		t.Fatalf("Error staring subprocess: %v", err)
	}
	pid := cmd.Process.Pid
	go func() {
		cmd.Wait()
	}()
	err = WriteFile(serverAddr, pid)
	if err != nil {
		t.Errorf("WriteFile(%v,%v) returned unexpected error: %v", serverAddr, pid, err)
	}
	pf, err := ReadFile(serverAddr)
	if err != nil {
		t.Errorf("ReadFile(%v) returned unexpected error: %v", serverAddr, err)
	}
	deadCh := make(chan error)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	go pf.PollForDeath(ctx, 50*time.Millisecond, deadCh)
	select {
	case err := <-deadCh:
		if err != nil {
			t.Errorf("PollForDeath returned an unexpected error: %v", err)
		}
	case <-ctx.Done():
		t.Errorf("Timed out after 10s waiting for process %d to die", pid)
	}
}
