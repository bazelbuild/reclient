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

package toolchain

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/filemetadata"
	"github.com/bazelbuild/rules_go/go/tools/bazel"
	"github.com/google/go-cmp/cmp"
)

func getCwd(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Unable to get current working directory: %v", err)
	}
	if runtime.GOOS != "windows" {
		return cwd
	}
	// On windows, runfiles are not available in current directory
	// by default.
	// Copy runfiles in temp directory.
	// https://github.com/bazelbuild/rules_go/issues/2491
	dir, err := bazel.NewTmpDir("toolchain_test.runfiles")
	if err != nil {
		t.Fatalf("tmpdir: %v", err)
	}
	t.Cleanup(func() {
		os.Chdir(cwd)
		os.RemoveAll(dir)
	})
	err = os.Chdir(dir)
	if err != nil {
		t.Fatalf("Unable to chdir to %s: %v", dir, err)
	}
	files, err := bazel.ListRunfiles()
	if err != nil {
		t.Fatalf("Unable to get runfiles: %v", err)
	}
	for _, f := range files {
		fileCopy(t, f.Path, f.ShortPath)
	}
	return filepath.Join(dir, "internal", "pkg", "inputprocessor", "toolchain")
}

func fileCopy(t *testing.T, src, dst string) {
	t.Helper()
	// Windows local execution ListRunfiles only shows files, but windows
	// remote shows files and dirs. Maybe because locally this is invoked
	// by MSYS64 bash, which bazel uses, while remotely the executable
	// is run by cmd.
	stat, err := os.Stat(src)
	if err != nil {
		t.Errorf("Couldn't stat %s: %v", src, err)
	}
	if !stat.Mode().IsRegular() {
		return
	}

	t.Logf("%s -> %s", src, dst)
	err = os.MkdirAll(filepath.Dir(dst), 0755)
	if err != nil {
		t.Fatalf("Unable to mkdir %s: %v", filepath.Dir(dst), err)
	}
	r, err := os.Open(src)
	if err != nil {
		t.Fatalf("Unable to open runfiles src %s: %v", src, err)
	}
	defer r.Close()
	w, err := os.Create(dst)
	if err != nil {
		t.Fatalf("Unable to create runfiles dst %s: %v", dst, err)
	}
	_, err = io.Copy(w, r)
	if err != nil {
		t.Fatalf("Unable to copy runfiles from %s to %s: %v", src, dst, err)
	}
	err = w.Close()
	if err != nil {
		t.Fatalf("Unable to close runfiles dst %s: %v", dst, err)
	}
}

func TestProcessToolchainInputs(t *testing.T) {
	cwd := getCwd(t)
	ip := &InputProcessor{}
	got, err := ip.ProcessToolchainInputs(context.Background(), cwd, ".", "testdata/executable", nil, nil)
	if err != nil {
		t.Fatalf("ProcessToolchainInputs() returned error: %v", err)
	}

	want := &command.InputSpec{
		Inputs: []string{filepath.Clean("testdata/executable"), filepath.Clean("testdata/a.txt"), filepath.Clean("testdata/b.txt")},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ProcessToolchainInputs() returned diff. (-want +got)\n%v", diff)
	}
}

func TestProcessToolchainInputsMultipleToolchains(t *testing.T) {
	cwd := getCwd(t)
	fmc := filemetadata.NewSingleFlightCache()
	ip := &InputProcessor{}
	got, err := ip.ProcessToolchainInputs(context.Background(), cwd, ".", "testdata/executable", []string{"testdata/executable2"}, fmc)
	if err != nil {
		t.Fatalf("ProcessToolchainInputs() returned error: %v", err)
	}

	want := &command.InputSpec{
		Inputs:               []string{filepath.Clean("testdata/executable2"), filepath.Clean("testdata/a.txt"), filepath.Clean("testdata/b.txt"), filepath.Clean("testdata/executable")},
		EnvironmentVariables: map[string]string{"PATH": "testdata:" + strings.Join(defaultPath, ":")},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ProcessToolchainInputs() returned diff. (-want +got)\n%v", diff)
	}
	if runtime.GOOS == "windows" {
		md := fmc.Get(filepath.Join(cwd, "testdata/executable2"))
		if md.Err != nil || !md.IsExecutable {
			t.Errorf("fmc.Get(\"testdata/executable2\") = %v, IsExecutable: %v, want no err and IsExecutable=true", md.Err, md.IsExecutable)
		}
	}
}

func TestProcessToolchainInputsBinInputs(t *testing.T) {
	cwd := getCwd(t)
	ip := &InputProcessor{}
	got, err := ip.ProcessToolchainInputs(context.Background(), cwd, ".", "testdata2/executable", nil, nil)
	if err != nil {
		t.Fatalf("ProcessToolchainInputs() returned error: %v", err)
	}

	want := &command.InputSpec{
		Inputs: []string{filepath.Clean("testdata2/executable"), filepath.Clean("testdata2/a.txt"), filepath.Clean("testdata2/b.txt")},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ProcessToolchainInputs() returned diff. (-want +got)\n%v", diff)
	}
}

