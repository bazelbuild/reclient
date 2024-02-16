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

package cppcompile

import (
	"context"
	"fmt"

	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/clangparser"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/flags"
)

// ClangParser parses clang command args to produce a CommandFlags object.
type ClangParser struct{}

// ParseFlags is used to translate the given action command into clang compiler
// options, so that they can be used during input processing.
// Android build, throw error for unsupported flags.
func (cp ClangParser) ParseFlags(ctx context.Context, command []string, workingDir, execRoot string) (*flags.CommandFlags, error) {
	numArgs := len(command)
	if numArgs < 2 {
		return nil, fmt.Errorf("insufficient number of arguments in command: %v", command)
	}

	res := &flags.CommandFlags{
		ExecutablePath:   command[0],
		TargetFilePaths:  []string{},
		WorkingDirectory: workingDir,
		ExecRoot:         execRoot,
	}
	var state clangparser.State
	s := clangparser.New(command)
	defer state.Finalize(res)
	for s.HasNext() {
		if err := state.HandleClangFlags(s.ReadNextFlag(), res, false); err != nil {
			return res, err
		}
	}
	return res, nil
}
