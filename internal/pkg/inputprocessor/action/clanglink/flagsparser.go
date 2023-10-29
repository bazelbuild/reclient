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

package clanglink

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bazelbuild/reclient/internal/pkg/features"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/args"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/clangparser"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/flags"
	"github.com/bazelbuild/reclient/internal/pkg/rsp"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/cache"

	log "github.com/golang/glog"
)

var (
	arCache     = cache.SingleFlight{}
	linkerFlags = []string{
		"--version-script",
		"--symbol-ordering-file",
		"--dynamic-list",
		"-T",
		"--retain-symbols-file",
		"--script,",
	}
)

// parseFlags is used to translate the given action command into clang linker
// options, so that they can be used during input processing.
func parseFlags(ctx context.Context, command []string, workingDir, execRoot string, arDeepScan bool) (*flags.CommandFlags, error) {
	numArgs := len(command)
	if numArgs < 2 {
		return nil, fmt.Errorf("insufficient number of arguments in command: %v", command)
	}

	res := &flags.CommandFlags{
		ExecutablePath:   command[0],
		WorkingDirectory: workingDir,
		ExecRoot:         execRoot,
	}
	var state clangLinkState
	s := clangparser.New(command)
	for s.HasNext() {
		err := state.handleClangLinkFlags(s.NextResult(), res, s, arDeepScan)
		if err != nil {
			return nil, err
		}
	}
	state.Finalize(res)
	return res, nil
}

type clangLinkState struct {
	clangparser.State
}

func (s *clangLinkState) handleClangLinkFlags(nextRes *args.NextResult, flgs *flags.CommandFlags, scanner *args.Scanner, arDeepScan bool) error {
	// A temporary CommandFlags structure is used to collect the various dependencies, flags and outputs in the handleArgFunc,
	// and then the results of that are copied into the CommandFlags structure passed in (flgs).
	// This is done because if a flag is being processed that is in an rsp file, we don't want to add that flag to
	// the Flags list.  Perhaps a cleaner option is to add a flag to the handleArgFunc that indicates if the argument is from the
	// command line or from inside an rsp file.
	f := &flags.CommandFlags{
		ExecRoot:         flgs.ExecRoot,
		WorkingDirectory: flgs.WorkingDirectory,
	}

	handleArgFunc := func(arg *args.NextResult) error {
		flag, args, values := arg.NormalizedKey, arg.Args, arg.Values
	flagSwitch:
		switch flag {
		case "--sysroot", "--sysroot=":
			if !features.GetConfig().ExperimentalSysrootDoNotUpload {
				f.Dependencies = append(f.Dependencies, values[0])
			}
		case "-Wl,":
			for _, prefix := range linkerFlags {
				if strings.HasPrefix(values[0], prefix) {
					value := strings.TrimPrefix(values[0], prefix)
					// TODO(b/184928955): There's a few available options here,
					// eg ',' or '='. Frustratingly, we have to support them all.
					value = strings.TrimPrefix(value, "=")
					value = strings.TrimPrefix(value, ",")
					f.Dependencies = append(f.Dependencies, value)
					break flagSwitch
				}
			}
			if strings.HasPrefix(values[0], "--out-implib=") {
				value := strings.TrimPrefix(values[0], "--out-implib=")
				f.OutputFilePaths = append(f.OutputFilePaths, value)
			}
		case "-L":
			f.Dependencies = append(f.Dependencies, values[0])
		case "":
			switch {
			case strings.HasSuffix(args[0], ".a"):
				archdeps, err := processArchive(args[0], f, arDeepScan)
				if err != nil {
					log.Warningf("Unable to process archive %v, %v", args[0], err)
					return nil
				}
				f.Dependencies = append(f.Dependencies, archdeps...)
			default:
				if !strings.HasPrefix(args[0], "-l") {
					f.Dependencies = append(f.Dependencies, args[0])
				}
			}
		default:
			return s.State.HandleClangFlags(nextRes, f, true)
		}

		for _, arg := range args {
			f.Flags = append(f.Flags, &flags.Flag{Value: arg})
		}
		return nil
	}

	// Check if this is a rsp file that needs processing or just a normal flag.
	if strings.HasPrefix(nextRes.Args[0], "@") {
		rspFile := nextRes.Args[0][1:]
		flgs.Dependencies = append(flgs.Dependencies, rspFile)
		if !filepath.IsAbs(rspFile) {
			rspFile = filepath.Join(flgs.ExecRoot, flgs.WorkingDirectory, rspFile)
		}
		flgs.Flags = append(flgs.Flags, &flags.Flag{Value: nextRes.Args[0]})
		err := rsp.ParseWithFunc(rspFile, *scanner, handleArgFunc)
		if err != nil {
			return err
		}
		// We don't want to pass along the flags that were in the rsp file, just the
		// rsp file itself as a flag.
	} else {
		err := handleArgFunc(nextRes)
		if err != nil {
			return err
		}
		// We want to pass along flags that were on the command line.
		flgs.Flags = append(flgs.Flags, f.Flags...)
	}
	flgs.Dependencies = append(flgs.Dependencies, f.Dependencies...)
	flgs.OutputDirPaths = append(flgs.OutputDirPaths, f.OutputDirPaths...)
	flgs.OutputFilePaths = append(flgs.OutputFilePaths, f.OutputFilePaths...)
	return nil
}

func processArchive(archive string, f *flags.CommandFlags, arDeepScan bool) ([]string, error) {
	arAbsName := filepath.Join(f.ExecRoot, f.WorkingDirectory, archive)
	depsIntf, err := arCache.LoadOrStore(arAbsName, func() (interface{}, error) {
		archinputs := []string{archive}
		if arDeepScan {
			archdeps, err := readArchive(arAbsName, archive)
			if err != nil {
				return nil, err
			}
			return append(archdeps, archive), nil
		}
		return archinputs, nil
	})

	if err != nil {
		return nil, err
	}
	deps := depsIntf.([]string)
	return deps, nil
}

func readArchive(arPath string, archive string) ([]string, error) {
	arReader, err := newArReader(arPath, filepath.Dir(archive))
	if err != nil {
		return nil, err
	}
	defer arReader.Close()
	return arReader.ReadFileNames()
}
