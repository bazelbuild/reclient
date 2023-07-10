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
	"os/exec"
	"time"

	"team/foundry-x/re-client/internal/pkg/labels"
	"team/foundry-x/re-client/internal/pkg/localresources"
	"team/foundry-x/re-client/internal/pkg/logger"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/outerr"

	lpb "team/foundry-x/re-client/api/log"
)

type requirements struct {
	cpus   int64
	ramMBs int64
}

var (
	defaultReqs = requirements{1, 512}

	lblReqs = map[labels.Labels]requirements{
		labels.MetalavaLabels():  {8, 8192},
		labels.JavacLabels():     {8, 4096},
		labels.R8Labels():        {8, 4096},
		labels.D8Labels():        {8, 4096},
		labels.ClangLinkLabels(): {1, 8192},
	}
)

// Executor can run commands and retrieve their outputs.
type Executor interface {
	// ExecuteWithOutErr runs the given command inside the working directory.
	ExecuteWithOutErr(ctx context.Context, cmd *command.Command, oe outerr.OutErr) error
}

// LocalPool is responsible for executing commands locally.
type LocalPool struct {
	executor Executor
	resMgr   *localresources.Manager
}

// NewLocalPool creates a pool with the given args.
func NewLocalPool(exec Executor, resMgr *localresources.Manager) *LocalPool {
	return &LocalPool{
		executor: exec,
		resMgr:   resMgr,
	}
}

// Run runs a command locally. Returns the stdout, stderr, exit code, and error in case more
// information about the failure is needed.
func (l *LocalPool) Run(ctx, cCtx context.Context, cmd *command.Command, lbls map[string]string, oe outerr.OutErr, rec *logger.LogRecord) (int, error) {
	var req requirements
	var ok bool
	if req, ok = lblReqs[labels.FromMap(lbls)]; !ok {
		req = defaultReqs
	}
	if rec.GetLocalMetadata() == nil {
		rec.LocalMetadata = &lpb.LocalMetadata{}
	}

	qt := time.Now()
	release, err := l.resMgr.Lock(cCtx, req.cpus, req.ramMBs)
	et := rec.RecordEventTime(logger.EventLocalCommandQueued, qt)
	if err != nil {
		return 0, err
	}
	defer release()
	defer func() {
		rec.RecordEventTime(logger.EventLocalCommandExecution, et)
	}()
	if v := ctx.Value(testOnlyBlockLocalExecKey); v != nil {
		v.(func())()
	}
	err = l.executor.ExecuteWithOutErr(ctx, cmd, oe)
	exitCode := 0
	if exitErr, _ := err.(*exec.ExitError); exitErr != nil {
		exitCode = exitErr.ExitCode()
	}
	return exitCode, err
}
