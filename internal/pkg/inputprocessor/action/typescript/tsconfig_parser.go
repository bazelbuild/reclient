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
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/karrick/godirwalk"
)

// Tsconfig encapusulates configuration in tsconfig.json for a Typescript project.
type Tsconfig struct {
	// Extends is the parent tsconfig to be extended.
	Extends string `json:extends`
	// Exclude is the list of files to be excluded.
	Exclude []string `json:exclude`
	// Files is the list of files to be included.
	Files []string `json:files`
	// Include is the list of directories to be included.
	Include []string `json:include`
	// References is list of project references.
	References []Reference `json:references`
	// PaTsPathth is the path to this tsconfig.json file.
	TsPath string
}

// Reference defines the path to a project reference.
type Reference struct {
	// Path is the path to the project reference.
	Path string `json:path`
}

// parseTsconfig parses the tsconfig.json pointed to by Path.
func parseTsconfig(path string) (*Tsconfig, error) {
	tsconfig := &Tsconfig{
		TsPath: path,
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(content, tsconfig); err != nil {
		return nil, err
	}
	if tsconfig.Extends != "" {
		parent, err := parseTsconfig(filepath.Join(filepath.Dir(tsconfig.Path()), tsconfig.Extends))
		if err != nil {
			return nil, err
		}
		tsconfig.extend(parent)
	}

	// Include by default is [] if Files is non-empty, otherwise ["."]
	if (tsconfig.Include == nil || len(tsconfig.Include) == 0) && (tsconfig.Files == nil || len(tsconfig.Files) == 0) {
		tsconfig.Include = []string{"."}
	}
	return tsconfig, nil
}

// extend overrides the parent Tsconfig object with this Tsconfig object.
func (t *Tsconfig) extend(parent *Tsconfig) {
	if parent == nil {
		return
	}
	if t.Exclude == nil && parent.Exclude != nil {
		t.Exclude = parent.Exclude
	}
	if t.Files == nil && parent.Files != nil {
		t.Files = parent.Files
	}
	if t.Include == nil && parent.Include != nil {
		t.Include = parent.Include
	}
}

// Path returns the path to this tsconfig.
func (t *Tsconfig) Path() string {
	return t.TsPath
}

// Inputs returns a list of Tsinput specified in the Files, Include, and
// References section of this tsconfig.json.
func (t *Tsconfig) Inputs() ([]Tsinput, error) {
	inputs := []Tsinput{}
	TsconfigDir := filepath.Dir(t.Path())
	// Files section
	for _, file := range t.Files {
		path := filepath.Join(TsconfigDir, file)
		tsfile, err := parseTsfile(path)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, tsfile)
	}

	// Include section
	excludePatterns := []string{}
	for _, pattern := range t.Exclude {
		excludePatterns = append(excludePatterns, filepath.Join(t.Path(), pattern))
	}
	for _, dir := range t.Include {
		searchPath := filepath.Join(TsconfigDir, dir)
		err := godirwalk.Walk(searchPath, &godirwalk.Options{
			Callback: func(path string, de *godirwalk.Dirent) error {
				// Skips directories
				if de.IsDir() {
					return nil
				}
				// TODO(b/195338557): filepath.Match cannot handle `**`, which matches any number of
				// subdirectories.
				// Skips this file is matches with Exclude.
				if ext := filepath.Ext(path); ext == ".ts" || ext == ".tsx" {
					for _, excludePattern := range excludePatterns {
						matched, err := filepath.Match(excludePattern, path)
						if err != nil {
							return err
						}
						if matched {
							return nil
						}
					}
					tsfile, err := parseTsfile(path)
					if err != nil {
						return err
					}
					inputs = append(inputs, tsfile)
				}
				return nil
			}})
		if err != nil {
			return nil, err
		}
	}

	// References section
	for _, ref := range t.References {
		path := filepath.Join(TsconfigDir, ref.Path)
		// If ref is a directory, then include the `tsconfig.json` under this directory.
		if ext := filepath.Ext(ref.Path); ext != ".json" {
			path = filepath.Join(path, "tsconfig.json")
		}
		tsconfig, err := parseTsconfig(path)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, tsconfig)
	}
	return inputs, nil
}
