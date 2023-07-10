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

package headerabi

import (
	"context"
	"fmt"

	"team/foundry-x/re-client/internal/pkg/inputprocessor/args"
	"team/foundry-x/re-client/internal/pkg/inputprocessor/clangparser"
	"team/foundry-x/re-client/internal/pkg/inputprocessor/flags"
)

// Parser parses clang command args to produce a CommandFlags object.
type Parser struct{}

var (
	abiFlags map[string]int
)

func init() {
	abiFlags = clangparser.ClangOptions
	abiFlags["--root-dir"] = 1
}

// ParseFlags is used to translate the given action command into clang compiler
// options, so that they can be used during input processing.
// Android build, throw error for unsupported flags.
func (h Parser) ParseFlags(ctx context.Context, command []string, workingDir, execRoot string) (*flags.CommandFlags, error) {
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
	s := args.Scanner{
		Args:       command[1:],
		Flags:      abiFlags,
		Joined:     clangparser.ClangPrefixes,
		Normalized: clangparser.ClangNormalizedFlags,
	}
	defer state.Finalize(res)
	for s.HasNext() {
		nr := s.NextResult()
		if nr.NormalizedKey == "root-dir" || nr.OriginalKey == "--" {
			continue
		}
		if err := state.HandleClangFlags(nr, res, false); err != nil {
			return res, err
		}
	}
	return res, nil
}
