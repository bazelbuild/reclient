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

// Package inputprocessor is used to find non-obvious inputs for action types like C++ compile,
// Java compile, C++ link etc.
package inputprocessor

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"

	"team/foundry-x/re-client/internal/pkg/cppdependencyscanner"
	"team/foundry-x/re-client/internal/pkg/features"
	iproc "team/foundry-x/re-client/internal/pkg/inputprocessor"
	"team/foundry-x/re-client/internal/pkg/inputprocessor/action/archive"
	"team/foundry-x/re-client/internal/pkg/inputprocessor/action/clangcl"
	"team/foundry-x/re-client/internal/pkg/inputprocessor/action/clanglink"
	"team/foundry-x/re-client/internal/pkg/inputprocessor/action/clanglint"
	"team/foundry-x/re-client/internal/pkg/inputprocessor/action/cppcompile"
	"team/foundry-x/re-client/internal/pkg/inputprocessor/action/d8"
	"team/foundry-x/re-client/internal/pkg/inputprocessor/action/headerabi"
	"team/foundry-x/re-client/internal/pkg/inputprocessor/action/javac"
	"team/foundry-x/re-client/internal/pkg/inputprocessor/action/metalava"
	"team/foundry-x/re-client/internal/pkg/inputprocessor/action/nacl"
	"team/foundry-x/re-client/internal/pkg/inputprocessor/action/r8"
	"team/foundry-x/re-client/internal/pkg/inputprocessor/action/tool"
	"team/foundry-x/re-client/internal/pkg/inputprocessor/action/typescript"
	"team/foundry-x/re-client/internal/pkg/inputprocessor/depscache"
	"team/foundry-x/re-client/internal/pkg/labels"
	"team/foundry-x/re-client/internal/pkg/localresources"
	"team/foundry-x/re-client/internal/pkg/logger"

	ppb "team/foundry-x/re-client/api/proxy"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/cache"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/filemetadata"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/outerr"
	"golang.org/x/sync/semaphore"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// shallowLabel is the label key to indicate whether to use the shallow
	// input processor for the command.
	shallowLabel = "shallow"
)

var (
	// ErrIPTimeout is an error returned when IP action times out
	ErrIPTimeout = errors.New("Input Processor timeout")
	// shallowFallbackConfig denotes whether a specific action type, identified by a set of
	// labels, can fallback to shallow input processor or not, when their
	// primary input processor fails.
	// The default behaviour is to fallback to shallow mode if a set of labels are NOT present
	// in the following config.
	// If a CPP compile and remote execution strategy is specified, shallow fallback will be disabled.
	shallowFallbackConfig = map[labels.Labels]map[ppb.ExecutionStrategy_Value]bool{
		labels.HeaderAbiDumpLabels(): {ppb.ExecutionStrategy_UNSPECIFIED: false},
		labels.ClangLintLabels():     {ppb.ExecutionStrategy_UNSPECIFIED: false},
		labels.ClangCppLabels(): {ppb.ExecutionStrategy_REMOTE: false,
			ppb.ExecutionStrategy_REMOTE_LOCAL_FALLBACK: false},
		labels.ClangCLCppLabels(): {ppb.ExecutionStrategy_REMOTE: false,
			ppb.ExecutionStrategy_REMOTE_LOCAL_FALLBACK: false},
		labels.NaClLabels(): {ppb.ExecutionStrategy_REMOTE: false,
			ppb.ExecutionStrategy_REMOTE_LOCAL_FALLBACK: false},
	}
)

// Executor can run commands and retrieve their outputs.
type Executor interface {
	Execute(ctx context.Context, cmd *command.Command) (string, string, error)
	ExecuteInBackground(ctx context.Context, cmd *command.Command, oe outerr.OutErr, ch chan *command.Result) error
}

// InputProcessor retrieves the input spec for commands.
type InputProcessor struct {
	cppDepScanner   cppcompile.CPPDependencyScanner
	cppLinkDeepScan bool
	depScanTimeout  time.Duration
	executor        Executor
	resMgr          *localresources.Manager
	fmc             filemetadata.Cache
	depsCache       *depscache.Cache
	nfc             cache.SingleFlight
	fsc             cache.SingleFlight
	slots           *semaphore.Weighted

	// logger is a logger for input processor events that span a single reproxy run.
	logger *logger.Logger
}

type depsCacheMode int

const (
	noDepsCache depsCacheMode = iota
	reproxyDepsCache
	gomaDepsCache
)

// Options adds extra control for the input processor
type Options struct {
	EnableDepsCache             bool
	CacheDir                    string
	LogDir                      string
	DepsCacheMaxMb              int
	ClangDepsScanIgnoredPlugins []string
	CppLinkDeepScan             bool
	IPTimeout                   time.Duration
	DepsScannerAddress          string
	ProxyServerAddress          string
}

