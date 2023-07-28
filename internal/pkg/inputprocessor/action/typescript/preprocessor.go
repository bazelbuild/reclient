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

package typescript

import (
	"fmt"
	"path/filepath"

	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor"
	"github.com/bazelbuild/reclient/internal/pkg/pathtranslator"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
)

// Preprocessor is the preprocessor of typescript compile actions.
type Preprocessor struct {
	*inputprocessor.BasePreprocessor
}

// ComputeSpec computes any further action specification that is not immediately inferrable
// from flags or toolchain configuration.
func (p *Preprocessor) ComputeSpec() error {
	if len(p.Options.Cmd) < 2 {
		err := fmt.Errorf("insufficient number of arguments in command: %v", p.Options.Cmd)
		p.Err = err
		return err
	}
	targetPath := filepath.Join(p.Options.ExecRoot, p.Options.WorkingDir, p.Options.Cmd[1])
	tsprocessor, err := ProcessInputs(targetPath)
	if err != nil {
		p.Err = err
		return err
	}
	inputs, err := tsprocessor.Inputs()
	if err != nil {
		p.Err = err
		return err
	}
	inputs = pathtranslator.ListRelToExecRoot(p.Options.ExecRoot, p.Options.WorkingDir, inputs)
	outputs, err := tsprocessor.Outputs()
	if err != nil {
		p.Err = err
		return err
	}
	outfiles := pathtranslator.ListRelToExecRoot(p.Options.ExecRoot, p.Options.WorkingDir, outputs.Outfiles)
	// The SDK sorts files, so do it in test instead of during input discovery.
	s := &inputprocessor.ActionSpec{
		InputSpec: &command.InputSpec{
			Inputs: inputs,
		},
		OutputFiles: outfiles,
	}
	p.AppendSpec(s)
	return nil
}
