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

package subprocess

import (
	"context"
	"strings"
	"testing"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/outerr"
)

func TestExecute(t *testing.T) {
	want := "Hello"
	cmd := &command.Command{Args: []string{"echo", want}}
	got, _, err := SystemExecutor{}.Execute(context.Background(), cmd)
	if err != nil {
		t.Errorf("Execute(%v) failed with error: %v", cmd, err)
	}
	got = strings.TrimSpace(got)
	if got != want {
		t.Errorf("Execute(%v) stdout = %v, want %q", cmd, got, want)
	}
}

func TestExecuteWithOutErr(t *testing.T) {
	oe := outerr.NewRecordingOutErr()
	want := "Hello"
	cmd := &command.Command{Args: []string{"echo", want}}
	err := SystemExecutor{}.ExecuteWithOutErr(context.Background(), cmd, oe)
	if err != nil {
		t.Errorf("ExecuteWithOutErr(%v) failed with error: %v", cmd, err)
	}

	got := strings.TrimSpace(string(oe.Stdout()))
	if got != want {
		t.Errorf("ExecuteWithOutErr(%v) stdout = %v, want %q", cmd, string(oe.Stdout()), want)
	}
}

func TestExecuteInBackground(t *testing.T) {
	oe := outerr.NewRecordingOutErr()
	want := "Hello"
	cmd := &command.Command{Args: []string{"bash", "-c", "sleep 1 && echo " + want}}
	ch := make(chan *command.Result)
	err := SystemExecutor{}.ExecuteInBackground(context.Background(), cmd, oe, ch)
	if err != nil {
		t.Errorf("ExecuteInBackground(%v) failed with error: %v", cmd, err)
	}
	res := <-ch
	if !res.IsOk() {
		t.Errorf("ExecuteInBackground(%v) failed with error: %v", cmd, res)
	}

	got := strings.TrimSpace(string(oe.Stdout()))
	if got != want {
		t.Errorf("ExecuteInBackground(%v) stdout = %v, want %q", cmd, got, want)
	}
}

func TestExecuteInBackgroundCancellation(t *testing.T) {
	oe := outerr.NewRecordingOutErr()
	cmd := &command.Command{Args: []string{"bash", "-c", "sleep 2 && echo Hello"}}
	ch := make(chan *command.Result)
	ctx, cancel := context.WithCancel(context.Background())
	err := SystemExecutor{}.ExecuteInBackground(ctx, cmd, oe, ch)
	if err != nil {
		t.Errorf("ExecuteInBackground(%v) failed with error: %v", cmd, err)
	}
	cancel()
	res := <-ch
	if res.IsOk() {
		t.Errorf("ExecuteInBackground(%v) succeeded, want error", cmd)
	}

	got := strings.TrimSpace(string(oe.Stdout()))
	want := ""
	if got != want {
		t.Errorf("ExecuteInBackground(%v) stdout = %v, want %q", cmd, got, want)
	}
}
