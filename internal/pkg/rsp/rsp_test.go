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

package rsp

import (
	"os"
	"testing"

	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/args"
	"github.com/google/go-cmp/cmp"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{{
		name:    "space separated",
		content: "a b c",
		want:    []string{"a", "b", "c"},
	}, {
		name:    "new line separated",
		content: "a\nb\nc",
		want:    []string{"a", "b", "c"},
	}, {
		name:    "wrapped in quotes",
		content: "'a' 'b' \"c\"",
		want:    []string{"a", "b", "c"},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			tmpFile, err := os.CreateTemp(t.TempDir(), "")
			if err != nil {
				t.Fatalf("Cannot create temporary file: %v", err)
			}
			if _, err = tmpFile.Write([]byte(test.content)); err != nil {
				t.Fatalf("Failed to write to temporary file: %v", err)
			}
			if err := tmpFile.Close(); err != nil {
				t.Fatalf("Failed to close temporary file: %v", err)
			}

			got, err := Parse(tmpFile.Name())
			if err != nil {
				t.Errorf("Parse(%v) returned error: %v", tmpFile.Name(), err)
			}

			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("Parse(%v) returned diff, (-want +got): %s\n", tmpFile.Name(), diff)
			}
		})
	}
}

// TestParseWithFunc checks to make sure that rsp files can be processed by
// passing a function for handling the contents in a specific way.  Main use
// is for input processors to be able to parse rsp files as they would the
// command-line directly.  For the purposes of this test, the function
// just sums the length of all the strings it sees.
func TestParseWithFunc(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    int
	}{{
		name:    "space separated",
		content: "a b c",
		want:    3,
	}, {
		name:    "new line separated",
		content: "aaa\nbb\nc",
		want:    6,
	}, {
		name:    "wrapped in quotes",
		content: "'aaa' 'bbb' \"ccc\"",
		want:    9,
	}}

	// Use empty scanner
	scannerTemplate := args.Scanner{}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			tmpFile, err := os.CreateTemp(t.TempDir(), "")
			if err != nil {
				t.Fatalf("Cannot create temporary file: %v", err)
			}
			if _, err = tmpFile.Write([]byte(test.content)); err != nil {
				t.Fatalf("Failed to write to temporary file: %v", err)
			}
			if err := tmpFile.Close(); err != nil {
				t.Fatalf("Failed to close temporary file: %v", err)
			}

			sum := 0
			sumHandler := func(sc *args.Scanner) error {
				sum += len(sc.CurResult.Args[0])
				return nil
			}

			err = ParseWithFunc(tmpFile.Name(), scannerTemplate, sumHandler)
			if err != nil {
				t.Errorf("Parse(%v) returned error: %v", tmpFile.Name(), err)
			}

			if test.want != sum {
				t.Errorf("ParseWithFunc(%v) returned diff, (want got): %v %v\n", tmpFile.Name(), test.want, sum)
			}
		})
	}
}

// TestParseWithFunc checks to make sure that rsp files can be processed by
// passing a function for handling the contents in a specific way along with
// providing a scanner to parse the contents of the rsp into the flag, value,
// args triples (args.FlagResult).
func TestParseWithFuncWithClangesqueScanner(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantflag  string
		wantvalue []string
		wantarg   []string
	}{{
		name:      "clang flag",
		content:   "-fmax-opts 4",
		wantflag:  "-fmax-opts",
		wantvalue: []string{"4"},
		wantarg:   []string{"-fmax-opts", "4"},
	}}

	// Use a scanner that resembles a clang scanner.
	scannerTemplate := args.Scanner{
		Flags: map[string]int{
			"-fmax-opts": 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			tmpFile, err := os.CreateTemp(t.TempDir(), "")
			if err != nil {
				t.Fatalf("Cannot create temporary file: %v", err)
			}
			if _, err = tmpFile.Write([]byte(test.content)); err != nil {
				t.Fatalf("Failed to write to temporary file: %v", err)
			}
			if err := tmpFile.Close(); err != nil {
				t.Fatalf("Failed to close temporary file: %v", err)
			}

			var gotflag string
			var gotvalue, gotarg []string
			fHandler := func(sc *args.Scanner) error {
				curr := sc.CurResult
				gotflag = curr.NormalizedKey
				gotarg = curr.Args
				gotvalue = curr.Values
				return nil
			}

			err = ParseWithFunc(tmpFile.Name(), scannerTemplate, fHandler)
			if err != nil {
				t.Errorf("Parse(%v) returned error: %v", tmpFile.Name(), err)
			}

			if diff := cmp.Diff(test.wantarg, gotarg); diff != "" {
				t.Errorf("ParseWithFunc(%v) returned arg diff, (-want +got): %v\n", tmpFile.Name(), diff)
			}
			if test.wantflag != gotflag {
				t.Errorf("ParseWithFunc(%v) returned flag diff, (want got): %v %v\n", tmpFile.Name(), test.wantflag, gotflag)
			}
			if diff := cmp.Diff(test.wantvalue, gotvalue); diff != "" {
				t.Errorf("ParseWithFunc(%v) returned value diff, (-want +got): %v \n", tmpFile.Name(), diff)
			}
		})
	}
}
