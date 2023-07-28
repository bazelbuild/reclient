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
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bazelbuild/reclient/internal/pkg/labels"
	"github.com/bazelbuild/reclient/internal/pkg/localresources"
	"github.com/bazelbuild/reclient/internal/pkg/logger"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/outerr"

	lpb "github.com/bazelbuild/reclient/api/log"
)

func TestLocalPoolMaxParallelism(t *testing.T) {
	t.Parallel()
	exec := &stubExecutor{
		stdout: "out",
		stderr: "err",
		err:    nil,
	}
	pool := &LocalPool{
		executor: exec,
		resMgr:   localresources.NewManager(5, 512*5),
	}
	ctx := context.Background()
	wg := sync.WaitGroup{}
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			oe := outerr.NewRecordingOutErr()
			exitCode, err := pool.Run(ctx, ctx, &command.Command{}, nil, oe, &logger.LogRecord{LogRecord: &lpb.LogRecord{}})
			stdout := string(oe.Stdout())
			stderr := string(oe.Stderr())
			if stdout != exec.stdout || stderr != exec.stderr || exitCode != 0 || err != exec.err {
				t.Errorf("Run() = %v,%v,%v,%v, want %v,%v,0,%v", stdout, stderr, exitCode, err, exec.stdout, exec.stderr, exec.err)
			}
		}()
	}
	wg.Wait()
	if exec.maxParallel > 5 {
		t.Errorf("Called Run() concurrently, got %v max concurrent executions, want 5", exec.maxParallel)
	}
}

func TestLocalPoolCancellation(t *testing.T) {
	t.Parallel()
	exec := &stubExecutor{
		err: nil,
	}
	pool := &LocalPool{
		executor: exec,
		resMgr:   localresources.NewManager(1, 512),
	}
	ctx := context.Background()
	rel, err := pool.resMgr.Lock(ctx, 1, 512)
	if err != nil {
		t.Fatalf("Lock() returned error: %v", err)
	}
	defer rel()
	cCtx, cancel := context.WithCancel(ctx)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		oe := outerr.NewRecordingOutErr()
		exitCode, err := pool.Run(ctx, cCtx, &command.Command{}, nil, oe, &logger.LogRecord{LogRecord: &lpb.LogRecord{}})
		if exitCode != 0 || err == nil || !errors.Is(err, context.Canceled) {
			t.Errorf("Run() = %v,%v, want context canceled error", exitCode, err)
		}
	}()
	time.Sleep(time.Second)
	cancel()
	wg.Wait()
	if exec.maxParallel != 0 {
		t.Errorf("Called Run(), wanted cancellation before it is called")
	}
}

func TestLocalPool_InsufficientSystemResources(t *testing.T) {
	t.Parallel()
	exec := &stubExecutor{
		stdout: "out",
		stderr: "err",
		err:    nil,
	}
	pool := &LocalPool{
		executor: exec,
		resMgr:   localresources.NewManager(1, 512),
	}
	ctx := context.Background()
	wg := sync.WaitGroup{}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			oe := outerr.NewRecordingOutErr()
			exitCode, err := pool.Run(ctx, ctx, &command.Command{}, labels.ToMap(labels.MetalavaLabels()), oe, &logger.LogRecord{LogRecord: &lpb.LogRecord{}})
			stdout := string(oe.Stdout())
			stderr := string(oe.Stderr())
			if stdout != exec.stdout || stderr != exec.stderr || exitCode != 0 || err != exec.err {
				t.Errorf("Run() = %v,%v,%v,%v, want %v,%v,0,%v", stdout, stderr, exitCode, err, exec.stdout, exec.stderr, exec.err)
			}
		}()
	}
	wg.Wait()
	if exec.maxParallel > 1 {
		t.Errorf("Called Run() concurrently, got %v max concurrent executions, want 1", exec.maxParallel)
	}
}
func TestLocalPoolHighMem(t *testing.T) {
	t.Parallel()
	exec := &stubExecutor{
		stdout: "out",
		stderr: "err",
		err:    nil,
	}
	pool := &LocalPool{
		executor: exec,
		resMgr:   localresources.NewManager(32, 16*1024),
	}
	ctx := context.Background()
	wg := sync.WaitGroup{}
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			oe := outerr.NewRecordingOutErr()
			exitCode, err := pool.Run(ctx, ctx, &command.Command{}, labels.ToMap(labels.MetalavaLabels()), oe, &logger.LogRecord{LogRecord: &lpb.LogRecord{}})
			stdout := string(oe.Stdout())
			stderr := string(oe.Stderr())
			if stdout != exec.stdout || stderr != exec.stderr || exitCode != 0 || err != exec.err {
				t.Errorf("Run() = %v,%v,%v,%v, want %v,%v,0,%v", stdout, stderr, exitCode, err, exec.stdout, exec.stderr, exec.err)
			}
		}()
	}
	wg.Wait()
	if exec.maxParallel > 2 {
		t.Errorf("Called Run() concurrently, got %v max concurrent executions, want 2", exec.maxParallel)
	}
}

