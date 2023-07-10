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

package args

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestScanner(t *testing.T) {
	args := []string{
		"-I", "include-dir", // flags
		"-Iother-include-dir",      // joined
		"--sysroot", "sysroot-dir", // flags
		"-target", "target-name", // flags
		"--prefix=prefix-value", // joined
		"-O",                    // -<flag>
		"-c",                    // flags but false.
		"@rspfile",              // no flag
		"foo.cc",                // no flag
	}
	s := Scanner{
		Args: args,
		Flags: map[string]int{
			"-I":        1,
			"--sysroot": 1,
			"-target":   1,
			"-c":        0,
		},
		Joined: []PrefixOption{
			{"--prefix=", 0},
			{"-I", 0},
		},
	}
	type result struct {
		Flag   string
		Args   []string
		Values []string
		Joined bool
	}
	var got []result
	for s.HasNext() {
		flag, args, values, joined := s.Next()
		got = append(got, result{
			Flag:   flag,
			Args:   args,
			Values: values,
			Joined: joined,
		})
	}
	want := []result{
		{
			Flag:   "-I",
			Args:   []string{"-I", "include-dir"},
			Values: []string{"include-dir"},
		},
		{
			Flag:   "-I",
			Args:   []string{"-Iother-include-dir"},
			Values: []string{"other-include-dir"},
			Joined: true,
		},
		{
			Flag:   "--sysroot",
			Args:   []string{"--sysroot", "sysroot-dir"},
			Values: []string{"sysroot-dir"},
		},
		{
			Flag:   "-target",
			Args:   []string{"-target", "target-name"},
			Values: []string{"target-name"},
		},
		{
			Flag:   "--prefix=",
			Args:   []string{"--prefix=prefix-value"},
			Values: []string{"prefix-value"},
			Joined: true,
		},
		{
			Flag:   "-O",
			Args:   []string{"-O"},
			Values: []string{},
		},
		{
			Flag:   "-c",
			Args:   []string{"-c"},
			Values: []string{},
		},
		{
			Args:   []string{"@rspfile"},
			Values: []string{"@rspfile"},
		},
		{
			Args:   []string{"foo.cc"},
			Values: []string{"foo.cc"},
		},
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("scan %q: -want +got %s", args, diff)
	}
}

func TestScannerAltPrefix(t *testing.T) {
	args := []string{
		"/I", "include-dir", // flags
		"/Iother-include-dir",      // joined
		"--sysroot", "sysroot-dir", // flags
		"/target", "target-name", // flags
		"--prefix=prefix-value", // joined
		"/O",                    // no flag
		"/c",                    // flags but false.
		"@rspfile",              // no flag
		"/src/foo.cc",           // no flag
	}

	s := Scanner{
		Args: args,
		Flags: map[string]int{
			"-I":        1,
			"/I":        1,
			"--sysroot": 1,
			"-target":   1,
			"/target":   1,
			"-c":        0,
			"-/":        0,
		},
		Joined: []PrefixOption{
			{"--prefix=", 0},
			{"-I", 0},
			{"/I", 0},
		},
		Normalized: map[string]string{
			"/I":      "-I",
			"/target": "-target",
			"/c":      "-c",
		},
	}
	type result struct {
		Flag   string
		Args   []string
		Values []string
		Joined bool
	}
	var got []result
	for s.HasNext() {
		flag, args, values, joined := s.Next()
		got = append(got, result{
			Flag:   flag,
			Args:   args,
			Values: values,
			Joined: joined,
		})
	}
	want := []result{
		{
			Flag:   "-I",
			Args:   []string{"/I", "include-dir"},
			Values: []string{"include-dir"},
		},
		{
			Flag:   "-I",
			Args:   []string{"/Iother-include-dir"},
			Values: []string{"other-include-dir"},
			Joined: true,
		},
		{
			Flag:   "--sysroot",
			Args:   []string{"--sysroot", "sysroot-dir"},
			Values: []string{"sysroot-dir"},
		},
		{
			Flag:   "-target",
			Args:   []string{"/target", "target-name"},
			Values: []string{"target-name"},
		},
		{
			Flag:   "--prefix=",
			Args:   []string{"--prefix=prefix-value"},
			Values: []string{"prefix-value"},
			Joined: true,
		},
		{
			Flag:   "",
			Args:   []string{"/O"},
			Values: []string{"/O"},
		},
		{
			Flag:   "-c",
			Args:   []string{"/c"},
			Values: []string{},
		},
		{
			Args:   []string{"@rspfile"},
			Values: []string{"@rspfile"},
		},
		{
			Args:   []string{"/src/foo.cc"},
			Values: []string{"/src/foo.cc"},
		},
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("scan %q: -want +got %s", args, diff)
	}
}
