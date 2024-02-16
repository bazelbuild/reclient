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
	"path/filepath"
	"strings"

	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/args"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/clangparser"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/flags"
	"github.com/bazelbuild/reclient/internal/pkg/rsp"
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
	s := clangparser.New(command)
	for s.HasNext() {
		if err := state.handleClangCLFlags(res, s); err != nil {
			return nil, err
		}
	}
	state.Finalize(res)
	return res, nil
}

type clangCLState struct {
	clangparser.State
}

func shouldPopLastFlag(f *flags.Flag) bool {
	if f.Key == clangOption && f.Value == "-MF" && f.Joined && f.OriginalKey() == "/clang:" {
		return true
	}
	return false
}

func (s *clangCLState) handleClangCLFlags(cmdFlags *flags.CommandFlags, scanner *args.Scanner) error {
	nextRes := scanner.ReadNextFlag()
	f := &flags.CommandFlags{
		ExecRoot:         cmdFlags.ExecRoot,
		WorkingDirectory: cmdFlags.WorkingDirectory,
	}
	handleArgFunc := func(sc *args.Scanner) error {
		prev, cur := sc.PrevResult, sc.CurResult
		flag, values := cur.NormalizedKey, cur.Values
		if prev.NormalizedKey == clangOption {
			if prev.Values[0] == "-MF" && flag == clangOption {
				if lastFlg := cmdFlags.Flags[len(cmdFlags.Flags)-1]; shouldPopLastFlag(lastFlg) {
					// Pop the last flag, if it is &{-clang: -MF true /clang:}, because we handle it here.
					cmdFlags.Flags = cmdFlags.Flags[:len(cmdFlags.Flags)-1]
				}
				// Overwrite should be OK here as -MF only appear once in a clang-cl cmd.
				cmdFlags.EmittedDependencyFile = values[0]
				f.OutputFilePaths = append(f.OutputFilePaths, values[0])
				return nil
			}
		}
		switch flag {
		case "-Fo":
			f.OutputFilePaths = append(f.OutputFilePaths, values[0])
			return nil
		}
		return s.State.HandleClangFlags(&cur, f, false)
	}

	// Check if this is a rsp file that needs processing or just a normal flag.
	if strings.HasPrefix(nextRes.Args[0], "@") {
		rspFile := nextRes.Args[0][1:]
		cmdFlags.Dependencies = append(cmdFlags.Dependencies, rspFile)
		if !filepath.IsAbs(rspFile) {
			rspFile = filepath.Join(cmdFlags.ExecRoot, cmdFlags.WorkingDirectory, rspFile)
		}
		cmdFlags.Flags = append(cmdFlags.Flags, &flags.Flag{Value: nextRes.Args[0]})
		if err := rsp.ParseWithFunc(rspFile, *scanner, handleArgFunc); err != nil {
			return err
		}
		// We don't want to pass along the flags that were in the rsp file, just the
		// rsp file itself as a flag.
	} else {
		if err := handleArgFunc(scanner); err != nil {
			return err
		}
		// We want to pass along flags that were on the command line.
		cmdFlags.Flags = append(cmdFlags.Flags, f.Flags...)
	}
	cmdFlags.Dependencies = append(cmdFlags.Dependencies, f.Dependencies...)
	cmdFlags.OutputDirPaths = append(cmdFlags.OutputDirPaths, f.OutputDirPaths...)
	cmdFlags.OutputFilePaths = append(cmdFlags.OutputFilePaths, f.OutputFilePaths...)
	cmdFlags.IncludeDirPaths = append(cmdFlags.IncludeDirPaths, f.IncludeDirPaths...)
	cmdFlags.TargetFilePaths = append(cmdFlags.TargetFilePaths, f.TargetFilePaths...)
	return nil
}
