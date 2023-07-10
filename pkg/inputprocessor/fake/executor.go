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

// Package fake defines fakes to be used for testing integration with the input processor.
package fake

import (
	"context"
	"fmt"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
)

// ClangExecutor is a fake executor used to replace actual execution of clang in the process of
// determining inputs.
type ClangExecutor struct {
	InputFiles     []string
	ToolchainFiles []string
	Err            error
}

// Execute fakes execution of a command and returns outputs predefined in the ClangExecutor.
func (e *ClangExecutor) Execute(ctx context.Context, cmd *command.Command) (string, string, error) {
	for _, arg := range cmd.Args {
		if arg == "-M" {
			return e.clangInputFiles(), "", e.Err
		} else if arg == "strace" {
			return e.clangStraceFiles(), "", e.Err
		}
	}
	return "", "", e.Err
}

func (e *ClangExecutor) clangInputFiles() string {
	out := ""
	seps := []string{"\n", " ", ":", "\r"}
	idx := 0
	for _, i := range e.InputFiles {
		out += i + seps[idx]
		idx = (idx + 1) % len(seps)
	}
	return out
}

func (e *ClangExecutor) clangStraceFiles() string {
	out := ""
	pfx := []string{"\n", " ", ":", "\r"}
	idx := 0
	for _, i := range e.ToolchainFiles {
		out += fmt.Sprintf("%v%q, \"O_RDONLY|O_CLOEXEC\") = 0", pfx[idx], i)
		idx = (idx + 1) % len(pfx)
	}
	return out
}
