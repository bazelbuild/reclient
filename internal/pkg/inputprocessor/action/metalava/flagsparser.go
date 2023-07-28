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
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/args"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/flags"
	"github.com/bazelbuild/reclient/internal/pkg/rsp"
)

var (
	depFlags = map[string]bool{
		"--source-files":                         true,
		"--source-path":                          true,
		"-sourcepath":                            true,
		"-classpath":                             true,
		"-bootclasspath":                         true,
		"--merge-qualifier-annotations":          true,
		"--merge-inclusion-annotations":          true,
		"--validate-nullability-from-list":       true,
		"--input-api-jar":                        true,
		"--manifest":                             true,
		"--subtract-api":                         true,
		"--register-artifact":                    true,
		"--check-compatibility:api:current":      true,
		"--check-compatibility:removed:current":  true,
		"--check-compatibility:api:released":     true,
		"--check-compatibility:removed:released": true,
		"--api-lint":                             true,
		"--migrate-nullness":                     true,
		"--annotation-coverage-of":               true,
		"--include-annotation-classes":           true,
		"--rewrite-annotations":                  true,
		"--apply-api-levels":                     true,
		"--baseline:api-lint":                    true,
		"--baseline:compatibility:released":      true,
	}

	outFileFlags = map[string]bool{
		"--api":                                    true,
		"--private-api":                            true,
		"--dex-api":                                true,
		"--private-dex-api":                        true,
		"--dex-api-mapping":                        true,
		"--removed-api":                            true,
		"--removed-dex-api":                        true,
		"--proguard":                               true,
		"--write-doc-stubs-source-list":            true,
		"--report-even-if-suppressed":              true,
		"--api-xml":                                true,
		"--write-class-coverage-to":                true,
		"--write-member-coverage-to":               true,
		"--extract-annotations":                    true,
		"--generate-api-levels":                    true,
		"--nullability-warnings-txt":               true,
		"--update-baseline:compatibility:released": true,
		"--update-baseline:api-lint":               true,
	}

	outDirFlags = map[string]bool{
		"--sdk-values":          true,
		"--stubs":               true,
		"--doc-stubs":           true,
		"--rewrite-annotations": true,
	}

	inOutFlags = map[string]bool{
		"--baseline":        true,
		"--update-baseline": true,
		"--merge-baseline":  true,
	}

	srcDestFlags = map[string]bool{
		"--convert-to-jdiff": true,
		"--convert-to-v1":    true,
		"--convert-to-v2":    true,
		"--copy-annotations": true,
	}

	srcSrcDestFlags = map[string]bool{
		"--convert-new-to-jdiff": true,
		"--convert-new-to-v2":    true,
	}

	metavalaFlags = map[string]int{}
)

func init() {
	// depFlags has no flag value.

	// srcDestFlags takes 2 values.
	for f := range srcDestFlags {
		metavalaFlags[f] = 2
	}
	// srcSrcDestFlags takes 3 values.
	for f := range srcSrcDestFlags {
		metavalaFlags[f] = 3
	}

	// inOutFlags, outFileFlags, outDirFlags take value
	for f := range inOutFlags {
		metavalaFlags[f] = 1
	}
	for f := range outFileFlags {
		metavalaFlags[f] = 1
	}
	for f := range outDirFlags {
		metavalaFlags[f] = 1
	}
	metavalaFlags["--android-jar-pattern"] = 1
	metavalaFlags["--strict-input-files"] = 1
	metavalaFlags["--strict-input-files:warn"] = 1
}

// parseFlags is used to transform a metalava command into a CommandFlags structure.
func parseFlags(ctx context.Context, command []string, workingDir, execRoot string) (*flags.CommandFlags, error) {
	numArgs := len(command)
	if numArgs < 2 {
		return nil, fmt.Errorf("insufficient number of arguments in command: %v", command)
	}

	res := &flags.CommandFlags{
		ExecutablePath:   command[0],
		WorkingDirectory: workingDir,
		ExecRoot:         execRoot,
	}
	s := args.Scanner{
		Args:  command[1:],
		Flags: metavalaFlags,
	}
	for s.HasNext() {
		flag, args, values, _ := s.Next()
		switch {
		case depFlags[flag] && s.HasNext() && s.LookAhead() == "":
			_, args, _, _ = s.Next()
			if flag == "-sourcepath" {
				res.VirtualDirectories = append(res.VirtualDirectories, args...)
				continue
			}
			value := args[0]
			var deps []string
			switch {
			case strings.Contains(value, ":"):
				deps = strings.Split(value, ":")
			case strings.Contains(value, ","):
				deps = strings.Split(value, ",")
			case strings.HasPrefix(value, "@"):
				deps = []string{value[1:]}
				rspDeps, err := rsp.Parse(filepath.Join(execRoot, workingDir, deps[0]))
				if err != nil {
					return nil, err
				}
				deps = append(deps, rspDeps...)
			default:
				deps = []string{value}
			}
			for _, d := range deps {
				// Exclude empty strings and . strings from dependencies.
				if d == "" || d == "." {
					continue
				}
				res.Dependencies = append(res.Dependencies, d)
			}
			continue
		case inOutFlags[flag]:
			res.Dependencies = append(res.Dependencies, values[0])
			res.OutputFilePaths = append(res.OutputFilePaths, values[0])
			continue
		case outFileFlags[flag]:
			res.OutputFilePaths = append(res.OutputFilePaths, values[0])
			continue
		case outDirFlags[flag]:
			res.OutputDirPaths = append(res.OutputDirPaths, values[0])
			continue
		case srcDestFlags[flag]:
			res.Dependencies = append(res.Dependencies, values[0])
			res.OutputFilePaths = append(res.OutputFilePaths, values[1])
			continue
		case srcSrcDestFlags[flag]:
			res.Dependencies = append(res.Dependencies, values[0], values[1])
			res.OutputFilePaths = append(res.OutputFilePaths, values[2])
			continue
		case flag == "--android-jar-pattern":
			res.Dependencies = append(res.Dependencies, strings.SplitN(values[0], "%", 2)[0])
			continue
		case flag == "--strict-input-files", flag == "--strict-input-files:warn":
			res.EmittedDependencyFile = values[0]
			res.OutputFilePaths = append(res.OutputFilePaths, values[0])
			continue
		case flag == "":
			if strings.HasPrefix(args[0], "@") {
				rspFile := args[0][1:]
				res.TargetFilePaths = append(res.TargetFilePaths, rspFile)
				rspDeps, err := rsp.Parse(filepath.Join(execRoot, workingDir, rspFile))
				if err != nil {
					return nil, err
				}
				res.Dependencies = append(res.Dependencies, rspDeps...)
				continue
			}
		}
		res.Flags = append(res.Flags, &flags.Flag{Value: args[0]})

	}
	return res, nil
}
