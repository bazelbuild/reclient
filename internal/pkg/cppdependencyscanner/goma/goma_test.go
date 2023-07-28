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

package includescanner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bazelbuild/reclient/internal/pkg/logger"
	"github.com/bazelbuild/reclient/internal/pkg/stats"

	"github.com/google/go-cmp/cmp"
)

const execID = "ExecID"

type gomaCrashImpl struct {
	res            processInputsResult
	delay          time.Duration
	mu             sync.Mutex
	closeCnt       int
	processCnt     int
	processDoneCnt int
}

func (g *gomaCrashImpl) close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closeCnt++
}

func (g *gomaCrashImpl) processInputs(req *processInputsRequest) error {
	g.mu.Lock()
	g.processCnt++
	g.mu.Unlock()
	go func() {
		select {
		case <-time.After(g.delay):
			req.res <- g.res
			g.mu.Lock()
			g.processDoneCnt++
			defer g.mu.Unlock()
			return
		}
	}()
	return nil
}

func TestGoma(t *testing.T) {
	l, _ := newLogger(t)
	compileCommand := []string{"test", "command"}
	filename := "name.cpp"
	directory := "/this/is/a/dir"
	cmdEnv := []string{"ENV=test"}
	res := processInputsResult{
		res:       []string{"this", "is", "correct"},
		usedCache: true,
		err:       nil,
	}
	impl := &gomaCrashImpl{
		res:   res,
		delay: 100 * time.Millisecond,
	}

	ctx := context.Background()
	ds := &DepsScanner{
		cacheDir:       "",
		logDir:         "",
		cacheFileMaxMb: 10,
		implNewFn: func(_, _ string, _ int, _ bool) gomaImpl {
			return impl
		},
		l: l,
	}
	ds.init()
	time.Sleep(time.Second * 5)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	ch := make(chan processInputsResult)
	defer close(ch)
	t.Cleanup(cancel)

	go func() {
		res, usedCache, err := ds.ProcessInputs(ctx, execID, compileCommand, filename, directory, cmdEnv)
		ch <- processInputsResult{
			res, usedCache, err,
		}
	}()
	var got processInputsResult
	select {
	case <-ctx.Done():
		t.Fatalf("Test timed out, possible deadlock")
	case res := <-ch:
		got = res
	}

	l.CloseAndAggregate()

	if diff := cmp.Diff(res, got, cmp.AllowUnexported(processInputsResult{})); diff != "" {
		t.Errorf("ProcessInputs() returned diff in result: (-want +got)\n%s", diff)
	}
}

func TestGomaTimeout(t *testing.T) {
	l, _ := newLogger(t)
	compileCommand := []string{"test", "command"}
	filename := "name.cpp"
	directory := "/this/is/a/dir"
	cmdEnv := []string{"ENV=test"}
	res := processInputsResult{
		res:       []string{"this", "is", "correct"},
		usedCache: true,
		err:       nil,
	}
	impl := &gomaCrashImpl{
		res:   res,
		delay: 10 * time.Second,
	}

	ctx := context.Background()
	ds := &DepsScanner{
		cacheDir:       "",
		logDir:         "",
		cacheFileMaxMb: 10,
		implNewFn: func(_, _ string, _ int, _ bool) gomaImpl {
			return impl
		},
		l: l,
	}
	ds.init()
	time.Sleep(time.Second * 5)

	testCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	dsCtx, _ := context.WithTimeout(ctx, time.Second)
	ch := make(chan processInputsResult)
	defer close(ch)
	t.Cleanup(cancel)

	go func() {
		res, usedCache, err := ds.ProcessInputs(dsCtx, execID, compileCommand, filename, directory, cmdEnv)
		ch <- processInputsResult{
			res, usedCache, err,
		}
	}()
	var got processInputsResult
	select {
	case <-testCtx.Done():
		t.Fatalf("Test timed out, possible deadlock")
	case res := <-ch:
		got = res
	}

	if got.err == nil {
		t.Errorf("Expected error got res=%v, usedCache=%v", got.res, got.usedCache)
	}

	if !errors.Is(got.err, context.DeadlineExceeded) {
		t.Errorf("Expected %q', got %q", context.DeadlineExceeded, got.err)
	}

	l.CloseAndAggregate()
}

func newLogger(t *testing.T) (*logger.Logger, string) {
	t.Helper()
	execRoot := t.TempDir()
	l, err := logger.New(logger.TextFormat, execRoot, Name, stats.New(), nil, nil)
	if err != nil {
		t.Fatalf("failed to initialize logger: %v", err)
	}
	return l, execRoot
}
