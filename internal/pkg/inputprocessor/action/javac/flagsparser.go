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

package javac

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/args"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/flags"
	"github.com/bazelbuild/reclient/internal/pkg/rsp"
)

// parseFlags is used to transform a javac command into a CommandFlags structure.
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
	s := args.Scanner{
		Args: command[1:],
		Flags: map[string]int{
			"-bootclasspath": 1,
			"-classpath":     1,
			"-processorpath": 1,
			"-d":             1,
			"-s":             1,
		},
		Joined: []args.PrefixOption{
			{"--system=", 0},
		},
	}
	for s.HasNext() {
		flag, args, values, _ := s.Next()
		switch flag {
		case "-bootclasspath", "-classpath", "-processorpath":
			deps := strings.Split(values[0], ":")
			for _, d := range deps {
				// Exclude empty strings and . strings from dependencies.
				if d == "" || d == "." {
					continue
				}
				res.Dependencies = append(res.Dependencies, d)
			}
			continue
		case "--system=":
			res.Dependencies = append(res.Dependencies, values[0])
			continue
		case "-d", "-s":
			res.OutputDirPaths = append(res.OutputDirPaths, values[0])
			continue
		case "":
			if strings.HasPrefix(args[0], "@") {
				rspFile := args[0][1:]
				res.TargetFilePaths = append(res.TargetFilePaths, rspFile)
				rspDeps, err := rsp.Parse(filepath.Join(execRoot, workingDir, rspFile))
				if err != nil {
					return nil, err
				}
				res.Dependencies = append(res.Dependencies, rspDeps...)
				continue
			}
		}
		res.Flags = append(res.Flags, &flags.Flag{Value: args[0]})
	}
	return res, nil
}
