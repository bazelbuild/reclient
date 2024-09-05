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

import (
	"context"
	"os"
	"os/exec"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/outerr"

	log "github.com/golang/glog"

	winjob "github.com/kolesnikovae/go-winjob"
)

var (
	jobObj *winjob.JobObject
)

// Subprocess setup on Windows creates a JobObject for keeping track of child processes
// created by reproxy.  A cleanup function to close the job object is returned.
func Setup() (func(), error) {
	var err error
	// Create the job object configured to kill any remaining unfinished processes
	// when it is closed.
	jobObj, err = winjob.Create("", winjob.WithKillOnJobClose())
	if err != nil {
		return nil, err
	}
	// Cleanup function closes the job object, which should terminate all remaining processes
	// that have not yet finished.
	return func() {
		if err := jobObj.Close(); err != nil {
			log.Errorf("Failure closing job object: %v", err)
		}
	}, nil
}

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

// ExecuteInBackground executes the command in the background. Command result is written to the
// passed channel.  For Windows, the child process will be added to a Job for tracking and guaranteed
// shutdown on reproxy close.
func (SystemExecutor) ExecuteInBackground(ctx context.Context, cmd *command.Command, oe outerr.OutErr, ch chan *command.Result) error {
	cmdCtx, stdout, stderr, err := setupCommand(ctx, cmd)
	if err != nil {
		return err
	}

	if err = cmdCtx.Start(); err != nil {
		log.V(2).Infof("Starting command %v >> err=%v", cmd.Args, err)
		return err
	}

	if jobObj != nil {
		if err = jobObj.Assign(cmdCtx.Process); err != nil {
			log.Warningf("Failed to assign process %v (%v) to Job Object", cmd.Args[0], cmdCtx.Process)
		}
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
