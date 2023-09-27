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

// Package inputprocessor contains code for processing actions to determine their inputs/outputs.
package inputprocessor

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/flags"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/flagsparser"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/toolchain"
	"github.com/bazelbuild/reclient/internal/pkg/localresources"
	"github.com/bazelbuild/reclient/internal/pkg/pathtranslator"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/cache"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/filemetadata"
	log "github.com/golang/glog"
)

const (
	// shallowLabel is the label key to indicate whether to use the shallow
	// input processor for the command.
	shallowLabel = "shallow"
)

var (
	normalizedFileErrCache = &sync.Map{}
)

// Options encapsulates options for initializing a preprocessor.
type Options struct {
	// ExecutionID is the ID of the action.
	ExecutionID string
	// Cmd is the list of args.
	Cmd []string
	// WorkingDir is the working directory of the action.
	WorkingDir string
	// ExecRoot is the exec root of the action.
	ExecRoot string
	// Inputs is the InputSpec passed explicitly with the action request.
	Inputs *command.InputSpec
	// Labels is a map of label keys to values.
	Labels map[string]string
	// ToolchainInputs is a list of toolchain inputs in addition to the toolchains
	// inferred from the command.
	ToolchainInputs []string
	// ShallowFallback indicates whether preprocessing is allowed to fallback to shallow
	// mode if an error is encountered.
	ShallowFallback bool

	// WindowsCross indicates whether to use Linux worker for Windows.
	WindowsCross bool
}

// ActionSpec encapsulates the inputs and outputs a command. All paths are relative to the
// exec root.
type ActionSpec struct {
	// InputSpec holds information about files and environment variables required to
	// run the command.
	InputSpec *command.InputSpec
	// OutputFiles is a list of output files produced by the command.
	OutputFiles []string
	// OutputDirectories is a list of output directories produced by the command.
	OutputDirectories []string
	// EmiitedDependencyFile is the name of the dependency file produced by the command.
	EmittedDependencyFile string
	// UsedShallowMode indicates whether the shallow input processor was used to
	// determine inputs.
	UsedShallowMode bool
}

// Executor can run commands and retrieve their outputs.
type Executor interface {
	Execute(ctx context.Context, cmd *command.Command) (string, string, error)
}

// Preprocessor is an interface for determining specs of an action. Refer to Compute() below to
// see how implementers of the interface should be called.
type Preprocessor interface {
	// Init initializes the preprocessor with the given options.
	Init(options Options)
	// ParseFlags parses the commands flags and populates the ActionSpec object with inferred
	// information. Returning an error means preprocessing should not continue, and the user
	// should Sanitize() the currently processed ActionSpec and then read it if available using
	// Spec().
	ParseFlags() error
	// ProcessToolchains determines toolchain inputs required for the command. Returning an
	// error means preprocessing should not continue, and the user should Sanitize() the
	// currently processed ActionSpec and then read it if available using Spec().
	ProcessToolchains() error
	// ComputeSpec computes any further action specification that is not immediately inferrable
	// from flags or toolchain configuration. Returning an error means preprocessing should not
	// continue, and the user should Sanitize() the currently processed ActionSpec and then read
	// it if available using Spec().
	ComputeSpec() error
	// Sanitize cleans up the spec by removing unwanted entries. Normally this includes
	// deduping and removing non-existent inputs.
	Sanitize()
	// Spec retrieves the ActionSpec currently inferred for the options passed to the
	// context.
	Spec() (*ActionSpec, error)
	// Error returns the fatal error encountered during preprocessing, if exists.
	Error() error
}

// BasePreprocessor is the base preprocessor with commonly objects and preprocessing logic.
type BasePreprocessor struct {
	// Ctx is the context to use for internal preprocessing actions.
	Ctx context.Context
	// Executor is an entity that can execute commands on the local system.
	Executor Executor
	// ResourceManager manages available local resources to ensure local operations do not
	// overwhelm the machine.
	ResourceManager *localresources.Manager
	// Options is the options used to initialize the preprocessor. Should not change once the
	// preprocessor is initialized.
	Options Options
	// Flags is the set of flags determined from parsing the command. Should not change once
	// ParseFlags has been called.
	Flags *flags.CommandFlags
	// Err is an error encountered during preprocessing. An action that has an error could
	// not be remotely executed or cached.
	Err error
	// actionSpec is the specification of the action as currently determined by the
	// preprocessor.
	actionSpec *ActionSpec
	// FileMetadataCache is used to obtain the metadata of files.
	FileMetadataCache filemetadata.Cache
	// NormalizedFileCache is used to cache normalized paths.
	NormalizedFileCache *cache.SingleFlight
	// FileStatCache caches the results of os.Stat calls.
	FileStatCache *cache.SingleFlight
}

