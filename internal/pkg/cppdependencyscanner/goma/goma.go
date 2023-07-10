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

package includescanner

// Below CXXFLAGS are required to link against the goma input processor and are used in
// goma build configuration. Defines are required because goma links against a bundled
// version of libc++.

// #cgo CXXFLAGS: -std=c++14
// #cgo CXXFLAGS: -D_LIBCPP_ENABLE_NODISCARD -D_LIBCPP_HAS_NO_VENDOR_AVAILABILITY_ANNOTATIONS
// #cgo CXXFLAGS: -fno-exceptions -Wno-deprecated-declarations
// #cgo linux CXXFLAGS: -nostdinc++
// #cgo linux LDFLAGS: -static-libstdc++
// #cgo linux LDFLAGS: -Wl,--wrap=glob
// #cgo windows LDFLAGS: -Wl,-Bstatic,-lstdc++,-lwinpthread,-lssp,-Bdynamic
// #cgo windows LDFLAGS: -lws2_32
// #cgo windows LDFLAGS: -ldbghelp
// #cgo windows LDFLAGS: -lpsapi
// #cgo windows LDFLAGS: -static-libstdc++
// #include <stdlib.h>
// #include <stdint.h>
// #include "bridge.h"
import "C"
import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/cgo"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/filemetadata"

	"team/foundry-x/re-client/internal/pkg/logger"

	log "github.com/golang/glog"
)
import "C"

// Name of the include scanner.
const Name = "Goma"

// TODO(b/233275188): add unit test.

const (
	commandOutputMergeOutErr int = 0
	commandOutputOutOnly         = 1
	commandOutputInvalid         = 2
)

func isValidCommandOutputOption(commandOutputOption int) bool {
	return commandOutputOption < commandOutputInvalid
}

// DepsScanner wraps LLVM/Clang's scan deps scanner.
type DepsScanner struct {
	implNewFn      func(cacheDir, logDir string, cacheFileMaxMb int, useDepsCache bool) gomaImpl
	impl           gomaImpl
	cacheDir       string
	logDir         string
	cacheFileMaxMb int
	useDepsCache   bool
	l              *logger.Logger
	waitForReqsMu  sync.Mutex
	reqs           chan *processInputsRequest
	fmc            filemetadata.Cache
}

type processInputsRequest struct {
	execID         string
	compileCommand []string
	filename       string
	directory      string
	cmdEnv         []string
	res            chan<- processInputsResult
}

type processInputsResult struct {
	res       []string
	usedCache bool
	err       error
}

type gomaImpl interface {
	close()
	processInputs(req *processInputsRequest) error
}

type gomaCppImpl struct {
	impl unsafe.Pointer
}

func newGomaCppImpl(cacheDir, logDir string, cacheFileMaxMb int, useDepsCache bool) gomaImpl {
	processNameStr := C.CString("reproxy-gomaip")
	defer C.free(unsafe.Pointer(processNameStr))
	cacheDirStr := C.CString(cacheDir)
	defer C.free(unsafe.Pointer(cacheDirStr))
	logDirStr := C.CString(logDir)
	defer C.free(unsafe.Pointer(logDirStr))
	return &gomaCppImpl{
		impl: C.NewDepsScanner(processNameStr, cacheDirStr, logDirStr, C.int(cacheFileMaxMb), C.bool(useDepsCache)),
	}
}

// New creates new DepsScanner.
// The function's definition needs to be identical with the one in clangscandeps.go
// so reclient can be built with various dependency scanner implementations.
func New(fmc filemetadata.Cache, cacheDir, logDir string, cacheFileMaxMb int, _ []string, useDepsCache bool, l *logger.Logger) *DepsScanner {
	ds := &DepsScanner{
		l:              l,
		cacheDir:       cacheDir,
		logDir:         logDir,
		cacheFileMaxMb: cacheFileMaxMb,
		useDepsCache:   useDepsCache,
		implNewFn:      newGomaCppImpl,
		fmc:            fmc,
	}
	ds.init()
	return ds
}

func (ds *DepsScanner) waitForRequests() {
	ds.waitForReqsMu.Lock()
	defer ds.waitForReqsMu.Unlock()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ds.impl = ds.implNewFn(ds.cacheDir, ds.logDir, ds.cacheFileMaxMb, ds.useDepsCache)
	for req := range ds.reqs {
		ds.impl.processInputs(req)
	}
}

// Close releases resource associated with DepsScanner.
func (ds *DepsScanner) Close() {
	if ds.impl != nil {
		ds.impl.close()
	}
	close(ds.reqs)
}

func (g *gomaCppImpl) close() {
	C.Close(g.impl)
}

func (ds *DepsScanner) init() {
	ds.reqs = make(chan *processInputsRequest)
	go ds.waitForRequests()
}

// ProcessInputs runs clang-scan-deps on the given command and returns input files.
// Returns list of dependencies, boolean indicating whether deps cache was used, and
// error if exists.
func (ds *DepsScanner) ProcessInputs(ctx context.Context, execID string, compileCommand []string, filename, directory string, cmdEnv []string) ([]string, bool, error) {
	log.V(3).Infof("%v: Started Goma input processing for %q in %q", execID, filename, directory)
	resCh := make(chan processInputsResult, 1)
	req := &processInputsRequest{
		execID:         execID,
		compileCommand: compileCommand,
		filename:       filename,
		directory:      directory,
		cmdEnv:         cmdEnv,
		res:            resCh,
	}

	select {
	case ds.reqs <- req:
		log.V(3).Infof("%v: Sent the input processing request for %q. Waiting for the response",
			execID, filename)
	case <-ctx.Done():
		return nil, false, fmt.Errorf("failed to send the request: %w", ctx.Err())
	}

	// wait for the result to arrive on resCh
	// if ctx done before receiving the response, return error
	select {
	case result := <-resCh:
		return result.res, result.usedCache, result.err
	case <-ctx.Done():
		return nil, false, fmt.Errorf("failed to receive the response from dependency scanner: %w",
			ctx.Err())
	}
}

