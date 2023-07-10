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
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	importRE     = regexp.MustCompile(`^import.*['"].+['"]`)
	importPathRE = regexp.MustCompile(`['"].+['"]`)
)

// Tsfile is a struct for a single typescript file.
type Tsfile struct {
	// Imports is a list of imports in the typescript file.
	Imports []string
	// TsPath is the path to this typescript file.
	TsPath string
}

// parseTsfile parses the imports of the typescript file pointed to by path.
func parseTsfile(path string) (*Tsfile, error) {
	tsfile := &Tsfile{
		TsPath:  path,
		Imports: []string{},
	}
	f, err := os.Open(tsfile.Path())
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		// Check if this line is an import statement.
		match := importRE.FindString(line)
		if match == "" {
			continue
		}
		// Find the imported file.
		match = importPathRE.FindString(line)
		if match == "" {
			continue
		}
		importString := strings.Trim(match, `'"`)
		tsfile.Imports = append(tsfile.Imports, importString)
	}
	if err = scanner.Err(); err != nil {
		return nil, err
	}
	return tsfile, nil
}

// Path returns the path to this tsfile.
func (t *Tsfile) Path() string {
	return t.TsPath
}

// Inputs returns a list of files imported by this tsfile.
func (t *Tsfile) Inputs() ([]Tsinput, error) {
	inputs := []Tsinput{}
	suffixes := []string{
		".tsx",
		".ts",
		".d.ts",
		"/package.json",
		"/index.ts",
		"/index.tsx",
		"/index.d.ts",
	}
	TsfileDir := filepath.Dir(t.Path())
	for _, impt := range t.Imports {
		path := filepath.Join(TsfileDir, impt)
		// Includes its type definition, if no extension specified or javascript imports.
		if ext := filepath.Ext(impt); ext == "" || ext == ".js" {
			searchPrefix := strings.TrimSuffix(path, ext)
			found := false
			for _, suffix := range suffixes {
				searchPath := searchPrefix + suffix
				tsfile, err := parseTsfile(searchPath)
				// Skips this searchPath, if no file found or returned error.
				if err == nil {
					inputs = append(inputs, tsfile)
					found = true
					break
				}
			}
			// returns error if cannot find the imported file
			if !found {
				return nil, errors.New("failed to find dependency file: " + path)
			}
			continue
		}
		tsfile, err := parseTsfile(path)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, tsfile)
	}
	return inputs, nil
}
