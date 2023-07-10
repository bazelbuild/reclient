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

package includescanner

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParse_PosixPath(t *testing.T) {
	input := `out/Release/obj/third_party/breakpad/dump_syms/dymp_syms.o: \
  third_party/breakpad/breakpad/src/tools/windows/dump_syms/dump_syms.cc \
  third_party/depot_tools/win_toolchain/vs_files/a687d8e2e4114d9015eb550e1b156af21381faac/DIA\ SDK/include/dia2.h \
  third_party/breakpad/breakpad/src/common/basictypes.h
`

	got := parse(input)
	want := []string{
		`third_party/breakpad/breakpad/src/tools/windows/dump_syms/dump_syms.cc`,
		`third_party/depot_tools/win_toolchain/vs_files/a687d8e2e4114d9015eb550e1b156af21381faac/DIA SDK/include/dia2.h`,
		`third_party/breakpad/breakpad/src/common/basictypes.h`,
	}
	if !cmp.Equal(got, want) {
		t.Errorf("parse(%q)=%q; want %q\ndiff -want +got\n%s", input, got, want, cmp.Diff(want, got))
	}
}

func TestParse_WinPath(t *testing.T) {
	input := `clang-scan-deps dependency: \
  c:\src\chromium\src\third_party\breakpad\breakpad\src\tools\windows\dump_syms\dump_syms.cc \
  c:\src\chromium\src\third_party\depot_tools\win_toolchain\vs_files\a687d8e2e4114d9015eb550e1b156af21381faac\DIA\ SDK\include\dia2.h \
  c:\src\chromium\src\third_party\breakpad\breakpad\src\common\basictypes.h
`

	got := parse(input)
	want := []string{
		`c:\src\chromium\src\third_party\breakpad\breakpad\src\tools\windows\dump_syms\dump_syms.cc`,
		`c:\src\chromium\src\third_party\depot_tools\win_toolchain\vs_files\a687d8e2e4114d9015eb550e1b156af21381faac\DIA SDK\include\dia2.h`,
		`c:\src\chromium\src\third_party\breakpad\breakpad\src\common\basictypes.h`,
	}
	if !cmp.Equal(got, want) {
		t.Errorf("parse(%q)=%q; want %q\ndiff -want +got\n%s", input, got, want, cmp.Diff(want, got))
	}
}
