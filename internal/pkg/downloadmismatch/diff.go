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

// Package downloadmismatch downloads compare build mismatch outputs.
package downloadmismatch

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	stringsSuffix = ".strings"
	diffFileName  = "compare_action.diff"
)

func runAndWriteOutput(outFp string, cmd *exec.Cmd) error {
	cmd.Env = append(os.Environ())
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &out

	// Ignore the command return code, non-zero code from e.g. diff is expected.
	cmd.Run()

	if stderr.Len() > 0 {
		fmt.Printf("%v stderr: %q\n", cmd.String(), stderr.String())
	}
	return os.WriteFile(outFp, out.Bytes(), os.ModePerm)
}

func linuxStrings(fp string) (string, error) {
	outFp := fp + stringsSuffix
	return outFp, runAndWriteOutput(outFp, exec.Command("strings", fp))
}

// DiffReadableString diffs the strings outputs of the two inputs and store the diff outputs in outFp.
func DiffReadableString(outFp, fp1, fp2 string) error {
	fp1Strings, err1 := linuxStrings(fp1)
	fp2Strings, err2 := linuxStrings(fp2)
	if err1 != nil {
		fmt.Printf("error reading %v: %v", fp1, err1)
		return err1
	}
	if err2 != nil {
		fmt.Printf("error reading %v: %v", fp1, err1)
		return err2
	}
	return runAndWriteOutput(outFp, exec.Command("diff", fp1Strings, fp2Strings))
}

// Visit each action directory and diff remote vs local outputs. If one mismatch has multiple retries,
// we only diff the first remote vs first local output.
func visitAction(curPath string) error {
	f, err := os.Open(curPath)
	if err != nil {
		return err
	}
	outputs, err := f.Readdir(0)
	if err != nil {
		return err
	}
	for _, output := range outputs {
		// TODO: output can be directories if we modify
		// download.go to download directories.
		if err := visitOutput(filepath.Join(curPath, output.Name())); err != nil {
			return err
		}
	}
	return nil
}

func visitOutput(outputPath string) error {
	localOutputs, err := os.ReadDir(filepath.Join(outputPath, LocalOutputDir))
	if err != nil {
		return err
	}
	remoteOutputs, err := os.ReadDir(filepath.Join(outputPath, RemoteOutputDir))
	if err != nil {
		return err
	}
	if len(localOutputs) != 1 || len(remoteOutputs) != 1 {
		return fmt.Errorf("Missing or more than 1 local/remote output")
	}

	DiffReadableString(filepath.Join(outputPath, diffFileName), filepath.Join(outputPath, LocalOutputDir, localOutputs[0].Name()), filepath.Join(outputPath, RemoteOutputDir, remoteOutputs[0].Name()))
	return nil
}

// DiffOutputDir visits the directory storing compare build outputs downloaded by this package.
func DiffOutputDir(outputDir string) error {
	dirs, err := os.ReadDir(outputDir)
	if err != nil {
		return err
	}

	for _, f := range dirs {
		if f.IsDir() {
			curPath := filepath.Join(outputDir, f.Name())
			if err := visitAction(curPath); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("Error iterating output Directory, %s is not directory", f.Name())
		}
	}
	return nil
}
