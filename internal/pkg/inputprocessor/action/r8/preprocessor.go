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

// Package r8 performs include processing of r8 actions.
package r8

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"team/foundry-x/re-client/internal/pkg/inputprocessor"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/cache"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
)

const (
	// includePrefix is used in flag files to indicate that another flag file is included.
	includePrefix = "-include "
)

var (
	r8Cache = cache.SingleFlight{}
)

// Preprocessor is the context for processing tool type actions.
type Preprocessor struct {
	*inputprocessor.BasePreprocessor
}

// ParseFlags parses the commands flags and populates the ActionSpec object with inferred
// information.
func (p *Preprocessor) ParseFlags() error {
	f, err := parseFlags(p.Ctx, p.Options.Cmd, p.Options.WorkingDir, p.Options.ExecRoot)
	if err != nil {
		p.Err = fmt.Errorf("flag parsing failed. %v", err)
		return p.Err
	}
	p.Flags = f
	p.FlagsToActionSpec()
	return nil
}

// ComputeSpec computes any further action specification that is not immediately inferrable
// from flags or toolchain configuration.
func (p *Preprocessor) ComputeSpec() error {
	s := &inputprocessor.ActionSpec{InputSpec: &command.InputSpec{}}

	// Add including flag files.
	for _, dep := range p.Flags.Dependencies {
		if filepath.Ext(dep) == ".txt" || filepath.Ext(dep) == ".flags" {
			depIncludes, err := p.includesInFlagsFile(p.Ctx, p.Options.ExecRoot, p.Options.WorkingDir, dep)
			if err != nil {
				return err
			}
			s.InputSpec.Inputs = append(s.InputSpec.Inputs, depIncludes...)
		}
	}
	p.AppendSpec(s)

	// Add output directory as virtual input.
	s, err := p.Spec()
	if err != nil {
		return err
	}
	var vi []*command.VirtualInput
	for _, od := range s.OutputDirectories {
		vi = append(vi, &command.VirtualInput{Path: od, IsEmptyDirectory: true})
	}
	p.AppendSpec(&inputprocessor.ActionSpec{
		InputSpec: &command.InputSpec{VirtualInputs: vi},
	})
	return nil
}

func (p *Preprocessor) includesInFlagsFile(ctx context.Context, execRoot, workingDir, f string) ([]string, error) {
	compute := func() (interface{}, error) {
		file, err := os.Open(filepath.Join(execRoot, workingDir, f))
		if err != nil {
			return nil, err
		}
		defer file.Close()

		var includes []string
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			if !strings.HasPrefix(scanner.Text(), includePrefix) {
				continue
			}
			path := scanner.Text()[len(includePrefix):]
			includes = append(includes, filepath.Join(filepath.Dir(f), path))
		}

		if err := scanner.Err(); err != nil {
			return nil, err
		}
		return includes, nil
	}
	files, err := r8Cache.LoadOrStore(f, compute)
	if err != nil {
		return nil, err
	}
	v, ok := files.([]string)
	if !ok {
		return nil, fmt.Errorf("unexpected type stored in the cache: %v", files)
	}
	var includes []string
	for _, inc := range v {
		includes = append(includes, inc)
		subIncludes, err := p.includesInFlagsFile(ctx, execRoot, workingDir, inc)
		if err != nil {
			return nil, err
		}
		includes = append(includes, subIncludes...)
	}
	return includes, nil
}
