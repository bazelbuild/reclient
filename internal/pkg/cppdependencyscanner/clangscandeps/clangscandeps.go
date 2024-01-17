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

// Package includescanner uses a tool from the LLVM project called
// [clang-scan-deps](https://github.com/llvm/llvm-project/tree/main/clang/tools/clang-scan-deps)
// in order to do the dependency determination. Since invoking the tool
// as a binary would be too expensive and in-order to share the in-memory
// cache of the tool across multiple compile commands, we build the necessary
// LLVM lib behind this tool and link via cgo.
package includescanner

// https://github.com/bazelbuild/bazel-gazelle/issues/870
// LDFLAGS -> clinkopts in go_library
// https://github.com/bazelbuild/rules_go/issues/2545
// -static-libstdc++ has no effect if invoked as clang.
// it removes -lstdc++ from ldflags.

// -Wl,--wrap is used for compatibility. See the comments on
// __exp2_compatible on bridge.cc.

// #cgo CXXFLAGS: -std=c++17
// #cgo CXXFLAGS: -Wall
// #cgo CXXFLAGS: -Werror
// #cgo CXXFLAGS: -Wno-range-loop-analysis
// #cgo CXXFLAGS: -Wno-deprecated
// #cgo CXXFLAGS: -fno-rtti
// #cgo linux LDFLAGS: -static-libgcc
// #cgo linux LDFLAGS: -static-libstdc++
// #cgo linux LDFLAGS: -Wl,-Bstatic,-ltinfo,-lstdc++,-Bdynamic
// #cgo linux LDFLAGS: -Wl,--wrap=exp2 -Wl,--wrap=pow -Wl,--wrap=log2f
// #cgo windows LDFLAGS: -lversion
// #cgo windows LDFLAGS: -lole32
// #cgo windows LDFLAGS: -luuid
// #cgo windows LDFLAGS: -static-libgcc
// #cgo windows LDFLAGS: -static-libstdc++
// #cgo windows LDFLAGS: -Wl,-Bstatic,-lz,-lstdc++,-lpthread,-lwinpthread,-Bdynamic
// #cgo darwin LDFLAGS: -lcurses
// #include <stdlib.h>
// #include "bridge.h"
import "C"
import (
	"context"
	"fmt"
	"strings"
	"unsafe"

	log "github.com/golang/glog"
)

// Name of the include scanner.
const Name = "ClangScanDeps"

// IsStub reflects that this is not a stub deps scanner.
const IsStub = false

// DepsScanner wraps LLVM/Clang's scan deps scanner.
type DepsScanner struct {
	impl unsafe.Pointer
}

type processInputsResult struct {
	res []string
	err error
}

// New creates new DepsScanner.
// The function's definition needs to be identical with the one in goma.go
// so reclient can be built with various dependency scanner implementations.
func New(_, _, _, _, _, _ any) *DepsScanner {
	return &DepsScanner{
		impl: C.NewDepsScanner(),
	}
}

// Close releases resource associated with DepsScanner.
func (ds *DepsScanner) Close() {
	C.DeleteDepsScanner(ds.impl)
}

// ProcessInputs runs clang-scan-deps on the given command and returns input files.
// If the provided context becomes done before clang-scan-deps execution is finished,
// the function returns nil as a result and a wrapped context's error.
// The boolean returned is always false since clangscandeps does not cache dependencies.
func (ds *DepsScanner) ProcessInputs(ctx context.Context, execID string, compileCommand []string, filename, directory string, _ []string) ([]string, bool, error) {
	log.V(3).Infof("%v: Started ClangScanDeps input processing for %v", execID, filename)
	resCh := make(chan processInputsResult)
	go func() {
		argsStr := make([]*C.char, len(compileCommand))
		for i, v := range compileCommand {
			argsStr[i] = C.CString(v)
			defer C.free(unsafe.Pointer(argsStr[i]))
		}
		filenameStr := C.CString(filename)
		defer C.free(unsafe.Pointer(filenameStr))
		dirStr := C.CString(directory)
		defer C.free(unsafe.Pointer(dirStr))
		var depsStr *C.char
		defer C.free(unsafe.Pointer(depsStr))
		var errsStr *C.char
		defer C.free(unsafe.Pointer(errsStr))

		exitCode := C.ScanDependencies(ds.impl,
			C.int(len(argsStr)), &argsStr[0], filenameStr, dirStr,
			&depsStr, &errsStr)

		piRes := processInputsResult{}
		if exitCode == 0 {
			deps := C.GoString(depsStr)
			log.V(3).Infof("ScanDependencies=%q", deps)
			piRes.res = parse(deps)
		} else {
			piRes.err = fmt.Errorf("failed to get dependencies: exit=%d %v", exitCode, C.GoString(errsStr))
		}
		select {
		case resCh <- piRes:
			return
		case <-ctx.Done():
			return
		}
	}()
	select {
	case resp := <-resCh:
		return resp.res, false, resp.err
	case <-ctx.Done():
		return nil, false, fmt.Errorf("failed to receive the response from dependency scanner: %w",
			ctx.Err())
	}
}

// SupportsCache implements DepsScanner.SupportsCache.
func (ds *DepsScanner) SupportsCache() bool {
	return false
}

func parse(deps string) []string {
	// deps contentes
	// <output>: <input> ...
	// <input> is space sparated
	// '\'+newline is space
	// '\'+space is espaced space (not separater)
	s := deps
	var token string
	// skip until ':'
	i := strings.IndexByte(s, ':')
	if i < 0 {
		return nil
	}
	s = s[i+1:]
	// collects inputs
	var res []string
	for {
		token, s = nextToken(s)
		if token != "" {
			res = append(res, token)
		}
		if s == "" { // end
			return res
		}
	}
}

func nextToken(s string) (string, string) {
	var sb strings.Builder
	// skip spaces
skipSpaces:
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && s[i+1] == '\n' {
			i++
			continue
		}
		switch s[i] {
		case ' ', '\t', '\n':
			continue
		default:
			s = s[i:]
			break skipSpaces
		}
	}
	// extract next space not escaped
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case ' ':
				sb.WriteByte(s[i])
			default:
				sb.WriteByte('\\')
				sb.WriteByte(s[i])
			}
			continue
		}
		switch s[i] {
		case ' ', '\t', '\n':
			return sb.String(), s[i+1:]
		}
		sb.WriteByte(s[i])
	}
	return sb.String(), ""
}
