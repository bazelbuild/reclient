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

package metalava

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/cache"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
)

var (
	// metalavaRE is a regular expression to find the version number of metalava.
	metalavaRE   = regexp.MustCompile(`^[\w\s]+:\s*(.+)`)
	versionCache = cache.SingleFlight{}
)

// Preprocessor is the context for processing metalava actions.
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
	err := p.verifyMetalavaVersion("1.3.0")
	if err != nil {
		p.Err = err
		return err
	}
	return nil
}

// verifyMetalavaVersion checks the version of a binary using the --version flag.
func (p *Preprocessor) verifyMetalavaVersion(want string) error {
	compute := func() (interface{}, error) {
		if p.Executor == nil {
			return "", fmt.Errorf("No executor passed to the toolchain input processor")
		}
		s, err := p.Spec()
		if err != nil {
			return "", err
		}
		stdout, _, err := p.Executor.Execute(p.Ctx, &command.Command{
			Args:       []string{p.Flags.ExecutablePath, "--version"},
			WorkingDir: filepath.Join(p.Options.ExecRoot, p.Options.WorkingDir),
			InputSpec: &command.InputSpec{
				EnvironmentVariables: s.InputSpec.EnvironmentVariables,
			},
		})
		if err != nil {
			return "", err
		}
		lines := strings.FieldsFunc(stdout, func(r rune) bool {
			return r == '\n' || r == '\r'
		})
		for _, l := range lines {
			matches := metalavaRE.FindSubmatch([]byte(l))
			if len(matches) > 1 {
				return string(matches[1]), nil
			}
		}
		return "", fmt.Errorf("Version check of %v produced unexpected std out: %v", p.Flags.ExecutablePath, stdout)
	}
	version, err := versionCache.LoadOrStore(p.Flags.ExecutablePath, compute)
	if err != nil {
		return err
	}
	if v, ok := version.(string); !ok || v != want {
		return fmt.Errorf("unexpected metalava version, want 1.3.0, got %v", version)
	}
	return nil
}