// Init initializes the preprocessor with the given options.
func (c *BasePreprocessor) Init(options Options) {
	c.Options = options
	c.actionSpec = &ActionSpec{
		InputSpec: c.Options.Inputs,
	}

	if c.actionSpec.InputSpec == nil {
		c.actionSpec.InputSpec = &command.InputSpec{}
	}

	v, ok := options.Labels[shallowLabel]
	if !ok {
		return
	}
	shallow, err := strconv.ParseBool(v)
	if err != nil {
		log.Warningf("Failed to parse shallow label value: %v", err)
	}
	c.actionSpec.UsedShallowMode = shallow
}

// ParseFlags parses the commands flags and populates the ActionSpec object with inferred
// information.
func (c *BasePreprocessor) ParseFlags() error {
	f, err := flagsparser.CommandFlags(c.Ctx, c.Options.Cmd, c.Options.WorkingDir, c.Options.ExecRoot)
	if err != nil {
		c.Err = fmt.Errorf("flag parsing failed. %v", err)
		return c.Err
	}
	c.Flags = f
	c.FlagsToActionSpec()
	return nil
}

// ProcessToolchains determines toolchain inputs required for the command.
func (c *BasePreprocessor) ProcessToolchains() error {
	if c.Flags == nil {
		c.Err = fmt.Errorf("no flags set")
		return c.Err
	}
	tp := &toolchain.InputProcessor{}
	tci, err := tp.ProcessToolchainInputs(c.Ctx, c.Options.ExecRoot, c.Options.WorkingDir, c.Flags.ExecutablePath, c.Options.ToolchainInputs, c.FileMetadataCache)
	if err != nil {
		c.actionSpec.UsedShallowMode = true
		return fmt.Errorf("toolchain processing failed. %v", err)
	}
	c.AppendSpec(&ActionSpec{InputSpec: tci})
	return nil
}

// ComputeSpec computes any further action specification that is not immediately inferrable
// from flags or toolchain configuration.
func (c *BasePreprocessor) ComputeSpec() error {
	return nil
}

// Sanitize cleans up the spec by removing unwanted entries. Normally this includes
// deduping and removing non-existent inputs.
func (c *BasePreprocessor) Sanitize() {
	c.FilterInputsUnderExecRoot()
	c.RewriteEnvironmentVariables()
	c.FilterVirtualInputs()
}

// Spec retrieves the ActionSpec currently inferred for the options passed to the context.
// For complete input/output processing, call ComputeSpec first.
func (c *BasePreprocessor) Spec() (*ActionSpec, error) {
	if c.Err != nil {
		return nil, c.Err
	}
	return c.actionSpec, nil
}

// Error returns the fatal error encountered during input processing if exists.
func (c *BasePreprocessor) Error() error {
	return c.Err
}

// FlagsToActionSpec populates the ActionSpec struct with inputs parsed fromt the command flags.
func (c *BasePreprocessor) FlagsToActionSpec() {
	if c.actionSpec == nil || c.actionSpec.InputSpec == nil {
		c.actionSpec = &ActionSpec{InputSpec: c.Options.Inputs}
	}
	allInputs := append(c.Flags.TargetFilePaths, c.Flags.Dependencies...)
	if c.Flags.ExecutablePath != "" {
		allInputs = append(allInputs, c.Flags.ExecutablePath)
	}
	allInputs = pathtranslator.ListRelToExecRoot(c.Options.ExecRoot, c.Options.WorkingDir, allInputs)
	//TODO(b/161932505): remove virtual inputs from CommandFlags.
	var vi []*command.VirtualInput
	for _, od := range pathtranslator.ListRelToExecRoot(c.Options.ExecRoot, c.Options.WorkingDir, c.Flags.VirtualDirectories) {
		vi = append(vi, &command.VirtualInput{Path: od, IsEmptyDirectory: true})
	}
	c.AppendSpec(&ActionSpec{
		InputSpec: &command.InputSpec{
			Inputs:        allInputs,
			VirtualInputs: vi,
		},
		OutputFiles:           pathtranslator.ListRelToExecRoot(c.Options.ExecRoot, c.Options.WorkingDir, c.Flags.OutputFilePaths),
		OutputDirectories:     pathtranslator.ListRelToExecRoot(c.Options.ExecRoot, c.Options.WorkingDir, c.Flags.OutputDirPaths),
		EmittedDependencyFile: pathtranslator.RelToExecRoot(c.Options.ExecRoot, c.Options.WorkingDir, c.Flags.EmittedDependencyFile),
	})
}

