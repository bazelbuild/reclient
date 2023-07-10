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

package clangcl

import (
	"context"
	"fmt"

	"team/foundry-x/re-client/internal/pkg/inputprocessor/args"
	"team/foundry-x/re-client/internal/pkg/inputprocessor/clangparser"
	"team/foundry-x/re-client/internal/pkg/inputprocessor/flags"
)

const clangOption = "-clang:"

// parseFlags is used to translate the given action command into
// clang-cl compiler options, so that they can be used during
// input processing.
func parseFlags(ctx context.Context, command []string, workingDir, execRoot string) (*flags.CommandFlags, error) {
	numArgs := len(command)
	if numArgs < 2 {
		return nil, fmt.Errorf("insufficient number of arguments in command: %v", command)
	}

	res := &flags.CommandFlags{
		ExecutablePath:   command[0],
		WorkingDirectory: workingDir,
		ExecRoot:         execRoot,
	}

	var state clangCLState
	var prev *args.NextResult
	s := clangparser.New(command)
	for s.HasNext() {
		curr := s.NextResult()
		state.handleClangCLFlags(prev, curr, res)
		prev = curr
	}
	state.Finalize(res)
	return res, nil
}

type clangCLState struct {
	clangparser.State
}

func (s *clangCLState) handleClangCLFlags(prev, curr *args.NextResult, f *flags.CommandFlags) error {
	if prev != nil && prev.NormalizedKey == clangOption {
		if prev.Values[0] == "-MF" && curr.NormalizedKey == clangOption {
			f.Flags = f.Flags[:len(f.Flags)-1] // pop the last flag because we handle it here
			f.OutputFilePaths = append(f.OutputFilePaths, curr.Values[0])
			f.EmittedDependencyFile = curr.Values[0]
			return nil
		}
	}
	nextRes := curr
	switch nextRes.NormalizedKey {
	case "-Fo":
		f.OutputFilePaths = append(f.OutputFilePaths, nextRes.Values[0])
		return nil
	}
	return s.State.HandleClangFlags(nextRes, f, false)
}
