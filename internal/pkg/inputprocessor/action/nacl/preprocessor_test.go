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

package nacl

import (
	"context"
	"os"
	"regexp"
	"testing"

	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/action/cppcompile"
)

func TestComputeSpec(t *testing.T) {
	tests := []struct {
		name          string
		execPath      string
		extraArgs     []string
		wantFlagRegex string
		wantErr       bool
	}{
		{
			name:          "nacl toolchain x86_64",
			execPath:      "fake/native_client/toolchain/mac_x86/x86_64-nacl-clang",
			wantFlagRegex: "--target=x86_64-nacl",
		},
		{
			name:          "nacl toolchain mipsel",
			execPath:      "native_client/toolchain/foo/bar/fake_os/mipsel-nacl-clang",
			wantFlagRegex: "--target=mipsel-nacl",
		},
		{
			name:          "nacl toolchain pnacl",
			execPath:      "native_client/toolchain/pnacl-clang",
			extraArgs:     []string{"-pnacl=foo", "--pnacl=bar"},
			wantFlagRegex: "--target=i686-nacl",
		},
		{
			name:          "nacl toolchain pnacl, already has target",
			execPath:      "native_client/toolchain/pnacl",
			extraArgs:     []string{"-target", "i686-nacl"},
			wantFlagRegex: "-target",
		},
		{
			name:     "no nacl toolchain, failure",
			execPath: "clang",
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s := &stubCPPDepScanner{
				res: []string{"foo.h"},
				err: nil,
			}
			cc := &cppcompile.Preprocessor{
				CPPDepScanner:    s,
				BasePreprocessor: &inputprocessor.BasePreprocessor{Ctx: ctx},
			}
			c := &Preprocessor{cc}

			pwd, err := os.Getwd()
			if err != nil {
				t.Fatalf("Unable to get current working directory: %v", err)
			}
			cc.Options = inputprocessor.Options{
				ExecRoot: pwd,
				Cmd:      []string{tc.execPath, "-o", "foo.o", "foo.cpp"},
			}
			cc.Options.Cmd = append(cc.Options.Cmd, tc.extraArgs...)

			err = c.ParseFlags()
			if !tc.wantErr && err != nil {
				t.Fatalf("ParseFlags() failed: %v", err)
			}
			err = c.ComputeSpec()
			if !tc.wantErr && err != nil {
				t.Fatalf("ComputeSpec() failed: %v", err)
			} else if tc.wantErr && err == nil {
				t.Fatalf("ComputeSpec() did not fail: %v", err)
			}
			if containsRegexCount(s.gotCmd, "-pnacl") > 0 {
				t.Errorf("ComputeSpec() must remove '-pnacl' flag when calling clang-scan-deps for nacl toolchain: %v", s.gotCmd)
			}
			if tc.wantFlagRegex != "" && containsRegexCount(s.gotCmd, tc.wantFlagRegex) != 1 {
				t.Errorf("ComputeSpec() must append %s when calling clang-scan-deps for nacl toolchains: %v", tc.wantFlagRegex, s.gotCmd)
			} else if tc.wantFlagRegex == "" && containsRegexCount(s.gotCmd, "-target") > 0 {
				t.Errorf("ComputeSpec() must _not_ append %s when calling clang-scan-deps for non nacl toolchains: %v", tc.wantFlagRegex, s.gotCmd)
			}
		})
	}
}

type stubCPPDepScanner struct {
	gotCmd       []string
	gotFileName  string
	gotDirectory string

	res []string
	err error
}

func (s *stubCPPDepScanner) ProcessInputs(_ context.Context, _ string, command []string, filename, directory string, _ []string) ([]string, bool, error) {
	s.gotCmd = command
	s.gotFileName = filename
	s.gotDirectory = directory

	return s.res, false, s.err
}

func containsRegexCount(src []string, pattern string) int {
	total := 0
	for _, v := range src {
		if ok, err := regexp.Match(pattern, []byte(v)); err == nil && ok {
			total = total + 1
		}
	}
	return total
}