// FilterInputsUnderExecRoot removes any inputs that do not exist under the exec root.
func (c *BasePreprocessor) FilterInputsUnderExecRoot() {
	if c.actionSpec == nil || c.actionSpec.InputSpec == nil {
		return
	}
	pn := newPathNormalizer(c.Options.WindowsCross)
	var filtered []string
	m := make(map[string]bool)
	for _, f := range c.actionSpec.InputSpec.Inputs {
		if f == "" {
			continue
		}
		var normalized string
		var err error
		if c.NormalizedFileCache != nil {
			cache, e, loaded := c.NormalizedFileCache.Load(f)
			if !loaded {
				cache, e = pn.normalize(c.Options.ExecRoot, f)
				if e == nil {
					c.NormalizedFileCache.Store(f, cache)
				}
			}
			normalized = cache.(string)
			err = e
		} else {
			normalized, err = pn.normalize(c.Options.ExecRoot, f)
		}
		if err != nil {
			//Example error msg we want to get rid of:
			//c23aff58-b24a-41fb-8676-3f2886f544eb: failed to normalize out/rbe-build/python3 @ /tmpfs/source/chromium/src/: stat /tmpfs/source/chromium/src/out/rbe-build/python3: no such file or directory
			//with these variables defined:
			//c.Options.ExecutionID: c23aff58-b24a-41fb-8676-3f2886f544eb
			//f: out/rbe-build/python3
			//c.Options.ExecRoot: /tmpfs/source/chromium/src/
			//err: stat /tmpfs/source/chromium/src/out/rbe-build/python3: no such file or directory
			//
			//These error msgs are all generated from this line: cache, e = pn.normalize(c.Options.ExecRoot, f)
			// TODO(b/302290967) Update normalize() to prevent this error msg from generating.
			_, loaded := normalizedFileErrCache.LoadOrStore(err.Error(), struct{}{})
			if !loaded || bool(log.V(3)) {
				log.Warningf("%v: failed to normalize %s @ %s: %v", c.Options.ExecutionID, f, c.Options.ExecRoot, err)
			}
			continue
		}
		if normalized != f {
			log.V(2).Infof("%v: normalize %s -> %s @ %s", c.Options.ExecutionID, f, normalized, c.Options.ExecRoot)
		}
		if m[normalized] {
			log.V(1).Infof("%v: drop duplicate %s (normalized from %s) @ %s", c.Options.ExecutionID, normalized, f, c.Options.ExecRoot)
			continue
		}
		m[normalized] = true
		filtered = append(filtered, normalized)
	}
	c.actionSpec.InputSpec.Inputs = filtered

	// need to normalize other fields in InputSpec?
}

// FilterVirtualInputs removes virtual inputs that are not physically existing directories.
func (c *BasePreprocessor) FilterVirtualInputs() {
	if c.actionSpec == nil || c.actionSpec.InputSpec == nil {
		return
	}

	var filtered []*command.VirtualInput
	for _, vi := range c.actionSpec.InputSpec.VirtualInputs {
		virtualInputPath := vi.Path
		isDir := false
		path := filepath.Join(c.Options.ExecRoot, virtualInputPath)
		if c.FileMetadataCache != nil {
			fileMetadata := c.FileMetadataCache.Get(path)
			isDir = fileMetadata.IsDirectory
		} else {
			var stat fs.FileInfo
			var err error
			if c.FileStatCache != nil {
				cache, e, loaded := c.FileStatCache.Load(path)
				if !loaded {
					cache, e = os.Stat(path)
					if e == nil {
						c.FileStatCache.Store(path, cache)
					}
				}
				stat = cache.(fs.FileInfo)
				err = e
			} else {
				stat, err = os.Stat(path)
			}
			if err != nil {
				log.Warningf("Failed to determine whether virtual input %v exists", virtualInputPath)
			} else {
				isDir = stat.IsDir()
			}
		}

		if (isDir && vi.IsEmptyDirectory) || !vi.IsEmptyDirectory {
			filtered = append(filtered, vi)
		}
	}

	c.actionSpec.InputSpec.VirtualInputs = filtered
}

