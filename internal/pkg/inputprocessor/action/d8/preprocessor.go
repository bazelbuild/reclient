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

package d8

import (
	"fmt"

	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
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

// ComputeSpec computes ActionSpec for the options passed to the context.
func (p *Preprocessor) ComputeSpec() error {
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
