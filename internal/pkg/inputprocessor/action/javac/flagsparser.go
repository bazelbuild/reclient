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
			{"-Aroom.schemaLocation=", 0},
		},
	}
	for s.HasNext() {
		if err := handleJavacFlags(res, &s); err != nil {
			return nil, err
		}
	}
	return res, nil
}

func handleJavacFlags(cmdFlags *flags.CommandFlags, scanner *args.Scanner) error {
	nextRes := scanner.ReadNextFlag()
	// A temporary CommandFlags structure is used to collect the various dependencies, flags and outputs in the handleArgFunc,
	// and then the results of that are copied into the CommandFlags structure passed in (cmdFlags).
	// This is done because if a flag is being processed that is in a rsp file, we don't want to add that flag to
	// the Flags list; otherwise, the flags will show up on the remote bot twice, one time in the cmd itself,
	// the second time inside the rsp file. Perhaps a cleaner option is to add a
	// flag to the handleArgFunc that indicates if the argument is from the command line or from inside a rsp file.
	f := &flags.CommandFlags{
		ExecRoot:         cmdFlags.ExecRoot,
		WorkingDirectory: cmdFlags.WorkingDirectory,
	}
	handleArgFunc := func(sc *args.Scanner) error {
		curr := sc.CurResult
		flag, args, values := curr.NormalizedKey, curr.Args, curr.Values
		switch flag {
		case "-bootclasspath", "-classpath", "-processorpath":
			deps := strings.Split(values[0], ":")
			for _, d := range deps {
				// Exclude empty strings and . strings from dependencies.
				if d == "" || d == "." {
					continue
				}
				f.Dependencies = append(f.Dependencies, d)
			}
		case "--system=", "-Aroom.schemaLocation=":
			f.Dependencies = append(f.Dependencies, values[0])
		case "-d", "-s":
			f.OutputDirPaths = append(f.OutputDirPaths, values[0])
		case "":
			f.Dependencies = append(f.Dependencies, args[0])
		}

		for _, arg := range args {
			f.Flags = append(f.Flags, &flags.Flag{Value: arg})
		}
		return nil
	}
	// Check if this is a rsp file that needs processing or just a normal flag.
	if strings.HasPrefix(nextRes.Args[0], "@") {
		rspFile := nextRes.Args[0][1:]
		cmdFlags.TargetFilePaths = append(cmdFlags.TargetFilePaths, rspFile)
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
	return nil
}