// NewInputProcessor creates a new input processor.
// Its resources are bound by the local resources manager.
func NewInputProcessor(ctx context.Context, executor Executor, resMgr *localresources.Manager, fmc filemetadata.Cache, l *logger.Logger, opt *Options) (*InputProcessor, func()) {
	depsCacheMode := getDepsCacheMode(opt.CacheDir, opt.EnableDepsCache)
	var ignoredPlugins []string
	if cppdependencyscanner.Type() == cppdependencyscanner.ClangScanDeps {
		ignoredPlugins = opt.ClangDepsScanIgnoredPlugins
	}
	depScanner := cppdependencyscanner.New(ctx, executor, fmc, opt.CacheDir, opt.LogDir, opt.DepsCacheMaxMb, ignoredPlugins, depsCacheMode == gomaDepsCache, l, opt.DepsScannerAddress, opt.ProxyServerAddress)
	ip := newInputProcessor(depScanner, opt.IPTimeout, opt.CppLinkDeepScan, executor, resMgr, fmc)
	cleanup := func() {}
	if depsCacheMode == reproxyDepsCache {
		ip.depsCache, cleanup = newDepsCache(fmc, opt.CacheDir)
	}
	return ip, func() {
		cleanup()
		depScanner.Close()
	}
}

func getDepsCacheMode(depsCacheDir string, enableDepsCache bool) depsCacheMode {
	if depsCacheDir == "" || !enableDepsCache {
		return noDepsCache
	}
	if cppdependencyscanner.Type() == cppdependencyscanner.ClangScanDeps {
		return reproxyDepsCache
	}
	if features.GetConfig().ExperimentalGomaDepsCache {
		return reproxyDepsCache
	}
	return gomaDepsCache
}

// NewInputProcessorWithStubDependencyScanner creates a new input processor with given parallelism
// and a stub CPP dependency scanner. It is meant to be only used for testing.
func NewInputProcessorWithStubDependencyScanner(ds cppcompile.CPPDependencyScanner, cppLinkDeepScan bool, executor Executor, resMgr *localresources.Manager) *InputProcessor {
	return newInputProcessor(ds, 0, cppLinkDeepScan, executor, resMgr, nil)
}

func newInputProcessor(ds cppcompile.CPPDependencyScanner, depScanTimeout time.Duration, cppLinkDeepScan bool, executor Executor, resMgr *localresources.Manager, fmc filemetadata.Cache) *InputProcessor {
	return &InputProcessor{
		cppDepScanner:   ds,
		cppLinkDeepScan: cppLinkDeepScan,
		depScanTimeout:  depScanTimeout,
		executor:        executor,
		resMgr:          resMgr,
		fmc:             fmc,
		slots:           semaphore.NewWeighted(int64(runtime.NumCPU())),
	}
}

func newDepsCache(fmc filemetadata.Cache, depsCacheDir string) (*depscache.Cache, func()) {
	dc := depscache.New(fmc)
	go dc.LoadFromDir(depsCacheDir)
	return dc, func() {
		dc.WriteToDisk(depsCacheDir)
	}
}

// SetLogger sets the logger on the input processor.
func (p *InputProcessor) SetLogger(l *logger.Logger) {
	p.logger = l
	if p.depsCache != nil {
		p.depsCache.Logger = l
	}
}

// ProcessInputsOptions encapsulates options for a ProcessInputs call.
type ProcessInputsOptions struct {
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

	// WindowsCross indicates whether use linux worker for Windows.
	WindowsCross bool

	// ExecStrategy indicates which execution strategy was used
	ExecStrategy ppb.ExecutionStrategy_Value

	// CmdEnvironment captures the environment of the command to be executed, in the form "key=value" strings.
	CmdEnvironment []string
}

