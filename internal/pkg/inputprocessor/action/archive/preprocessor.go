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

package archive

import (
	"fmt"
	"path/filepath"
	"strings"

	"team/foundry-x/re-client/internal/pkg/inputprocessor"
	"team/foundry-x/re-client/internal/pkg/inputprocessor/flags"
	"team/foundry-x/re-client/internal/pkg/rsp"
)

// Preprocessor is the preprocessor of archive link actions.
type Preprocessor struct {
	*inputprocessor.BasePreprocessor
}

const (
	opArchive = iota
	opPrint
	opDelete
)

// ParseFlags is used to parse the flags in an archive link invocation command.
func (p *Preprocessor) ParseFlags() error {
	if len(p.Options.Cmd) < 1 {
		return fmt.Errorf("insufficient number of arguments in command: %v", p.Options.Cmd)
	}

	p.Flags = &flags.CommandFlags{
		WorkingDirectory: p.Options.WorkingDir,
		ExecRoot:         p.Options.ExecRoot,
		ExecutablePath:   p.Options.Cmd[0],
	}

	// Warning: This is a NAIVE implementation. Expect edge cases to fail.
	// For instance:
	//
	// * It doesn't know how to parse flags grouped together (eg -abCfX).
	// * It doesn't know how to extract archives (to do that, it'd need to read them).
	//
	// See reference for `ar` commands: https://linux.die.net/man/1/ar
	nextArgs := p.Options.Cmd[1:]

	// Indicates that the archive is the input, not the ouput.
	operation := opArchive
	var archive string
	var objects []string

	for len(nextArgs) > 0 {
		arg := nextArgs[0]
		nextArgs = nextArgs[1:]

		switch {
		case strings.HasPrefix(arg, "-"):
			p.Flags.Flags = append(p.Flags.Flags, &flags.Flag{Value: arg})

			switch arg {
			case "-t":
				operation = opPrint
			case "-d":
				operation = opDelete
			}
		case strings.HasSuffix(arg, ".a"):
			archive = arg
		case strings.HasPrefix(arg, "@"):
			arg = arg[1:]
			objects = append(objects, arg)

			if !filepath.IsAbs(arg) {
				arg = filepath.Join(p.Flags.ExecRoot, p.Flags.WorkingDirectory, arg)
			}

			rspFlags, err := rsp.Parse(arg)
			if err != nil {
				return err
			}

			nextArgs = append(nextArgs, rspFlags...)
		default:
			objects = append(objects, arg)
		}
	}

	// For eg print or delete, there should be no other dependencies
	// besides the archive itself.
	//
	// (maybe printing shallow archives requires the contents of the archives?)
	switch operation {
	case opArchive:
		p.Flags.Dependencies = objects
		p.Flags.OutputFilePaths = []string{archive}
	case opDelete:
		p.Flags.Dependencies = []string{archive}
		p.Flags.OutputFilePaths = []string{archive}
	case opPrint:
		p.Flags.Dependencies = []string{archive}
	}

	p.FlagsToActionSpec()
	return nil
}
