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

// Package labels has helper functions for conditionally supporting functionality based
// on the command's labels.
package labels

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/cache"

	log "github.com/golang/glog"
)

const (
	actionType = "type"
	compiler   = "compiler"
	lang       = "lang"
	actionTool = "tool"
	toolName   = "toolname"

	// Action types

	// Compile is a compile action.
	Compile = "compile"
	// Tool is an action that invokes a generic tool.
	Tool = "tool"
	// Link denotes the C++ link action type label.
	Link = "link"
	// Archive denotes the C++ archive action type label.
	Archive = "archive"
	// AbiDump is a header abi dump action.
	AbiDump = "abi-dump"
	// Lint is a code linting action.
	Lint = "lint"
	// APKSigning is an action that's used to digitally sign Android APKs.
	APKSigning = "apksigning"

	// Compilers

	// Clang for compiling c/c++.
	Clang = "clang"
	// ClangCL for compiling c/c++ with clang-cl (different flag semantics)
	ClangCL = "clang-cl"
	// NaCl for compiling c/c++ with nacl clang (different flag semantics)
	NaCl = "nacl"
	// Javac for compiling java files.
	Javac = "javac"
	// R8 for compiling class files into dex files with further optimizations.
	R8 = "r8"
	// D8 for compiling class files into dex files.
	D8 = "d8"
	// Metalava for compiling documentation of java source files.
	Metalava = "metalava"
	// HeaderAbiDumper for generating docs for C++.
	HeaderAbiDumper = "header-abi-dumper"
	// ClangTidy for C++ linting.
	ClangTidy = "clang-tidy"
	// SignAPKJAR for signing apk files in Android.
	SignAPKJAR = "signapkjar"
	// TsCompiler for compiling typescript files.
	TsCompiler = "tsc"

	// Languages

	// Cpp indicates the language is c++.
	Cpp = "cpp"
	// Java indicates the language is java.
	Java = "java"
	// Ts indicates the language is typescript.
	Ts = "typescript"

	// Action Tools

	// ClangTool indicates that the tool being used for this action is clang.
	// This is currently used for link actions, where clang++ is used as a driver
	// to call underlying ld linker.
	ClangTool = "clang"
	// LLVMTool indicates that the tool being used for this action is llvm.
	// This is currently used for archive actions, where llvm-ar is used.
	LLVMTool = "llvm"
)

var (
	labelsDigestCache = cache.SingleFlight{}
)

// Labels encapsulates common labels used to identify actions.
type Labels struct {
	// ActionType is the type of the action.
	ActionType string
	// Compiler is the compiler used in a compile action.
	Compiler string
	// Lang is the code language.
	Lang string
	// ActionTool refers to a generic tool used for the action.
	ActionTool string
}

// ToolLabels is the set of labels for running a generic tool.
func ToolLabels() Labels {
	return Labels{ActionType: Tool}
}

// ClangCppLabels is the set of labels identifying a cpp compile with clang.
func ClangCppLabels() Labels {
	return Labels{
		ActionType: Compile,
		Compiler:   Clang,
		Lang:       Cpp,
	}
}

// ClangCLCppLabels is the set of labels identifying a cpp compile with clang-cl.
func ClangCLCppLabels() Labels {
	return Labels{
		ActionType: Compile,
		Compiler:   ClangCL,
		Lang:       Cpp,
	}
}

// NaClLabels is the set of labels identifying a cpp compile with native client compilers.
func NaClLabels() Labels {
	return Labels{
		ActionType: Compile,
		Compiler:   NaCl,
		Lang:       Cpp,
	}
}

// ClangLinkLabels is the set of labels identifying a cpp link with clang driver.
func ClangLinkLabels() Labels {
	return Labels{
		ActionType: Link,
		ActionTool: ClangTool,
	}
}

// NaClLinkLabels is the set of labels identifying a cpp link with native client.
func NaClLinkLabels() Labels {
	return Labels{
		ActionType: Link,
		ActionTool: NaCl,
	}
}

// LLVMArLabels is the set of labels identifying a cpp archive with llvm driver.
func LLVMArLabels() Labels {
	return Labels{
		ActionType: Archive,
		ActionTool: LLVMTool,
	}
}