// CommandIO encapsulates the inputs and outputs a command. All paths are relative to the
// exec root.
type CommandIO struct {
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

// ProcessInputs receives a valid action command and returns the set of inputs needed to
// successfully run the command remotely. Also returns a struct of parsed flags and the
// .d file produced by the command if exists.
func (p *InputProcessor) ProcessInputs(ctx context.Context, opts *ProcessInputsOptions, rec *logger.LogRecord) (*CommandIO, error) {
	st := time.Now()
	defer rec.RecordEventTime(logger.EventProcessInputs, st)
	lbls := labels.FromMap(opts.Labels)

	// We set shallow fallback based on the labels and execution strategy.
	shallowFallback := true
	if m, ok := shallowFallbackConfig[lbls]; ok {
		if s, ok := m[opts.ExecStrategy]; ok {
			shallowFallback = s
		}
	}
	// The code here is a temporary hack to make CLs easier to review. The entire input
	// processor package under pkg/ should be removed and replaced with
	// internal/pkg/inputprocessor, where there will be only one definition of
	// ProcessInputsOptions and CommandIO.
	options := iproc.Options{
		ExecutionID:     opts.ExecutionID,
		Cmd:             opts.Cmd,
		WorkingDir:      opts.WorkingDir,
		ExecRoot:        opts.ExecRoot,
		Inputs:          opts.Inputs,
		Labels:          opts.Labels,
		ToolchainInputs: opts.ToolchainInputs,
		ShallowFallback: shallowFallback,
		WindowsCross:    opts.WindowsCross,
	}
	var pp iproc.Preprocessor
	bp := &iproc.BasePreprocessor{Ctx: ctx,
		Executor:            p.executor,
		ResourceManager:     p.resMgr,
		FileMetadataCache:   p.fmc,
		NormalizedFileCache: &p.nfc,
		FileStatCache:       &p.fsc,
	}
	cp := &cppcompile.Preprocessor{
		BasePreprocessor: bp,
		CPPDepScanner:    p.cppDepScanner,
		Rec:              rec,
		DepsCache:        p.depsCache,
		CmdEnvironment:   opts.CmdEnvironment,
		DepScanTimeout:   p.depScanTimeout,
		Slots:            p.slots,
	}
	switch lbls {
	case labels.ToolLabels():
		pp = &tool.Preprocessor{
			BasePreprocessor: bp,
		}
	// SignAPKLabels is equivalent to ToolLabels, but
	// is kept for historical reasons and for distinction.
	case labels.SignAPKLabels():
		pp = &tool.Preprocessor{
			BasePreprocessor: bp,
		}
	case labels.D8Labels():
		pp = &d8.Preprocessor{
			BasePreprocessor: bp,
		}
	case labels.R8Labels():
		pp = &r8.Preprocessor{
			BasePreprocessor: bp,
		}
	case labels.MetalavaLabels():
		pp = &metalava.Preprocessor{
			BasePreprocessor: bp,
		}
	case labels.ClangCppLabels():
		pp = cp
	case labels.ClangLintLabels():
		pp = &clanglint.Preprocessor{
			Preprocessor: cp,
		}
	case labels.HeaderAbiDumpLabels():
		pp = &headerabi.Preprocessor{
			Preprocessor: cp,
		}
	case labels.ClangCLCppLabels():
		pp = &clangcl.Preprocessor{
			Preprocessor: cp,
		}
	case labels.NaClLabels():
		pp = &nacl.Preprocessor{
			Preprocessor: cp,
		}
	case labels.ClangLinkLabels():
		pp = &clanglink.Preprocessor{
			BasePreprocessor: bp,
			ARDeepScan:       p.cppLinkDeepScan,
		}
	case labels.NaClLinkLabels():
		pp = &clanglink.Preprocessor{
			BasePreprocessor: bp,
			ARDeepScan:       p.cppLinkDeepScan,
		}
	case labels.JavacLabels():
		pp = &javac.Preprocessor{
			BasePreprocessor: bp,
		}
	case labels.LLVMArLabels():
		pp = &archive.Preprocessor{
			BasePreprocessor: bp,
		}
	case labels.TscLabels():
		pp = &typescript.Preprocessor{
			BasePreprocessor: bp,
		}
	}
	if pp != nil {
		ch := make(chan bool)
		var res *iproc.ActionSpec
		var err error
		go func() {
			res, err = iproc.Compute(pp, options)
			// in a general sense ErrIPTimeout represents an error caused by IP execution
			// exceeding IPTimeout value (set by ip_timeout) flag; however,
			// at the moment IPTimeout is used only by cpp dependency scanner.
			// If, in the future, it will be used more widely, more error types might need to be
			// translated to ErrIPTimeout
			if errors.Is(err, cppdependencyscanner.ErrDepsScanTimeout) {
				err = fmt.Errorf("%w: %v", ErrIPTimeout, err)
			}
			close(ch)
		}()
		select {
		case <-ch:
			if err != nil {
				return nil, err
			}
			return &CommandIO{
				InputSpec:             res.InputSpec,
				OutputFiles:           res.OutputFiles,
				OutputDirectories:     res.OutputDirectories,
				EmittedDependencyFile: res.EmittedDependencyFile,
				UsedShallowMode:       res.UsedShallowMode,
			}, nil
		case <-ctx.Done():
			return nil, fmt.Errorf("context was cancelled before completing input processing")
		}

	}
	return nil, status.Errorf(codes.Unimplemented, "unsupported labels: %v", opts.Labels)
}
