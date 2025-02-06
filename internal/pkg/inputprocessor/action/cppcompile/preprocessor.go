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

package cppcompile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	spb "github.com/bazelbuild/reclient/api/scandeps"
	"github.com/bazelbuild/reclient/internal/pkg/cppdependencyscanner"
	"github.com/bazelbuild/reclient/internal/pkg/event"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/depscache"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/flags"
	"github.com/bazelbuild/reclient/internal/pkg/logger"
	"github.com/bazelbuild/reclient/internal/pkg/pathtranslator"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/cache"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/digest"
	"golang.org/x/sync/semaphore"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"

	log "github.com/golang/glog"
)

const (
	cpus                 = 1
	ramMBs               = 64
	clangCompilerArgFlag = "-Xclang"
)

var (
	toAbsArgs = map[string]bool{
		"--sysroot":              true,
		"--sysroot=":             true,
		"-isysroot":              true,
		"-fprofile-sample-use=":  true,
		"-fsanitize-blacklist=":  true,
		"-fsanitize-ignorelist=": true,
		"-fprofile-list=":        true,
	}
	// These arguments are unsupported in the current version of clang-scan-deps and
	// need to be removed, otherwise they cause input processing to fail early in
	// argument parsing.
	toRemoveArgs = map[string]struct{}{
		"-fno-experimental-new-pass-manager": struct{}{},
		"-fexperimental-new-pass-manager":    struct{}{},
		// Our release binary has a few bytes difference than pre-release/local
		// build, this flag is falling the input processor only with our prod release
		// binary, see b/391924509
		"-fconserve-stack": struct{}{},
	}
	// These Xclang flags are unsupported, need to be removed before calling clang-scan-deps.
	toRemoveXclangFlags = map[string]struct{}{
		// Clang-scan-deps removes comments in the source code for optimized
		// preprocessing, which results in removal of "expected-*" directives
		// in comments. Consequently when the "-verify" flag is used and no
		// "expected-*" directives are found, the preprocessor fails with error.
		// Thus, remove the verify flag before calling clang-scan-deps.
		// TODO(b/148145163): Fix it in clang-scan-deps lexer in the long term.
		"-verify": struct{}{},
		// Clang-scan-deps is not able to process the `-fallow-half-arguments-and-returns`
		// flag, actions with this flag will not be able to reach to the RBE and
		// cause remote_failures. Thus, this flag is removed.
		// For details about "-fallow-half-arguments-and-returns", see b/296438658.
		"-fallow-half-arguments-and-returns": struct{}{},
	}
	virtualInputFlags = map[string]bool{
		"-I":                        true,
		"-isystem":                  true,
		"-isysroot":                 true,
		"--sysroot=":                true,
		"--sysroot":                 true,
		"-internal-isystem":         true,
		"-internal-externc-isystem": true,
	}
)

// CPPDependencyScanner is an interface to dependency scanner which provides
// functionality to find input dependencies given a compile command.
type CPPDependencyScanner interface {
	ProcessInputs(ctx context.Context, execID string, command []string, filename, directory string, cmdEnv []string) ([]string, bool, error)
	Capabilities() *spb.CapabilitiesResponse
}

type resourceDirInfo struct {
	fileInfo    os.FileInfo
	resourceDir string
}

var (
	resourceDirsMu sync.Mutex
	resourceDirs   = map[string]resourceDirInfo{}
	// virtualInputCache is a cache for the virtual inputs calculated from a given path.
	virtualInputCache = cache.SingleFlight{}
)

// VirtualInputsProcessor processes the flags and aappends virtual inputs to command's InputSpec.
type VirtualInputsProcessor interface {
	// IsVirtualInput returns true if the specified flag should result in appending a virtual input.
	IsVirtualInput(flag string) bool
	// AppendVirtualInput appends a virtual input to res. path arg should be exec root relative.
	AppendVirtualInput(res []*command.VirtualInput, flag, path string) []*command.VirtualInput
}

