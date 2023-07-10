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

package reproxy

import (
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
)

func copyFiles(destDir, src string) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}
	r, err := os.Open(src)
	if err != nil {
		return err
	}
	defer r.Close()
	st, err := r.Stat()
	if err != nil {
		return err
	}
	if st.IsDir() {
		names, err := r.Readdirnames(-1)
		if err != nil {
			return err
		}
		for _, name := range names {
			err = copyFiles(filepath.Join(destDir, filepath.Base(src)), filepath.Join(src, name))
			if err != nil {
				return err
			}
		}
		return nil
	}
	w, err := os.Create(filepath.Join(destDir, filepath.Base(src)))
	if err != nil {
		return err
	}
	_, err = io.Copy(w, r)
	cerr := w.Close()
	if err != nil {
		return err
	}
	return cerr
}

func dumpInputs(cmd *command.Command) error {
	execPath, err := os.Executable()
	if err != nil {
		return err
	}
	prefix := filepath.Join(os.TempDir(), filepath.Base(execPath))
	for _, inp := range cmd.InputSpec.Inputs {
		destDir := filepath.Join(prefix, cmd.Identifiers.ExecutionID, path.Dir(inp))
		if err := copyFiles(destDir, inp); err != nil {
			return err
		}
	}
	for _, inp := range cmd.InputSpec.VirtualInputs {
		newDir := path.Dir(inp.Path)
		if inp.IsEmptyDirectory {
			newDir = inp.Path
		}
		destDir := filepath.Join(prefix, cmd.Identifiers.ExecutionID, newDir)
		if err := os.MkdirAll(destDir, os.FileMode(0777)); err != nil {
			return err
		}
		if inp.IsEmptyDirectory {
			continue
		}
		if err := os.WriteFile(filepath.Join(destDir, path.Base(inp.Path)), inp.Contents, os.FileMode(0777)); err != nil {
			return err
		}
	}
	return nil
}
