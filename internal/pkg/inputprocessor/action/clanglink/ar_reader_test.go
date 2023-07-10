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

package clanglink

import (
	"runtime"
	"testing"

	"github.com/bazelbuild/rules_go/go/tools/bazel"
	"github.com/google/go-cmp/cmp"
)

func TestReadFileNamesCalledMultipleTimes(t *testing.T) {
	test := []struct {
		name    string
		file    string
		want    []string
		wantErr bool
	}{
		{
			name: "Reading archive file.",
			file: "testdata/testarchive.a",
			want: []string{
				"foo.o",
				"bar.o",
				"baz.o",
			},
			wantErr: false,
		},
		{
			name: "Reading extended file names with BSD file format.",
			file: "testdata/bsd.a",
			want: []string{
				"bsd_extended_file_name.txt",
				"another_bsd_extended_file_name.txt",
			},
			wantErr: false,
		},
		{
			name: "Reading extended file names with GNU file format.",
			file: "testdata/gnu.a",
			want: []string{
				"gnu_extended_file_name.txt",
				"another_gnu_extended_file_name.txt",
			},
			wantErr: false,
		},
		{
			name: "2 byte alignment in data section.",
			file: "testdata/byte_alignment.a",
			want: []string{
				"odd",
				"even",
				"new_file",
			},
			wantErr: false,
		},
		{
			name:    "malformed archive file.",
			file:    "testdata/malformed.a",
			want:    []string{},
			wantErr: true,
		},
		{
			name: "thin archive file.",
			file: "testdata/thinarchive.a",
			want: map[string][]string{
				"darwin": []string{
					"DirA/foo.o",
					"DirB/bar.o",
				},
				"linux": []string{
					"DirA/foo.o",
					"DirB/bar.o",
				},
				"windows": []string{
					"DirA\\foo.o",
					"DirB\\bar.o",
				},
			}[runtime.GOOS],
			wantErr: false,
		},
	}

	for _, test := range test {
		t.Run(test.name, func(t *testing.T) {
			file, err := bazel.Runfile(test.file)
			if err != nil {
				t.Fatalf("Failed to find archive: %v err: %v", test.file, err)
			}
			arReader, err := newArReader(file, "")
			if arReader != nil {
				defer arReader.Close()
			}
			if !test.wantErr && err != nil {
				t.Errorf("newArReader(f) returned error: %v", err)
			}

			for i := 0; i < 5; i++ {
				if arReader != nil {
					got, err := arReader.ReadFileNames()
					if err != nil {
						t.Errorf("arReader.ReadFileNames() returned error: %v", err)
					}

					if diff := cmp.Diff(test.want, got); diff != "" {
						t.Errorf("arReader.ReadFileNames() failed on run %v returned diff, (-want +got): %s", i+1, diff)
					}
				}
			}

		})
	}

}
