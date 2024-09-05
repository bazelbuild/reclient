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

package subprocess

import (
	"context"
	"os"
	"os/exec"
	"syscall"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/outerr"

	log "github.com/golang/glog"
)

// Setup any necessary one-time things for subprocess management.  On linux/mac,
// this is a no-op and returns a no-op cleanup function.
func Setup() (func(), error) {
	// No-op on unix
	return func() {}, nil
}

// Exists returns true if a pid is assigned to a process that is actively running.
// Based on comment from: https://github.com/golang/go/issues/34396
func Exists(pid int) (bool, error) {
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

// ExecuteInBackground executes the command in the background. Command result is written to the
// passed channel.
func (SystemExecutor) ExecuteInBackground(ctx context.Context, cmd *command.Command, oe outerr.OutErr, ch chan *command.Result) error {
	cmdCtx, stdout, stderr, err := setupCommand(ctx, cmd)
	if err != nil {
		return err
	}
	if err = cmdCtx.Start(); err != nil {
		log.V(2).Infof("Starting command %v >> err=%v", cmd.Args, err)
		return err
	}
	go func() {
		err := cmdCtx.Wait()
		if err != nil {
			log.V(2).Infof("Executed command %v\n >> err=%v", cmd.Args, err)
		}
		oe.WriteOut([]byte(stdout.String()))
		oe.WriteErr([]byte(stderr.String()))
		exitCode := 0
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr != nil {
			exitCode = exitErr.ExitCode()
		}
		res := command.NewResultFromExitCode(exitCode)
		if exitCode == 0 && err != nil {
			res = command.NewLocalErrorResult(err)
		}
		ch <- res
	}()
	return nil
}
