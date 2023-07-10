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
	"errors"
	"path/filepath"
	"strings"
)

// Tsinput is the interface for inputs of typescript.
type Tsinput interface {
	Path() string
	Inputs() ([]Tsinput, error)
}

// Tsoutput is the struct for output files and directories.
// Only Outfiles are supported for now.
type Tsoutput struct {
	Outdir   string
	Outfiles []string
}

// TsinputProcessor is used to find deps recursively from Tsinputs.
type TsinputProcessor struct {
	// Paths is a list of paths for inputs.
	Paths map[string]bool
	// Tsinputs is a list of Tsinput, whose deps are to be processed.
	Tsinputs []Tsinput
}

// Inputs returns a list of path of dependencies for t.Tsinputs.
func (t *TsinputProcessor) Inputs() ([]string, error) {
	// Find inputs until no Tsinput is in the list
	for len(t.Tsinputs) > 0 {
		newTsinputs := []Tsinput{}
		for _, tsinput := range t.Tsinputs {
			if _, exists := t.Paths[tsinput.Path()]; exists {
				continue
			}
			t.Paths[tsinput.Path()] = true
			directDeps, err := tsinput.Inputs()
			if err != nil {
				return nil, err
			}
			newTsinputs = append(newTsinputs, directDeps...)
		}
		t.Tsinputs = newTsinputs
	}
	result := make([]string, 0, len(t.Paths))
	for p := range t.Paths {
		result = append(result, p)
	}
	return result, nil
}

// Outputs returns Tsoutput struct, containing the output files from the inputs.
func (t *TsinputProcessor) Outputs() (*Tsoutput, error) {
	if t.Paths == nil {
		return nil, errors.New("invalid TsinputProcessor")
	}
	outfiles := []string{}
	for file := range t.Paths {
		// Output only files with extension ".ts" not ".d.ts"
		if ext := filepath.Ext(file); ext == ".ts" && !strings.Contains(file, ".d.ts") {
			outfile := strings.Replace(file, ".ts", ".js", 1)
			outfiles = append(outfiles, outfile)
		}
	}
	output := &Tsoutput{
		Outfiles: outfiles,
	}
	return output, nil
}

// ProcessInputs returns a TsInputProcessor, if the path points to a tsfile or
// a tsconfig, returns error otherwise.
func ProcessInputs(path string) (*TsinputProcessor, error) {
	var initialInput Tsinput
	var err error
	// Determine if path points to a tsfile or tsconfig.json based on its extension.
	// Path must points to a file, it fails otherwise.
	switch ext := filepath.Ext(path); ext {
	case ".json":
		initialInput, err = parseTsconfig(path)
	case ".ts", ".tsx":
		initialInput, err = parseTsfile(path)
	default:
		err = errors.New("unrecognized file extension")
	}
	if err != nil {
		return nil, err
	}
	tsinputProcessor := &TsinputProcessor{
		Tsinputs: []Tsinput{
			initialInput,
		},
		Paths: make(map[string]bool),
	}
	return tsinputProcessor, nil
}
