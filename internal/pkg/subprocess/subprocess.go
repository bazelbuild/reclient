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

// Package subprocess provides functionality to execute system commands.
package subprocess

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/outerr"

	log "github.com/golang/glog"
)

// SystemExecutor uses the native os/exec package to execute subprocesses.
type SystemExecutor struct{}

// Execute runs the given command and returns stdout and stderr.
// Returns *exec.ExitError if the command ran with a non-zero exit code.
func (SystemExecutor) Execute(ctx context.Context, cmd *command.Command) (string, string, error) {
	cmdCtx, stdout, stderr, err := setupCommand(ctx, cmd)
	if err != nil {
		return "", "", err
	}
	err = cmdCtx.Run()
	if err != nil {
		log.V(2).Infof("Executed command %v\n >> stdout=%v\n >> stderr=%v\n >> err=%v", cmd.Args, stdout, stderr, err)
	}
	return stdout.String(), stderr.String(), err
}

// ExecuteWithOutErr runs the given command and returns stdout and stderr in an OutErr object.
// Returns *exec.ExitError if the command ran with a non-zero exit code.
func (SystemExecutor) ExecuteWithOutErr(ctx context.Context, cmd *command.Command, oe outerr.OutErr) error {
	cmdCtx, stdout, stderr, err := setupCommand(ctx, cmd)
	if err != nil {
		return err
	}
	if err = cmdCtx.Start(); err != nil {
		log.V(2).Infof("Starting command %v >> err=%v", cmd.Args, err)
		return err
	}
	wg := &sync.WaitGroup{}
	wg.Add(1)
	// Wait for the command to complete, whether to completion, or if cancelled.
	go func() {
		err = cmdCtx.Wait()
		if err != nil {
			log.V(2).Infof("Executed command %v\n >> err=%v", cmd.Args, err)
		}
		oe.WriteOut([]byte(stdout.String()))
		oe.WriteErr([]byte(stderr.String()))
		wg.Done()
	}()
	wg.Wait()
	if err != nil {
		log.V(2).Infof("Executed command %v\n >> stdout=%v\n >> stderr=%v\n >> err=%v", cmd.Args, stdout, stderr, err)
	}
	return err
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

func setupCommand(ctx context.Context, cmd *command.Command) (*exec.Cmd, *strings.Builder, *strings.Builder, error) {
	if len(cmd.Args) < 1 {
		return nil, nil, nil, fmt.Errorf("command must have more than 1 argument")
	}
	cmdCtx := exec.CommandContext(ctx, cmd.Args[0], cmd.Args[1:]...)
	cmdCtx.Dir = filepath.Join(cmd.ExecRoot, cmd.WorkingDir)
	if cmd.InputSpec != nil && cmd.InputSpec.EnvironmentVariables != nil {
		cmdCtx.Env = envVarList(cmd.InputSpec.EnvironmentVariables)
	}
	var stdout, stderr strings.Builder
	cmdCtx.Stdout = &stdout
	cmdCtx.Stderr = &stderr
	return cmdCtx, &stdout, &stderr, nil
}

func envVarList(envVars map[string]string) []string {
	lst := make([]string, 0, len(envVars))
	for k, v := range envVars {
		lst = append(lst, fmt.Sprintf("%s=%s", k, v))
	}
	return lst
}