// RewriteEnvironmentVariables makes all environment variables specified in the input spec relative
// to the working directory.
func (c *BasePreprocessor) RewriteEnvironmentVariables() {
	if c.actionSpec == nil || c.actionSpec.InputSpec == nil {
		return
	}
	for k, v := range c.actionSpec.InputSpec.EnvironmentVariables {
		var vals []string
		for _, val := range strings.Split(v, string(os.PathListSeparator)) {
			if !filepath.IsAbs(val) {
				// If the path is not absolute, assume it is relative to the working directory.
				// Otherwise the action is incorrect even in local execution.
				vals = append(vals, val)
				continue
			}
			newVal := val
			if pathtranslator.RelToExecRoot(c.Options.ExecRoot, c.Options.WorkingDir, val) != "" {
				newVal = pathtranslator.RelToWorkingDir(c.Options.ExecRoot, c.Options.WorkingDir, val)
			}
			vals = append(vals, newVal)
		}
		c.actionSpec.InputSpec.EnvironmentVariables[k] = strings.Join(vals, string(os.PathListSeparator))
	}
}

// AppendSpec appends the given ActionSpec to the ActionSpec stored in the preprocessor.
func (c *BasePreprocessor) AppendSpec(s *ActionSpec) {
	if c.actionSpec == nil || c.actionSpec.InputSpec == nil {
		c.actionSpec = &ActionSpec{InputSpec: &command.InputSpec{}}
	}
	if s == nil {
		s = &ActionSpec{InputSpec: &command.InputSpec{}}
	}
	if s.InputSpec == nil {
		s.InputSpec = &command.InputSpec{}
	}

	c.actionSpec.InputSpec.Inputs = append(c.actionSpec.InputSpec.Inputs, s.InputSpec.Inputs...)
	c.actionSpec.InputSpec.VirtualInputs = append(c.actionSpec.InputSpec.VirtualInputs, s.InputSpec.VirtualInputs...)
	c.actionSpec.InputSpec.InputExclusions = append(c.actionSpec.InputSpec.InputExclusions, s.InputSpec.InputExclusions...)
	if c.actionSpec.InputSpec.EnvironmentVariables == nil && len(s.InputSpec.EnvironmentVariables) > 0 {
		c.actionSpec.InputSpec.EnvironmentVariables = make(map[string]string)
	}
	for k, v := range s.InputSpec.EnvironmentVariables {
		c.actionSpec.InputSpec.EnvironmentVariables[k] = v
	}
	c.actionSpec.OutputFiles = append(c.actionSpec.OutputFiles, s.OutputFiles...)
	c.actionSpec.OutputDirectories = append(c.actionSpec.OutputDirectories, s.OutputDirectories...)
	if s.EmittedDependencyFile != "" {
		c.actionSpec.EmittedDependencyFile = s.EmittedDependencyFile
	}
	if !c.actionSpec.UsedShallowMode {
		c.actionSpec.UsedShallowMode = s.UsedShallowMode
	}
}

// Compute computes the ActionSpec using the given preprocessor and options.
func Compute(p Preprocessor, options Options) (*ActionSpec, error) {
	p.Init(options)
	if err := p.ParseFlags(); err != nil {
		if options.ShallowFallback {
			log.Warningf("%v: Encountered error when parsing flags: %v", options.ExecutionID, err)
			p.Sanitize()
			return p.Spec()
		}
		return nil, err
	}
	if err := p.ProcessToolchains(); err != nil {
		if options.ShallowFallback {
			log.Warningf("%v: Encountered error when processing toolchains: %v", options.ExecutionID, err)
			p.Sanitize()
			return p.Spec()
		}
		return nil, err
	}
	s, _ := p.Spec()
	if s.UsedShallowMode {
		p.Sanitize()
		return p.Spec()
	}
	if err := p.ComputeSpec(); err != nil {
		if options.ShallowFallback {
			log.Warningf("%v: Encountered error when computing spec: %v", options.ExecutionID, err)
			p.Sanitize()
			return p.Spec()
		}
		return nil, err
	}
	p.Sanitize()
	return p.Spec()
}
