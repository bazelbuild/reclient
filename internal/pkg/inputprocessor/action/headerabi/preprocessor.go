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

// Package headerabi performs include processing given a valid ABI header dump action.
package headerabi

import (
	"fmt"

	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/action/cppcompile"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/flags"
)

// Preprocessor is the preprocessor of header-abi-dumper actions.
type Preprocessor struct {
	*cppcompile.Preprocessor
}

// ParseFlags parses the commands flags and populates the ActionSpec object with inferred
// information.
func (p *Preprocessor) ParseFlags() error {
	f, err := Parser{}.ParseFlags(p.Ctx, p.Options.Cmd, p.Options.WorkingDir, p.Options.ExecRoot)
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
	p.Flags = headerABIToCppFlags(p.Flags)
	return p.Preprocessor.ComputeSpec()
}

func headerABIToCppFlags(f *flags.CommandFlags) *flags.CommandFlags {
	// Ignore --root-dir argument since it is specific to the header-abi-dumper
	// command and doesn't translate into a clang-argument.
	res := f.Copy()
	res.Flags = []*flags.Flag{}
	for i := 0; i < len(f.Flags); i++ {
		if f.Flags[i].Key == "--root-dir" {
			continue
		}
		res.Flags = append(res.Flags, f.Flags[i])
	}
	return res
}