// ClangLintLabels is the set of labels identifying a clang-tidy action.
func ClangLintLabels() Labels {
	return Labels{
		ActionType: Lint,
		Lang:       Cpp,
		ActionTool: ClangTidy,
	}
}

// HeaderAbiDumpLabels is the set of labels identifying a header ABI dumper action.
func HeaderAbiDumpLabels() Labels {
	return Labels{
		ActionType: AbiDump,
		ActionTool: HeaderAbiDumper,
	}
}

// JavacLabels is the set of labels identifying a java compile with javac.
func JavacLabels() Labels {
	return Labels{
		ActionType: Compile,
		Compiler:   Javac,
		Lang:       Java,
	}
}

// R8Labels is the set of labels identifying an r8 command.
func R8Labels() Labels {
	return Labels{ActionType: Compile, Compiler: R8}
}

// D8Labels is the set of labels identifying a d8 command.
func D8Labels() Labels {
	return Labels{ActionType: Compile, Compiler: D8}
}

// MetalavaLabels is the set of labels identifying a metalava compile.
func MetalavaLabels() Labels {
	return Labels{
		ActionType: Compile,
		Compiler:   Metalava,
		Lang:       Java,
	}
}

// SignAPKLabels is the set of labels identifying a signapk command.
func SignAPKLabels() Labels {
	return Labels{
		ActionType: APKSigning,
		Compiler:   SignAPKJAR,
	}
}

// TscLabels is the set of labels identifying a typescript compile.
func TscLabels() Labels {
	return Labels{
		ActionType: Compile,
		Compiler:   TsCompiler,
		Lang:       Ts,
	}
}

// FromMap converts a map of labels to a Labels struct.
func FromMap(l map[string]string) Labels {
	res := Labels{}
	if v, ok := l[actionType]; ok {
		res.ActionType = v
	}
	if v, ok := l[compiler]; ok {
		res.Compiler = v
	}
	if v, ok := l[lang]; ok {
		res.Lang = v
	}
	if v, ok := l[actionTool]; ok {
		res.ActionTool = v
	}
	return res
}

// ToMap converts Labels to a map.
func ToMap(l Labels) map[string]string {
	res := map[string]string{}
	if l.ActionType != "" {
		res[actionType] = l.ActionType
	}
	if l.Compiler != "" {
		res[compiler] = l.Compiler
	}
	if l.Lang != "" {
		res[lang] = l.Lang
	}
	if l.ActionTool != "" {
		res[actionTool] = l.ActionTool
	}
	return res
}

// ToKey converts the labels to a flattened string in the form
// <key1>=<value1>,<key2>=<value2>,...
func ToKey(labels map[string]string) string {
	var ks []string
	for k, v := range labels {
		ks = append(ks, fmt.Sprintf("%s=%s", k, v))
	}
	sort.Strings(ks)
	return fmt.Sprintf("[%s]", strings.Join(ks, ","))
}

// ToDigest returns a SHA256 of the given labels.
//
// Digest of labels of various action types for quick reference:
// [compiler=clang,lang=cpp,type=compile]=1c2a12e4
// [compiler=clang-cl,lang=cpp,type=compile]=f1d5b747
// [tool=clang,type=link]=c8d5e900
// [lang=cpp,tool=clang-tidy,type=lint]=ef9b9dea
// [tool=header-abi-dumper,type=abi-dump]=480a02e7
// [compiler=javac,lang=java,type=compile]=2c59b32a
// [compiler=r8,type=compile]=fc0915a4
// [compiler=d8,type=compile]=4be622a4
// [compiler=metalava,lang=java,type=compile]=44779548
// [compiler=signapkjar,type=apksigning]=c5a25a91
// [type=tool]=8ea55c85
func ToDigest(l map[string]string) (string, error) {
	labelsKey := ToKey(l)
	valFn := func() (interface{}, error) {
		d := sha256.Sum256([]byte(labelsKey))
		h := hex.EncodeToString(d[:])[:8]
		log.V(1).Infof("Label digest mapping: %s=%s", labelsKey, h)
		return h, nil
	}
	val, err := labelsDigestCache.LoadOrStore(labelsKey, valFn)
	if err != nil {
		return "", fmt.Errorf("unable to digest label to key: %v", err)
	}
	return val.(string), nil
}