// Preprocessor is the preprocessor of clang cpp compile actions.
type Preprocessor struct {
	*inputprocessor.BasePreprocessor
	// CPPDepScanner is used to perform include processing by invoking
	// clang-scan-deps library.
	// https://github.com/llvm/llvm-project/tree/main/clang/tools/clang-scan-deps
	CPPDepScanner CPPDependencyScanner

	// DepScanTimeout is the max duration allowed for CPP dependency scanning operation
	// before it's interrupted
	DepScanTimeout time.Duration

	Rec *logger.LogRecord

	// DepsCache is a cache for cpp header dependencies.
	DepsCache *depscache.Cache

	// CmdEnvironment captures the environment of the command to be executed, in the form "key=value" strings.
	CmdEnvironment []string

	Slots *semaphore.Weighted

	testOnlySetDone func()
}

// ParseFlags parses the commands flags and populates the ActionSpec object with inferred
// information.
func (p *Preprocessor) ParseFlags() error {
	f, err := ClangParser{}.ParseFlags(p.Ctx, p.Options.Cmd, p.Options.WorkingDir, p.Options.ExecRoot)
	if err != nil {
		p.Err = fmt.Errorf("flag parsing failed. %v", err)
		return p.Err
	}
	p.Flags = f
	p.FlagsToActionSpec()
	return nil
}

// ComputeSpec computes cpp header dependencies.
func (p *Preprocessor) ComputeSpec() error {
	s := &inputprocessor.ActionSpec{InputSpec: &command.InputSpec{}}
	defer p.AppendSpec(s)

	args := p.BuildCommandLine("-o", false, toAbsArgs)
	if p.CPPDepScanner.Capabilities().GetExpectsResourceDir() {
		args = p.addResourceDir(args)
	}

	headerInputFiles, err := p.FindDependencies(args)
	if err != nil {
		s.UsedShallowMode = true
		return err
	}

	s.InputSpec = &command.InputSpec{
		Inputs:        headerInputFiles,
		VirtualInputs: VirtualInputs(p.Flags, p),
	}
	return nil
}

// FindDependencies finds the dependencies of the given adjusted command args
// using the current include scanner.
func (p *Preprocessor) FindDependencies(args []string) ([]string, error) {
	dg := digest.NewFromBlob([]byte(strings.Join(args, " ")))
	if len(p.Flags.TargetFilePaths) < 1 {
		log.Warningf("No target file was found in command-line (not running include scanner): %v", args)
		return nil, nil
	}
	filename := p.Flags.TargetFilePaths[0]
	key := depscache.Key{
		CommandDigest: dg.String(),
		SrcFilePath:   filepath.Join(p.Flags.ExecRoot, p.Flags.WorkingDirectory, filename),
	}
	res, ok := p.getFromDepsCache(key)
	if ok {
		return pathtranslator.ListRelToExecRoot(p.Flags.ExecRoot, "", res), nil
	}

	from := time.Now()
	evt := event.InputProcessorWait
	if p.Rec == nil {
		p.Rec = logger.NewLogRecord()
	}
	if p.Slots != nil {
		if err := p.Slots.Acquire(p.Ctx, 1); err != nil {
			return nil, err
		}
	}
	p.Rec.RecordEventTime(evt, from)

	from = time.Now()
	evt = event.CPPInputProcessor
	usedCache := false
	defer func() {
		if p.Slots != nil {
			p.Slots.Release(1)
		}
		if usedCache {
			evt = event.InputProcessorCacheLookup
		}
		if p.Rec == nil {
			p.Rec = logger.NewLogRecord()
		}
		p.Rec.RecordEventTime(evt, from)
	}()

	directory := filepath.Join(p.Flags.ExecRoot, p.Flags.WorkingDirectory)
	if !filepath.IsAbs(filename) {
		filename = filepath.Join(directory, filename)
	}
	dsCtx, cancel := maybeWithTimeout(p.Ctx, p.DepScanTimeout)
	defer cancel()
	var err error
	if res, usedCache, err = p.CPPDepScanner.ProcessInputs(dsCtx, p.Options.ExecutionID, args, filename, directory, p.CmdEnvironment); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: %v", cppdependencyscanner.ErrDepsScanTimeout, err)
		}
		return nil, fmt.Errorf("cpp dependency scanner's ProcessInputs failed: %w", err)
	}

	headerInputFiles := pathtranslator.ListRelToExecRoot(p.Flags.ExecRoot, "", res)
	go p.saveDeps(key, headerInputFiles)
	return headerInputFiles, nil
}