// files specified in remote_toolchain_inputs are not in InputSpec.Inputs (no cache)
func TestProcessToolchainInputs_RemoteToolchainInputsNotInInputSpec(t *testing.T) {
	cwd := getCwd(t)
	ip := &InputProcessor{}
	got, err := ip.ProcessToolchainInputs(context.Background(), cwd, ".", "testdata/executable", []string{}, nil)
	if err != nil {
		t.Fatalf("ProcessToolchainInputs() returned error: %v", err)
	}

	want := &command.InputSpec{
		// only files that exist on disk are expected to be added to Inputs (other files from remote_toolchain_inputs should be ignored)
		Inputs: []string{filepath.Clean("testdata/executable"), filepath.Clean("testdata/a.txt"), filepath.Clean("testdata/b.txt")},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ProcessToolchainInputs() returned diff. (-want +got)\n%v", diff)
	}
}

// files specified in remote_toolchain_inputs are not in InputSpec.Inputs (no cache)
func TestProcessToolchainInputs_RemoteToolchainInputsNotInInputSpec_Cache(t *testing.T) {
	cwd := getCwd(t)
	ip := &InputProcessor{}
	got, err := ip.ProcessToolchainInputs(context.Background(), cwd, ".", "testdata/executable", []string{},
		filemetadata.NewSingleFlightCache())
	if err != nil {
		t.Fatalf("ProcessToolchainInputs() returned error: %v", err)
	}

	want := &command.InputSpec{
		// only files that exist on disk are expected to be added to Inputs (other files from remote_toolchain_inputs should be ignored)
		Inputs: []string{filepath.Clean("testdata/executable"), filepath.Clean("testdata/a.txt"), filepath.Clean("testdata/b.txt")},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ProcessToolchainInputs() returned diff. (-want +got)\n%v", diff)
	}
}

func TestProcessToolchainInputs_NestedWorkingDirOneLevel(t *testing.T) {
	cwd := getCwd(t)
	ip := &InputProcessor{}
	got, err := ip.ProcessToolchainInputs(context.Background(), cwd, "build", "testdata/executable", []string{"testdata/executable2"}, nil)
	if err != nil {
		t.Fatalf("ProcessToolchainInputs() returned error: %v", err)
	}

	want := &command.InputSpec{
		Inputs:               []string{filepath.Clean("testdata/executable2"), filepath.Clean("testdata/a.txt"), filepath.Clean("testdata/b.txt"), filepath.Clean("build/testdata/executable")},
		EnvironmentVariables: map[string]string{"PATH": filepath.Join("..", "testdata:") + strings.Join(defaultPath, ":")},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ProcessToolchainInputs() returned diff. (-want +got)\n%v", diff)
	}
}

func TestProcessToolchainInputs_NestedWorkingDirMultipleLevels(t *testing.T) {
	cwd := getCwd(t)
	ip := &InputProcessor{}
	got, err := ip.ProcessToolchainInputs(context.Background(), cwd, "build/in/here", "testdata/executable", []string{"testdata/executable2"}, nil)
	if err != nil {
		t.Fatalf("ProcessToolchainInputs() returned error: %v", err)
	}

	want := &command.InputSpec{
		Inputs:               []string{filepath.Clean("testdata/executable2"), filepath.Clean("testdata/a.txt"), filepath.Clean("testdata/b.txt"), filepath.Clean("build/in/here/testdata/executable")},
		EnvironmentVariables: map[string]string{"PATH": filepath.Join("..", "..", "..", "testdata:") + strings.Join(defaultPath, ":")},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ProcessToolchainInputs() returned diff. (-want +got)\n%v", diff)
	}
}

func TestProcessToolchainInputs_NestedWorkingDirAbsPath(t *testing.T) {
	cwd := getCwd(t)
	ip := &InputProcessor{}
	got, err := ip.ProcessToolchainInputs(context.Background(), cwd, "build/in/here", "testdata/executable", []string{cwd + "/testdata/executable2"}, nil)
	if err != nil {
		t.Fatalf("ProcessToolchainInputs() returned error: %v", err)
	}

	want := &command.InputSpec{
		Inputs:               []string{filepath.Clean("testdata/executable2"), filepath.Clean("testdata/a.txt"), filepath.Clean("testdata/b.txt"), filepath.Clean("build/in/here/testdata/executable")},
		EnvironmentVariables: map[string]string{"PATH": filepath.Join("..", "..", "..", "testdata:") + strings.Join(defaultPath, ":")},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ProcessToolchainInputs() returned diff. (-want +got)\n%v", diff)
	}
}

type execStub struct {
	execCount int
	stdout    string
	stderr    string
	err       error
}

func (e *execStub) Execute(ctx context.Context, cmd *command.Command) (string, string, error) {
	e.execCount++
	return e.stdout, e.stderr, e.err
}
