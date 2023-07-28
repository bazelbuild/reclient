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

// Package flagsparser provides functionalities to understand command line flags associated with
// various action types so that they can be used during input processing.
package flagsparser

import (
	"context"
	"errors"

	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/args"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/flags"
)

// InputProcessor is a basic input processor that retrieves an InputSpec based on the parsed flags.
type InputProcessor struct {
	// OutputDirDep indicates that the action relies on the output directory to already
	// exist.
	OutputDirDep bool
}

// CommandFlags converts the given command into the CommandFlags representation of the command args.
func CommandFlags(ctx context.Context, command []string, workingDir, execRoot string) (*flags.CommandFlags, error) {
	if len(command) == 0 {
		return nil, errors.New("no command passed")
	}
	res := &flags.CommandFlags{
		ExecutablePath:   command[0],
		WorkingDirectory: workingDir,
		ExecRoot:         execRoot,
	}

	s := args.Scanner{
		Args: command[1:len(command)],
	}
	for s.HasNext() {
		flag, _, values, _ := s.Next()
		res.Flags = append(res.Flags, &flags.Flag{Value: flag})
		for _, value := range values {
			res.Flags = append(res.Flags, &flags.Flag{Value: value})
		}
	}
	return res, nil
}
