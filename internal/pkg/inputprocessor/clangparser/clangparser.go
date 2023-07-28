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

// Package clangparser provides functionalities to understand clang-family command line flags.
package clangparser

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/args"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/flags"
	"github.com/bazelbuild/reclient/internal/pkg/rsp"
)

const (
	thinltoBcExt      = ".thinlto.bc"
	thinltoImportsExt = ".imports"
)

func init() {
	ClangOptions["-fmax-tokens"] = 1

	ClangPrefixes = append(ClangPrefixes,
		args.PrefixOption{
			Prefix: "--coverage-data-file=",
		},
	)

	sort.Slice(ClangPrefixes, func(i, j int) bool {
		return ClangPrefixes[i].Prefix > ClangPrefixes[j].Prefix
	})

	// llvm archive is older than /showIncludes:user.
	// showInclude:user is added
	// commit: bcda1269c4c4 2020-02-24
	ClangCLOptions["/showIncludes:user"] = 0

	// llvm archive is older than -fprofile_use support for clang-cl.
	// -fprofile_use is added
	// commit: 6f1147f 2022-02-11
	ClangCLOptions["-fprofile_use"] = 0
	ClangCLPrefixes = append(ClangCLPrefixes,
		args.PrefixOption{
			Prefix: "-fprofile-use=",
		},
	)

	sort.Slice(ClangCLPrefixes, func(i, j int) bool {
		return ClangCLPrefixes[i].Prefix > ClangCLPrefixes[j].Prefix
	})

	// this prevents discarding all flags that appear after '--'
	ClangOptions["--"] = 0
	ClangCLOptions["--"] = 0
}

// New returns arguments scanner for command line type.
func New(cmdline []string) *args.Scanner {
	basename := filepath.Base(cmdline[0])
	switch strings.TrimSuffix(basename, filepath.Ext(basename)) {
	case "clang-cl":
		return &args.Scanner{
			Args:       cmdline[1:],
			Flags:      ClangCLOptions,
			Joined:     ClangCLPrefixes,
			Normalized: ClangCLNormalizedFlags,
		}
	default:
		return &args.Scanner{
			Args:       cmdline[1:],
			Flags:      ClangOptions,
			Joined:     ClangPrefixes,
			Normalized: ClangNormalizedFlags,
		}
	}
}

// State holds the state of clang flag parsing useful for understanding command inputs/outputs.
type State struct {
	objFile     string
	hasCoverage bool
	hasSplit    bool
}

// HandleClangFlags updates the given CommandFlags with the passed flag given the current ClangState.
func (s *State) HandleClangFlags(nextRes *args.NextResult, f *flags.CommandFlags, legacyBehavior bool) error {
	normalizedKey, values := nextRes.NormalizedKey, nextRes.Values
	switch normalizedKey {
	case "-MD":
		return nil
	case "-o":
		s.objFile = values[0]
		f.OutputFilePaths = append(f.OutputFilePaths, s.objFile)
		return nil
	case "-MF":
		f.OutputFilePaths = append(f.OutputFilePaths, values[0])
		f.EmittedDependencyFile = values[0]
		return nil
	case "-I":
		f.IncludeDirPaths = append(f.IncludeDirPaths, values[0])
	case "-B",
		"-fprofile-use=",
		"-fsanitize-blacklist=",
		"-fsanitize-ignorelist=",
		"-fprofile-sample-use=",
		"--prefix=",
		"-fprofile-list=":
		f.Dependencies = append(f.Dependencies, values[0])
	case "--coverage", "-ftest-coverage":
		s.hasCoverage = true
	case "-isysroot":
		if runtime.GOOS == "darwin" {
			f.Dependencies = append(f.Dependencies, filepath.Join(values[0], "SDKSettings.json"))
		}
	case "--coverage-data-file=":
		f.OutputFilePaths = append(f.OutputFilePaths, values[0])
	case "-gsplit-dwarf":
		s.hasSplit = true
	case "-fthinlto-index=":
		thinltoDeps, err := collectThinLTODeps(f.ExecRoot, f.WorkingDirectory, values[0])
		f.Dependencies = append(f.Dependencies, thinltoDeps...)
		if err != nil {
			return err
		}
	case "":
		if len(values) > 0 && strings.HasPrefix(values[0], "@") {
			f.Dependencies = append(f.Dependencies, values[0][1:])
		} else {
			// This should be a file to compile.
			f.TargetFilePaths = append(f.TargetFilePaths, values...)
		}
	}
	if normalizedKey == "" {
		return nil
	}
	// Handle all options not specially handled.
	switch {
	case !legacyBehavior && len(values) == 0:
		f.Flags = append(f.Flags, flags.New(normalizedKey, nextRes.OriginalKey, "", nextRes.Joined))
	case !legacyBehavior && len(values) == 1:
		f.Flags = append(f.Flags, flags.New(normalizedKey, nextRes.OriginalKey, values[0], nextRes.Joined))
	default:
		for _, arg := range nextRes.Args {
			f.Flags = append(f.Flags, &flags.Flag{Value: arg})
		}
	}
	return nil
}

// Finalize finalizes the passed CommandFlags based on the current State.
func (s *State) Finalize(f *flags.CommandFlags) {
	base := strings.TrimSuffix(s.objFile, filepath.Ext(s.objFile))
	if s.objFile == "" {
		return
	}
	if s.hasCoverage {
		f.OutputFilePaths = append(f.OutputFilePaths, base+".gcno")
	}
	if s.hasSplit {
		f.OutputFilePaths = append(f.OutputFilePaths, base+".dwo")
	}
}

// Collects ThinLTO bitcode files and dependencies.
//
// This function collects files needed for ThinLTO based on the flag
// -fthinlto-index which specifies the bitcode file which contains the ThinLTO summary.
// The bitcode file extension is `.thinlto.bc`.
//
// Another imports file that shares the same name as the bitcode file but with the extension
// `.imports` may be present next to the bitcode file. It contains a line-separated list of
// file names that are required for the linking step. The file paths are
// relative to the working directory of the command.
//
// For details about the flags, see this CL: https://reviews.llvm.org/D64461
// For ThinLTO docs in LLVM, see https://clang.llvm.org/docs/ThinLTO.html
// For a design overview on ThinLTO in LLVM, see http://blog.llvm.org/2016/06/thinlto-scalable-and-incremental-lto.html
//
// Returned file paths are relative to the working directory of the command and will
// be made relative to the ExecRoot in upstream code.
func collectThinLTODeps(execRoot, workingDir, bcFile string) ([]string, error) {
	var deps []string
	if !strings.HasSuffix(bcFile, thinltoBcExt) {
		return nil, fmt.Errorf("thinlto bitcode file extension is not %q: %s", thinltoBcExt, bcFile)
	}

	deps = append(deps, bcFile)

	// If an imports file is available, parse it to collect additional dependencies.
	importsFilepath := strings.Replace(bcFile, thinltoBcExt, thinltoImportsExt, 1)
	importedFiles, err := rsp.Parse(filepath.Join(execRoot, workingDir, importsFilepath))
	// Ignore a non-existent import file as a graceful failure since empty import files are valid.
	if errors.Is(err, os.ErrNotExist) {
		return deps, nil
	}
	// File exists and might contain required files.
	if err != nil {
		return nil, fmt.Errorf("failed to read thinlto import file: %w", err)
	}

	// fp is relative to the working directory of the command rather than
	// the directory of importsFilepath.
	for _, fp := range importedFiles {
		deps = append(deps, fp)
	}
	return deps, nil
}
