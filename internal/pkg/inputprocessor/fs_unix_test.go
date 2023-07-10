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

//go:build !windows && !darwin

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
		"foo",
		"dir/bar",
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
			input: "foo",
			want:  "foo",
		},
		{
			input:   "Foo",
			wantErr: true,
		},
		{
			input: "/foo",
			want:  "foo",
		},
		{
			input: "//foo",
			want:  "foo",
		},
		{
			input: "dir/bar",
			want:  "dir/bar",
		},
		{
			input: "dir//bar",
			want:  "dir/bar",
		},
		{
			input:   "foo/bar",
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
			t.Errorf("pn.normalize(execRoot, %q)=%q, %v; want=%q, <nil>", tc.input, got, err, tc.want)
		}
	}
}
