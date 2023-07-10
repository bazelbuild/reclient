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

//go:build windows || darwin

package inputprocessor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalize(t *testing.T) {
	execRoot := t.TempDir()
	t.Logf("execRoot=%q", execRoot)
	for _, pathname := range []string{
		"TopDir/SubDir/File.h",
		"other_top_dir/sub_dir/file.h",
	} {
		fullpath := filepath.Join(execRoot, pathname)
		err := os.MkdirAll(filepath.Dir(fullpath), 0755)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("create %q", fullpath)
		err = os.WriteFile(fullpath, nil, 0644)
		if err != nil {
			t.Fatal(err)
		}
	}

	pn := newPathNormalizer(false)
	for _, tc := range []struct {
		input   string
		want    string
		wantErr bool
	}{
		{
			input: filepath.Clean("TopDir/SubDir/File.h"),
			want:  filepath.Clean("TopDir/SubDir/File.h"),
		},
		{
			input: "TopDir/SubDir/File.h",
			want:  filepath.Clean("TopDir/SubDir/File.h"),
		},
		{
			input: filepath.Clean("topdir/subdir/file.h"),
			want:  filepath.Clean("TopDir/SubDir/File.h"),
		},
		{
			input: "topDir",
			want:  "TopDir",
		},
		{
			input: filepath.Clean("Other_Top_Dir/Sub_Dir/File.h"),
			want:  filepath.Clean("other_top_dir/sub_dir/file.h"),
		},
		{
			input: "//TopDir/SubDir/File.h",
			want:  filepath.Clean("TopDir/SubDir/File.h"),
		},
		{
			input:   filepath.Clean("topdir/subdir/NonExistingFile.h"),
			wantErr: true,
		},
		{
			input:   filepath.Clean("TopDir/SubDir/file2.h"),
			wantErr: true,
		},
	} {
		got, err := pn.normalize(execRoot, tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("pn.normalize(execRoot, %q)=%q; want error", tc.input, got)
			}
			continue
		}
		if got != tc.want || err != nil {
			t.Errorf("pn.normalize(execRoot, %q)=%q, %v; want %q, <nil>", tc.input, got, err, tc.want)
		}
	}

	t.Logf("delete TopDir/SubDir/File.h")
	err := os.Remove(filepath.Join(execRoot, "TopDir/SubDir/File.h"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("create TopDir/SubDir/File2.h")
	err = os.WriteFile(filepath.Join(execRoot, "TopDir/SubDir/File2.h"), nil, 0644)
	if err != nil {
		t.Fatal(err)
	}
	pn = newPathNormalizer(false)

	for _, tc := range []struct {
		input   string
		want    string
		wantErr bool
	}{
		{
			input:   filepath.Clean("TopDir/SubDir/File.h"),
			wantErr: true,
		},
		{
			input:   "TopDir/SubDir/File.h",
			wantErr: true,
		},
		{
			input:   filepath.Clean("topdir/subdir/file.h"),
			wantErr: true,
		},
		{
			input: "topDir",
			want:  "TopDir",
		},
		{
			input: filepath.Clean("Other_Top_Dir/Sub_Dir/File.h"),
			want:  filepath.Clean("other_top_dir/sub_dir/file.h"),
		},
		{
			input:   "//TopDir/SubDir/File.h",
			wantErr: true,
		},
		{
			input:   filepath.Clean("topdir/subdir/NonExistingFile.h"),
			wantErr: true,
		},
		{
			input: filepath.Clean("TopDir/SubDir/file2.h"),
			want:  filepath.Clean("TopDir/SubDir/File2.h"),
		},
	} {
		got, err := pn.normalize(execRoot, tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("pn.normalize(execRoot, %q)=%q; want error", tc.input, got)
			}
			continue
		}
		if got != tc.want || err != nil {
			t.Errorf("pn.normalize(execRoot, %q)=%q, %v; want %q, <nil>", tc.input, got, err, tc.want)
		}
	}

}