// BuildCommandLine builds a command line arguments from flags
func (p *Preprocessor) BuildCommandLine(outputFlag string, outputFlagJoined bool, toAbsArgs map[string]bool) []string {
	directory := filepath.Join(p.Flags.ExecRoot, p.Flags.WorkingDirectory)
	executablePath := p.Flags.ExecutablePath
	if !filepath.IsAbs(executablePath) {
		executablePath = filepath.Join(directory, executablePath)
	}
	args := []string{executablePath}
	filename := ""
	if len(p.Flags.TargetFilePaths) > 0 {
		filename = p.Flags.TargetFilePaths[0]
		if !filepath.IsAbs(filename) {
			filename = filepath.Join(directory, filename)
		}
	}
	for _, flag := range p.Flags.Flags {
		key, value := flag.Key, flag.Value
		if key == clangCompilerArgFlag {
			if _, present := toRemoveXclangFlags[value]; present {
				continue
			}
		}
		// Some arguments have files/directories that must be
		// relative to the current working directory or be
		// an absolute path for clang.
		// (b/157729681) This is a placeholder fix for flags not
		// using the provided working directory.
		if _, present := toAbsArgs[key]; present {
			if !filepath.IsAbs(value) {
				value = filepath.Join(directory, value)
			} else {
				value = filepath.Clean(value)
			}
		}
		if _, present := toRemoveArgs[key]; present {
			continue
		}
		args = appendFlag(args, flag.OriginalKey(), value, flag.Joined)
	}

	// When the input processor fails, we log the error and the error
	// output from clang-scan-deps has too much clutter about unused
	// arguments for assembly actions. Hence supress them.
	args = append(args, "-Qunused-arguments")

	for _, v := range p.Flags.OutputFilePaths {
		if v != p.Flags.EmittedDependencyFile {
			args = appendFlag(args, outputFlag, v, outputFlagJoined)
		}
	}
	args = append(args, filename)
	return args
}

// SaveDeps adjusts all header paths to be absolute and saves them in the deps cache. This should be called in a go routine
func (p *Preprocessor) saveDeps(key depscache.Key, headerInputFiles []string) {
	absInputFiles := make([]string, len(headerInputFiles))
	for i, path := range headerInputFiles {
		if filepath.IsAbs(path) {
			absInputFiles[i] = path
			continue
		}
		absInputFiles[i] = filepath.Join(p.Flags.ExecRoot, "", path)
	}
	if err := p.DepsCache.SetDeps(key, absInputFiles); err != nil {
		log.Errorf("SetDeps(%v) failed: %v", key, err)
	}
	if p.testOnlySetDone != nil {
		p.testOnlySetDone()
	}
}

func (p *Preprocessor) getFromDepsCache(key depscache.Key) ([]string, bool) {
	from := time.Now()
	evt := event.InputProcessorCacheLookup
	deps, ok := p.DepsCache.GetDeps(key)
	if ok {
		log.V(1).Infof("Found Deps Cache Hit for %v", key)
		if p.Rec == nil {
			p.Rec = logger.NewLogRecord()
		}
		p.Rec.RecordEventTime(evt, from)
		return deps, true
	}
	return nil, false
}

func appendFlag(slice []string, key, value string, joined bool) []string {
	if joined {
		return appendNonEmpty(slice, key+value)
	}
	return appendNonEmpty(appendNonEmpty(slice, key), value)
}

func appendNonEmpty(slice []string, value string) []string {
	if value != "" {
		slice = append(slice, value)
	}
	return slice
}

func extractVirtualSubdirectories(path string) []string {
	viFn := func() (interface{}, error) {
		var vis []string
		cpath := ""
		lastElem := ""
		for _, elem := range strings.Split(path, string(filepath.Separator)) {
			// A 'go back' has been hit, handle the various cases.
			if elem == ".." {
				// We've hit the first '..' in a possible sequence of '..'.
				// But we are at a directory that needs to be a virtual input.
				if lastElem != "" && lastElem != ".." {
					vis = append(vis, filepath.Clean(cpath))
				}
			}
			cpath = filepath.Join(cpath, elem)
			lastElem = elem
		}
		// cpath needs to be added as a virtual input, only if lastElem isn't '..' or there's
		// nothing in vis.  (Still needs adding if cpath is just full of '..' elements)
		if lastElem != ".." || len(vis) == 0 {
			vis = append(vis, filepath.Clean(cpath))
		}
		return vis, nil
	}
	val, err := virtualInputCache.LoadOrStore(path, viFn)
	if err != nil {
		log.Errorf("failed to process include directory path for virtual inputs %v", path)
		return []string{path}
	}
	return val.([]string)
}

