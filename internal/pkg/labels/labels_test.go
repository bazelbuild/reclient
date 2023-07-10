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

package labels

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestFromMap(t *testing.T) {
	tests := []struct {
		name     string
		labelMap map[string]string
		want     Labels
	}{
		{
			name: "Test all labels given",
			labelMap: map[string]string{
				"type":     "compile",
				"compiler": "clang",
				"lang":     "cpp",
				"tool":     "clang",
			},
			want: Labels{
				ActionType: Compile,
				Compiler:   Clang,
				Lang:       Cpp,
				ActionTool: ClangTool,
			},
		},
		{
			name: "Test some labels missing",
			labelMap: map[string]string{
				"type": "link",
				"tool": "clang",
			},
			want: Labels{
				ActionType: Link,
				ActionTool: ClangTool,
			},
		},
		{
			name:     "Test empty labels",
			labelMap: map[string]string{},
			want:     Labels{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := FromMap(test.labelMap); test.want != got {
				t.Errorf("FromMap(%v) returned diff, want %v, got %v", test.labelMap, test.want, got)
			}
		})
	}
}

func TestToMap(t *testing.T) {
	tests := []struct {
		name   string
		labels Labels
		want   map[string]string
	}{
		{
			name: "Test all labels given",
			labels: Labels{
				ActionType: Compile,
				Compiler:   Clang,
				Lang:       Cpp,
				ActionTool: ClangTool,
			},
			want: map[string]string{
				"type":     "compile",
				"compiler": "clang",
				"lang":     "cpp",
				"tool":     "clang",
			},
		},
		{
			name: "Test some labels missing",
			labels: Labels{
				ActionType: Link,
				ActionTool: ClangTool,
			},
			want: map[string]string{
				"type": "link",
				"tool": "clang",
			},
		},
		{
			name:   "Test empty labels",
			labels: Labels{},
			want:   map[string]string{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ToMap(test.labels)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("ToMap(%v) returned diff, (-want +got): %v\n", test.labels, diff)
			}
		})
	}
}

func TestToDigest(t *testing.T) {
	m := map[string]string{
		"type":     "compile",
		"compiler": "clang",
		"lang":     "cpp",
	}
	want := "1c2a12e4"

	got, err := ToDigest(m)
	if err != nil {
		t.Fatalf("ToDigest(%+v) failed: %v", m, err)
	}
	if want != got {
		t.Errorf("ToDigest(%+v) returned diff, want %q, got %q", m, want, got)
	}
}

func TestToDigest_UsesCache(t *testing.T) {
	m := map[string]string{
		"type":     "compile",
		"compiler": "clang",
		"lang":     "cpp",
	}
	lk := ToKey(m)
	want := "foo"
	valFn := func() (interface{}, error) { return want, nil }
	labelsDigestCache.Delete(lk)
	if _, err := labelsDigestCache.LoadOrStore(lk, valFn); err != nil {
		t.Fatalf("LoadOrStore(%v,%v) failed: %v", lk, want, err)
	}

	got, err := ToDigest(m)
	if err != nil {
		t.Fatalf("ToDigest(%+v) failed: %v", m, err)
	}
	if want != got {
		t.Errorf("ToDigest(%+v) returned diff, want %q, got %q", m, want, got)
	}
}