func (g *gomaCppImpl) processInputs(req *processInputsRequest) error {
	execIDStr := C.CString(req.execID)
	defer C.free(unsafe.Pointer(execIDStr))
	argsStr := newPCharArray(req.compileCommand)
	defer freePCharArray(argsStr)
	filenameStr := C.CString(req.filename)
	defer C.free(unsafe.Pointer(filenameStr))
	dirStr := C.CString(req.directory)
	defer C.free(unsafe.Pointer(dirStr))
	environ := newPCharArray(req.cmdEnv)
	defer freePCharArray(environ)
	h := cgo.NewHandle(req)

	exitCode := C.ScanDependencies(g.impl, execIDStr,
		C.int(len(req.compileCommand)), &argsStr[0], &environ[0], filenameStr, dirStr, C.uintptr_t(h))
	if exitCode != 0 {
		return fmt.Errorf("failed to queue dependencies request: exit=%d", exitCode)
	}
	return nil
}

//export gComputeIncludesDone
func gComputeIncludesDone(reqPtr C.uintptr_t, res *C.char, usedCache C.int, errString *C.char) {
	req := cgo.Handle(reqPtr).Value().(*processInputsRequest)
	if gErr := C.GoString(errString); gErr != "" {
		req.res <- processInputsResult{
			err: fmt.Errorf("input processing failed: %v", gErr),
		}
		return
	}
	req.res <- processInputsResult{
		res:       append(parse(C.GoString(res), req.directory), req.filename),
		usedCache: int(usedCache) != 0,
	}
}

// ShouldIgnorePlugin returns true if the plugin of given name should be ignored when passing compileCommand to the scanner
func (ds *DepsScanner) ShouldIgnorePlugin(_ string) bool {
	return false
}

func parse(deps string, dir string) []string {
	d := strings.Split(deps, ";")
	absDeps := make([]string, 0, len(d))
	for _, p := range d {
		if p == "" {
			continue
		}
		if filepath.IsAbs(p) {
			absDeps = append(absDeps, p)
			continue
		}
		absDeps = append(absDeps, filepath.Join(dir, p))
	}
	return absDeps
}

// newPCharArray returns array of C.char pointers that point to strings from the input
func newPCharArray(input []string) []*C.char {
	// initialized array has extra NULL element at the end
	arr := make([]*C.char, len(input)+1)
	for i, v := range input {
		arr[i] = C.CString(v)
	}
	return arr
}

// freePCharArray frees memory allocated for strings in the input array
func freePCharArray(arr []*C.char) {
	for i := range arr {
		C.free(unsafe.Pointer(arr[i]))
	}
}

func newStringSlice(cArray **C.char, len int) []string {
	cStringSlice := unsafe.Slice(cArray, len)
	stringSlice := make([]string, len)
	for i, cstring := range cStringSlice {
		stringSlice[i] = C.GoString(cstring)
	}
	return stringSlice
}

//export gReadCommandOutput
func gReadCommandOutput(prog *C.char, args **C.char, argsn C.int, env **C.char, envn C.int, cwd *C.char,
	commandOutputOption C.int, status *C.int) *C.char {
	output, exitCode := readCommandOutput(C.GoString(prog), newStringSlice(args, int(argsn)),
		newStringSlice(env, int(envn)), C.GoString(cwd), int(commandOutputOption))
	if status != nil {
		*status = C.int(exitCode)
	}
	return C.CString(output)
}

// readCommandOutput spawns a new process specified by arguments, waits until it finishes
// and returns the exit code and either stdout only (if commandOutputOption=commandOutputOutOnly)
// or combined stdout and stderr (if commandOutputOption=commandOutputMergeOutErr).
// The function calls must be behind the resource manager to avoid local resources exhaustion.
// If the function is called by gReadCommandOutput, it's a part of Preprocessor.FindDependencies
// execution chain which reserves the local resources before starting the execution.
func readCommandOutput(prog string, args, env []string, cwd string, commandOutputOption int) (string, int) {
	log.V(1).Infof("readCommandOutput: prog=%v, argv=%v, env=%v, cwd=%v, commandOutputOption=%v",
		prog, args, env, cwd, commandOutputOption)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = cwd
	cmd.Env = env

	var output []byte
	var err error
	exitCode := 0

	if !isValidCommandOutputOption(commandOutputOption) {
		log.Warningf("readCommandOutput called with invalid commandOutputOption=%v, defaulting to %v",
			commandOutputOption, commandOutputMergeOutErr)
		commandOutputOption = commandOutputMergeOutErr
	}

	switch commandOutputOption {
	case commandOutputMergeOutErr:
		output, err = cmd.CombinedOutput()
	case commandOutputOutOnly:
		output, err = cmd.Output()
	}

	if err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			exitCode = exiterr.ExitCode()
		} else {
			log.Errorf("readCommandOutput failed with '%v' error. prog: %v, args: %v, env: %v, cwd: %v, output: %v",
				err, prog, args, env, cwd, string(output))
			exitCode = 1
		}
	}

	return string(output), exitCode
}
