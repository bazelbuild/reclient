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

package d8

import (
	"context"
	"fmt"

	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/args"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/flags"
)

// parseFlags is used to transform an d8 command into a CommandFlags structure.
func parseFlags(ctx context.Context, command []string, workingDir, execRoot string) (*flags.CommandFlags, error) {
	numArgs := len(command)
	if numArgs < 2 {
		return nil, fmt.Errorf("insufficient number of arguments in command: %v", command)
	}

	res := &flags.CommandFlags{
		ExecutablePath:   command[0],
		WorkingDirectory: workingDir,
		TargetFilePaths:  []string{command[numArgs-1]},
		ExecRoot:         execRoot,
	}

	s := args.Scanner{
		Args: command[1 : len(command)-1],
		Flags: map[string]int{
			"--lib":           1,
			"--main-dex-list": 1,
			"--output":        1,
		},
		Joined: []args.PrefixOption{
			{Prefix: "--main-dex-list=", NumArgs: 0},
		},
	}
	for s.HasNext() {
		flag, args, values, _ := s.Next()
		switch flag {
		case "--lib",
			"--main-dex-list",
			"--main-dex-list=":
			res.Dependencies = append(res.Dependencies, values[0])
			continue
		case "--output":
			res.OutputDirPaths = append(res.OutputDirPaths, values[0])
			continue
		}
		res.Flags = append(res.Flags, &flags.Flag{Value: args[0]})
	}
	return res, nil
}