func TestLocalPoolMixed(t *testing.T) {
	t.Parallel()
	exec := &stubExecutor{
		stdout:   "out",
		stderr:   "err",
		err:      nil,
		chStart:  make(chan bool),
		chFinish: make(chan bool),
	}
	pool := &LocalPool{
		executor: exec,
		resMgr:   localresources.NewManager(24, 16*1024+8*512),
	}
	ctx := context.Background()
	wg := sync.WaitGroup{}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var lbls labels.Labels
			if i < 2 {
				lbls = labels.MetalavaLabels()
			} else {
				lbls = labels.ClangCppLabels()
			}
			oe := outerr.NewRecordingOutErr()
			exitCode, err := pool.Run(ctx, ctx, &command.Command{}, labels.ToMap(lbls), oe, &logger.LogRecord{LogRecord: &lpb.LogRecord{}})
			stdout := string(oe.Stdout())
			stderr := string(oe.Stderr())
			if stdout != exec.stdout || stderr != exec.stderr || exitCode != 0 || err != exec.err {
				t.Errorf("Run() = %v,%v,%v,%v, want %v,%v,0,%v", stdout, stderr, exitCode, err, exec.stdout, exec.stderr, exec.err)
			}
		}()
		<-exec.chStart
	}
	if exec.maxParallel != 10 {
		t.Errorf("Called Run() concurrently, got %v max concurrent executions, want 10", exec.maxParallel)
	}
	// Add another command that should be blocked because the pool is full.
	wg.Add(1)
	go func() {
		defer wg.Done()
		oe := outerr.NewRecordingOutErr()
		exitCode, err := pool.Run(ctx, ctx, &command.Command{}, labels.ToMap(labels.ClangCppLabels()), oe, &logger.LogRecord{LogRecord: &lpb.LogRecord{}})
		stdout := string(oe.Stdout())
		stderr := string(oe.Stderr())
		if stdout != exec.stdout || stderr != exec.stderr || exitCode != 0 || err != exec.err {
			t.Errorf("Run() = %v,%v,%v,%v, want %v,%v,0,%v", stdout, stderr, exitCode, err, exec.stdout, exec.stderr, exec.err)
		}
	}()
	select {
	case <-exec.chStart:
		t.Errorf("Pool allowed too many actions to run concurrently")
	case <-time.After(5 * time.Second):
		break
	}
	for i := 0; i < 10; i++ {
		exec.chFinish <- true
	}
	<-exec.chStart
	exec.chFinish <- true
	wg.Wait()
}

// TestLocalPoolEventTimes tests that the EventTimes "LocalCommandQueued" and "LocalCommandExecution" are properly set when running a command.
// This was based on TestLocalPoolMaxParallelism and modified to check for changes to the EventTimes.
func TestLocalPoolEventTimes(t *testing.T) {
	t.Parallel()
	exec := &stubExecutor{
		stdout: "out",
		stderr: "err",
		err:    nil,
	}
	pool := &LocalPool{
		executor: exec,
		resMgr:   localresources.NewManager(5, 512*5),
	}
	ctx := context.Background()
	wg := sync.WaitGroup{}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			oe := outerr.NewRecordingOutErr()
			rec := &logger.LogRecord{LogRecord: &lpb.LogRecord{}}
			pool.Run(ctx, ctx, &command.Command{}, nil, oe, rec)
			tiLCQ, okLCQ := rec.LocalMetadata.EventTimes["LocalCommandQueued"]
			tiLCE, okLCE := rec.LocalMetadata.EventTimes["LocalCommandExecution"]
			if !okLCQ {
				t.Fatalf("EventTimes for %s  were not logged", "LocalCommandQueued")
			}
			if !okLCE {
				t.Fatalf("EventTimes for %s were not logged", "LocalCommandExecution")
			}
			tiLCQTo := command.TimeFromProto(tiLCQ.To)
			tiLCEFrom := command.TimeFromProto(tiLCE.From)
			if tiLCQTo != tiLCEFrom {
				t.Errorf("To time of %s, does not match From time of %s", "LocalCommandQueued", "LocalCommandExecution")
			}
		}()
	}
	wg.Wait()
}

type stubExecutor struct {
	numParallel int64
	maxParallel int64
	mu          sync.Mutex
	chStart     chan bool
	chFinish    chan bool
	stdout      string
	stderr      string
	err         error
}

func (s *stubExecutor) ExecuteWithOutErr(ctx context.Context, cmd *command.Command, oe outerr.OutErr) error {
	s.mu.Lock()
	s.numParallel++
	if s.numParallel > s.maxParallel {
		s.maxParallel = s.numParallel
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.numParallel--
	}()
	if s.chStart == nil || s.chFinish == nil {
		time.Sleep(time.Millisecond * 10)
	} else {
		s.chStart <- true
		<-s.chFinish
	}
	oe.WriteOut([]byte(s.stdout))
	oe.WriteErr([]byte(s.stderr))
	return s.err
}