// VirtualInputs returns paths extracted from virtualInputFlags. If paths are absolute, they're transformed to working dir relative.
func VirtualInputs(f *flags.CommandFlags, vip VirtualInputsProcessor) []*command.VirtualInput {
	var res []*command.VirtualInput
	for _, flag := range f.Flags {
		if vip.IsVirtualInput(flag.Key) {
			path := flag.Value
			if path == "" {
				log.Warningf("Invalid flag specification, missing value after flag %s", flag.Key)
				continue
			}
			if filepath.IsAbs(path) {
				path, _ = filepath.Rel(filepath.Join(f.ExecRoot, f.WorkingDirectory), path)
			}
			path = filepath.Join(f.WorkingDirectory, path)
			for _, p := range extractVirtualSubdirectories(path) {
				res = vip.AppendVirtualInput(res, flag.Key, p)
			}
		}
	}
	return res
}

// IsVirtualInput returns true if the flag specifies a virtual input to be added to InputSpec.
func (p *Preprocessor) IsVirtualInput(flag string) bool {
	return virtualInputFlags[flag]
}

// AppendVirtualInput appends a virtual input to res.
func (p *Preprocessor) AppendVirtualInput(res []*command.VirtualInput, flag, path string) []*command.VirtualInput {
	return append(res, &command.VirtualInput{Path: path, IsEmptyDirectory: true})
}

func (p *Preprocessor) addResourceDir(args []string) []string {
	for _, arg := range args {
		if arg == "-resource-dir" {
			return args
		}
	}
	resourceDir := p.resourceDir(args)
	if resourceDir != "" {
		return append(args, "-resource-dir", resourceDir)
	}
	return args
}

func getObjFilePath(fname string) string {
	return strings.TrimSuffix(fname, filepath.Ext(fname)) + ".o"
}

func (p *Preprocessor) resourceDir(args []string) string {
	return p.ResourceDir(args, "-print-resource-dir", func(stdout string) (string, error) {
		return strings.TrimSpace(stdout), nil
	})
}

// StdoutToResourceDirMapper maps clang output to resourceDir
type StdoutToResourceDirMapper func(string) (string, error)

// ResourceDir extracts clang command and runs it with provided resource the resourceDirFlag
// to return resource directory relative to the clang
// it maps the command output to resourceDir with provided stdoutToResourceDirMapper argument
// cache the results for reuse.
// It returns resource directory path associated with the give invocation command.
func (p *Preprocessor) ResourceDir(args []string, resourceDirFlag string, stdoutToResourceDirMapper StdoutToResourceDirMapper) string {
	if len(args) == 0 {
		return ""
	}
	if !filepath.IsAbs(args[0]) {
		return ""
	}
	resourceDirsMu.Lock()
	defer resourceDirsMu.Unlock()
	if p.Executor == nil {
		log.Errorf("Executor is not set in Preprocessor")
		return ""
	}
	fi, err := os.Stat(args[0])
	if err != nil {
		log.Errorf("Failed to access %s: %v", args[0], err)
		return ""
	}
	ri, ok := resourceDirs[args[0]]
	if ok {
		if os.SameFile(ri.fileInfo, fi) {
			return ri.resourceDir
		}
		log.Infof("%s seems to be updated: %v -> %v", args[0], ri.fileInfo, fi)
	}
	stdout, stderr, err := p.Executor.Execute(p.Ctx, &command.Command{
		Args:       []string{args[0], resourceDirFlag},
		WorkingDir: "/",
	})
	if err != nil {
		log.Warningf("failed to execute \"%v %v\" : %v\nstdout:%s\nstderr:%s", args[0], resourceDirFlag, err, stdout, stderr)
		return ""
	}
	resourceDir, err := stdoutToResourceDirMapper(stdout)
	if err != nil {
		log.Warning(err)
		return resourceDir
	}
	resourceDirs[args[0]] = resourceDirInfo{
		fileInfo:    fi,
		resourceDir: resourceDir,
	}
	return resourceDir
}

func maybeWithTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}
