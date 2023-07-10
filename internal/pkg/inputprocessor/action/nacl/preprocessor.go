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

// Package nacl performs include processing given a valid nacl action.
package nacl

import (
	"fmt"
	"path/filepath"
	"strings"

	"team/foundry-x/re-client/internal/pkg/inputprocessor/action/cppcompile"
	"team/foundry-x/re-client/internal/pkg/inputprocessor/flags"
)

// Preprocessor is the preprocessor of nacl compile actions.
type Preprocessor struct {
	*cppcompile.Preprocessor
}

// ComputeSpec computes cpp header dependencies.
// As the versions of nacl that we work with are quite defased from our llvm
// version, we have to tweak the flags before treating them as a regular clang
// action.
func (p *Preprocessor) ComputeSpec() error {
	var hasTarget bool
	finalFlags := []*flags.Flag{}
	for _, flag := range p.Flags.Flags {
		if strings.HasPrefix(flag.Key, "-pnacl") || strings.HasPrefix(flag.Key, "--pnacl") || (flag.Key == "--" && strings.HasPrefix(flag.Value, "pnacl")) {
			// Chrome pnacl uses a custom clang compiler, pnacl-clang,
			// which supports flags that ClangScanDeps would never
			// support.
			continue
		}
		if flag.Key == "-target" || flag.Key == "--target=" {
			// See if !hasTarget below for more context.
			hasTarget = true
		}
		finalFlags = append(finalFlags, flag)
	}

	binaryName := filepath.Base(p.Flags.ExecutablePath)
	containsNacl := strings.Contains(binaryName, "nacl")
	if !containsNacl {
		return fmt.Errorf("nacl binary doesn't include 'nacl' in basename: %v", p.Flags.ExecutablePath)
	}

	if !hasTarget && containsNacl {
		// Nacl compilers target a specific architecture that's potentially different
		// than the host machine's architecutre, eg x86_64, i686 or mipsel. Those flavors have their
		// own defined macros, included headers and libraries. Our implementation of clang scan deps
		// sometimes is unable to find out that we're attempting a cross-compilation of sorts,
		// so we add a `--target` flag as a cheat for scan deps.
		//
		// In Chrome, NaCl binaries all follow the naming
		// {arch}-nacl-{tool}, eg x86_64-nacl-clang, mipsel-nacl-ld.
		// The exception being for pnacl, where the binaries are named pnacl-{tool}.
		// Under the hood, pnacl-clang uses the i686-nacl-clang.
		arch := strings.Split(binaryName, "nacl")
		if arch[0] != "p" {
			finalFlags = append(finalFlags, &flags.Flag{Key: "--target=", Value: fmt.Sprintf("%snacl", arch[0]), Joined: true})
		} else {
			// Under the hood, pnacl-clang is a python script that runs i686-nacl-clang.
			finalFlags = append(finalFlags, &flags.Flag{Key: "--target=", Value: "i686-nacl", Joined: true})
		}
	}

	p.Flags.Flags = finalFlags
	return p.Preprocessor.ComputeSpec()
}
