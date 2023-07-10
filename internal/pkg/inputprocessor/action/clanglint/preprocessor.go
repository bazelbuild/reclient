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

// Package clanglint performs include processing given a valid clang-tidy action.
package clanglint

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"team/foundry-x/re-client/internal/pkg/inputprocessor/action/cppcompile"
	"team/foundry-x/re-client/internal/pkg/inputprocessor/flags"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/cache"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"

	log "github.com/golang/glog"
)

var (
	clangTidyResourceDirOutputParser = regexp.MustCompile(`^(.*)\n`)
	rdCache                          = cache.SingleFlight{}
)

type systemExecutor interface {
	Execute(ctx context.Context, cmd *command.Command) (string, string, error)
}

// Preprocessor is the preprocessor of clang cpp lint actions.
type Preprocessor struct {
	*cppcompile.Preprocessor
}

// ComputeSpec computes cpp header dependencies.
func (p *Preprocessor) ComputeSpec() error {
	var err error
	p.Flags, err = p.clangTidyToCPPFlags(p.Flags)
	if err != nil {
		p.Err = err
		return p.Err
	}
	return p.Preprocessor.ComputeSpec()
}

// clangTidyToCPPFlags converts the given clang-tidy action to a cpp compile action
// so that we can run it by clang-scan-deps input processor.
func (p *Preprocessor) clangTidyToCPPFlags(f *flags.CommandFlags) (*flags.CommandFlags, error) {
	res := f.Copy()
	compilerFlags := []*flags.Flag{}
	for i := 0; i < len(res.Flags); i++ {
		if res.Flags[i].Key == "--" {
			rd, err := p.resourceDir(f)
			if err != nil {
				return nil, err
			}
			compilerFlags = append(compilerFlags, &flags.Flag{Key: "-resource-dir", Value: rd})
			compilerFlags = append(compilerFlags, res.Flags[i+1:]...)
			break
		}
	}
	res.Flags = compilerFlags
	return res, nil
}

// resourceDir is used to determine the resource directory used by the clang-tidy
// tool. It is explicitly passed into clang-scan-deps since scan-deps cannot find
// out the resource-dir if the clang-tidy executable does not support it via the
// "-print-resource-dir" option.
func (p *Preprocessor) resourceDir(f *flags.CommandFlags) (string, error) {
	computeResourceDir := func() (interface{}, error) {
		cmd := &command.Command{
			Args: []string{
				f.ExecutablePath,
				"--version",
				"--",
				"-print-resource-dir",
			},
			ExecRoot:   f.ExecRoot,
			WorkingDir: f.WorkingDirectory,
		}

		outStr, _, err := p.Executor.Execute(context.Background(), cmd)
		if err != nil {
			return nil, err
		}

		matches := clangTidyResourceDirOutputParser.FindStringSubmatch(outStr)
		if len(matches) < 1 {
			return nil, fmt.Errorf("unexpected clang-tidy output when trying to find resource directory: %v", outStr)
		}
		// The output of clang-tidy is this:
		// ```
		// lib64/clang/11.0.2
		// LLVM (http://llvm.org/):
		//   LLVM version 11.0.2git
		//   Optimized build.
		//   Default target: x86_64-unknown-linux-gnu
		//   Host CPU: broadwell
		// ```
		// The resource dir is determined in clang as:
		// `../../<check for lib64 / lib dir>/clang/<hard-coded-clang-version>`.
		resourceDir := filepath.Join(f.ExecutablePath, "../../", strings.TrimSuffix(matches[0], "\n"))
		if _, err := os.Stat(resourceDir); os.IsNotExist(err) {
			return nil, fmt.Errorf("resource-directory not found at: %v", resourceDir)
		}
		log.Infof("Determined resource-dir for clang-tidy action: %v", resourceDir)
		return resourceDir, nil
	}

	rd, err := rdCache.LoadOrStore(f, computeResourceDir)
	if err != nil {
		return "", err
	}
	res, ok := rd.(string)
	if !ok {
		return "", fmt.Errorf("unexpected type stored in cache: %v", rd)
	}
	return res, nil
}
