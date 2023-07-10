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

// Package clanglink performs include processing given a valid clang link action.
package clanglink

import (
	"fmt"
	"team/foundry-x/re-client/internal/pkg/inputprocessor"
)

// Preprocessor is the preprocessor of clang cpp link actions.
type Preprocessor struct {
	*inputprocessor.BasePreprocessor
	ARDeepScan bool
}

// ParseFlags parses the commands flags and populates the ActionSpec object with inferred
// information.
func (p *Preprocessor) ParseFlags() error {
	f, err := parseFlags(p.Ctx, p.Options.Cmd, p.Options.WorkingDir, p.Options.ExecRoot, p.ARDeepScan)
	if err != nil {
		p.Err = fmt.Errorf("flag parsing failed. %v", err)
		return p.Err
	}
	p.Flags = f
	p.FlagsToActionSpec()
	return nil
}
