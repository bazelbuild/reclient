// Copyright 2023 Google LLC
// Copyright 2026 NVIDIA CORPORATION
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

package reproxy

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	ppb "github.com/bazelbuild/reclient/api/proxy"
	"github.com/bazelbuild/reclient/internal/pkg/features"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/google/go-cmp/cmp"
)

func TestRelativizeRejectsOutsideRoot(t *testing.T) {
	config := features.GetConfig()
	saved := config.RelativizePaths
	config.RelativizePaths = true
	t.Cleanup(func() { config.RelativizePaths = saved })
	for _, strategy := range []ppb.ExecutionStrategy_Value{
		ppb.ExecutionStrategy_REMOTE,
		ppb.ExecutionStrategy_REMOTE_LOCAL_FALLBACK,
		ppb.ExecutionStrategy_RACING,
	} {
		t.Run(strategy.String(), func(t *testing.T) {
			outside := filepath.Join(t.TempDir(), "outside.h")
			a := &action{
				cmd: &command.Command{
					Identifiers: &command.Identifiers{},
					ExecRoot:    t.TempDir(),
					Args:        []string{"clang", "-include", outside, "-c", "source.cc"},
				},
				execStrategy:    strategy,
				reclientTimeout: 60,
			}
			server := &Server{}
			server.Init()
			// No execution services are installed: mapping must fail before
			// input processing, remote execution, or a local fallback starts.
			server.runAction(context.Background(), a)
			if a.res == nil || !errors.Is(a.res.Err, errPathsOutsideExecRoot) {
				t.Fatalf("result = %v; want outside-root error", a.res)
			}
			if !strings.Contains(a.res.Err.Error(), outside) {
				t.Errorf("error %q does not identify %q", a.res.Err, outside)
			}
		})
	}
}

func TestRelativizePaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("path rewriting is only exercised on posix hosts")
	}
	tests := []struct {
		name         string
		execRoot     string
		workingDir   string
		args         []string
		windowsCross bool
		wantArgs     []string
		wantOutside  []string
	}{{
		// The shape of a CMake-generated compile action: the compiler lives
		// outside the source tree, every -I is absolute, and two of them
		// escape upward through a "..".
		name:       "cmake compile action",
		execRoot:   "/work",
		workingDir: "llvm/oss/b/build_local",
		args: []string{
			"/work/crclang/bin/clang++",
			"-D_DEBUG",
			"-I/work/llvm/oss/b/build_local/lib/Support",
			"-I/work/llvm/oss/b/llvm/../third-party/siphash/include",
			"-I", "/work/llvm/oss/b/llvm/include",
			"-std=c++17",
			"-MF", "lib/Support/CMakeFiles/CommandLine.cpp.o.d",
			"-o", "lib/Support/CMakeFiles/CommandLine.cpp.o",
			"-c", "/work/llvm/oss/b/llvm/lib/Support/CommandLine.cpp",
		},
		wantArgs: []string{
			"../../../../crclang/bin/clang++",
			"-D_DEBUG",
			"-Ilib/Support",
			"-I../third-party/siphash/include",
			"-I", "../llvm/include",
			"-std=c++17",
			"-MF", "lib/Support/CMakeFiles/CommandLine.cpp.o.d",
			"-o", "lib/Support/CMakeFiles/CommandLine.cpp.o",
			"-c", "../llvm/lib/Support/CommandLine.cpp",
		},
	}, {
		name:       "paths outside exec root are reported, not rewritten",
		execRoot:   "/work/llvm/oss/b",
		workingDir: "build_local",
		args: []string{
			"/work/crclang/bin/clang++",
			"-isystem", "/usr/include",
			"-working-directory=/proc/self/cwd",
			"-c", "llvm/lib/Support/CommandLine.cpp",
		},
		wantArgs: []string{
			"/work/crclang/bin/clang++",
			"-isystem", "/usr/include",
			"-working-directory=/proc/self/cwd",
			"-c", "llvm/lib/Support/CommandLine.cpp",
		},
		// /proc/self/cwd is deliberately absent: a pseudo-filesystem path is
		// excluded from rewriting rather than reported as a hermeticity
		// problem, because it means the same thing on the worker.
		wantOutside: []string{"/work/crclang/bin/clang++", "/usr/include"},
	}, {
		// A define's value ends up in the compiled source, where a
		// working-directory-relative path means something else entirely.
		name:       "defines are left alone",
		execRoot:   "/work",
		workingDir: "b",
		args:       []string{"/work/bin/cc", "-DROOT=/work/src", "--sysroot=/work/sysroot", "-c", "/work/src/a.c"},
		wantArgs:   []string{"../bin/cc", "-DROOT=/work/src", "--sysroot=../sysroot", "-c", "../src/a.c"},
	}, {
		// exec_root=/ is the configuration that actually works for a toolchain
		// using the host's libstdc++, and it is the one case where the exec
		// root already ends in a separator.
		name:       "exec root is the filesystem root",
		execRoot:   "/",
		workingDir: "work/llvm/oss/b/build_local",
		args: []string{
			"/work/crclang/bin/clang++",
			"-isystem", "/usr/include/c++/15",
			"-c", "/work/llvm/oss/b/llvm/lib/Support/CommandLine.cpp",
		},
		wantArgs: []string{
			"../../../../crclang/bin/clang++",
			"-isystem", "../../../../../usr/include/c++/15",
			"-c", "../llvm/lib/Support/CommandLine.cpp",
		},
	}, {
		// With exec_root=/ every path looks rewritable, including the kernel's
		// virtual filesystems. Rewriting -working-directory=/proc/self/cwd
		// breaks resolution of every relative path on the command line.
		name:       "pseudo-filesystem paths are left alone",
		execRoot:   "/",
		workingDir: "work/b",
		args: []string{
			"/work/bin/cc",
			"-working-directory=/proc/self/cwd",
			"-c", "/work/src/a.c",
		},
		wantArgs: []string{
			"../bin/cc",
			"-working-directory=/proc/self/cwd",
			"-c", "../src/a.c",
		},
	}, {
		// -ffile-prefix-map and friends take an old=new pair. Relativizing the
		// whole value yields "../src=/src", a mapping that matches nothing, so
		// the recorded paths change silently rather than the action failing.
		name:       "prefix-map pairs are left alone",
		execRoot:   "/work",
		workingDir: "b",
		args: []string{
			"/work/bin/cc",
			"-ffile-prefix-map=/work/src=/src",
			"-fdebug-prefix-map=/work/src=/src",
			"-fmodule-file=Name=/work/mods/x.pcm",
			"-c", "/work/src/a.c",
		},
		wantArgs: []string{
			"../bin/cc",
			"-ffile-prefix-map=/work/src=/src",
			"-fdebug-prefix-map=/work/src=/src",
			"-fmodule-file=Name=/work/mods/x.pcm",
			"-c", "../src/a.c",
		},
	}, {
		// filepath.IsAbs("/std:c++17") is true on posix, so MSVC-style options
		// would otherwise be mangled and reported as hermeticity problems.
		name:       "clang-cl slash options are not paths",
		execRoot:   "/work",
		workingDir: "b",
		args: []string{
			"/work/bin/clang-cl",
			"/std:c++17",
			"/c", "/work/src/a.cc",
		},
		wantArgs: []string{
			"../bin/clang-cl",
			"/std:c++17",
			"/c", "/work/src/a.cc",
		},
	}, {
		// clang-cl invoked on a POSIX host (cross-compiling for a Windows target
		// against a sysroot copied onto the same host): the paths on the command
		// line are POSIX-style, so clang-cl path flags rewrite exactly like their
		// GNU-style equivalents.
		name:       "clang-cl path flags, posix-style",
		execRoot:   "/work",
		workingDir: "b",
		args: []string{
			"/work/bin/clang-cl",
			"/I/work/inc",
			"/I", "/work/inc2",
			"-imsvc/work/msvc",
			"-imsvc", "/work/msvc2",
			"/winsysroot/work/sysroot",
			"/external:I/work/ext",
			"/external:I", "/work/ext2",
			"/clang:-I/work/nested",
			"/clang:-I", "/clang:/work/nested2",
			"/Fo/work/b/out/a.obj",
			"/Fo:/work/b/out/a.obj.2",
			"/Fe/work/b/out/a.exe",
			"/Fd/work/b/out/a.pdb",
			"/c", "src/a.cc",
		},
		wantArgs: []string{
			"../bin/clang-cl",
			"/I../inc",
			"/I", "../inc2",
			"-imsvc../msvc",
			"-imsvc", "../msvc2",
			"/winsysroot../sysroot",
			"/external:I../ext",
			"/external:I", "../ext2",
			"/clang:-I../nested",
			"/clang:-I", "/clang:../nested2",
			"/Foout/a.obj",
			"/Fo:out/a.obj.2",
			"/Feout/a.exe",
			"/Fdout/a.pdb",
			"/c", "src/a.cc",
		},
	}, {
		// A bare name is a PATH lookup to exec(), not a path. Rewriting the
		// compiler to "cc" would let the worker pick its own.
		name:       "executable never becomes a bare name",
		execRoot:   "/work",
		workingDir: "bin",
		args:       []string{"/work/bin/cc", "-c", "/work/bin/a.c"},
		wantArgs:   []string{"./cc", "-c", "a.c"},
	}, {
		// Every include-path-management flag that names a directory or file,
		// in its joined spelling. A flag missing from the table is not an
		// error, it is an absolute path that silently reaches the worker.
		name:       "joined include flags",
		execRoot:   "/work",
		workingDir: "b",
		args: []string{
			"/work/bin/cc",
			"-I/work/inc",
			"-cxx-isystem/work/cxx",
			"-iapinotes-modules/work/apinotes",
			"-idirafter/work/after",
			"-iframework/work/fw",
			"-iframeworkwithsysroot/work/fwsys",
			"-imacros/work/m.h",
			"-include/work/pre.h",
			"-iprefix/work/pre",
			"-iquote/work/quote",
			"-isysroot/work/sysroot",
			"-isystem/work/sys",
			"-isystem-after/work/sysafter",
			"-ivfsoverlay/work/vfs.yaml",
			"-iwithprefix/work/wp",
			"-iwithprefixbefore/work/wpb",
			"-iwithsysroot/work/wsr",
			"-stdlib++-isystem/work/stdlibxx",
			"-L/work/lib",
			"-B/work/bprefix",
			"-F/work/frameworks",
			"-c", "/work/src/a.c",
		},
		wantArgs: []string{
			"../bin/cc",
			"-I../inc",
			"-cxx-isystem../cxx",
			"-iapinotes-modules../apinotes",
			"-idirafter../after",
			"-iframework../fw",
			"-iframeworkwithsysroot../fwsys",
			"-imacros../m.h",
			"-include../pre.h",
			"-iprefix../pre",
			"-iquote../quote",
			"-isysroot../sysroot",
			"-isystem../sys",
			"-isystem-after../sysafter",
			"-ivfsoverlay../vfs.yaml",
			"-iwithprefix../wp",
			"-iwithprefixbefore../wpb",
			"-iwithsysroot../wsr",
			"-stdlib++-isystem../stdlibxx",
			"-L../lib",
			"-B../bprefix",
			"-F../frameworks",
			"-c", "../src/a.c",
		},
	}, {
		// The same flags with the path as a separate argument, plus the
		// --long-name spellings clang accepts for several of them.
		name:       "separate-argument include flags",
		execRoot:   "/work",
		workingDir: "b",
		args: []string{
			"/work/bin/cc",
			"-I", "/work/inc",
			"-isystem", "/work/sys",
			"-isystem-after", "/work/sysafter",
			"-iquote", "/work/quote",
			"-idirafter", "/work/after",
			"-iframework", "/work/fw",
			"-ivfsoverlay", "/work/vfs.yaml",
			"-include-pch", "/work/pch/a.pch",
			"-fmodules-user-build-path", "/work/modules",
			"--include-directory", "/work/inc2",
			"--include-directory-after", "/work/after2",
			"--include-prefix", "/work/pre2",
			"--include-with-prefix", "/work/wp2",
			"--include-with-prefix-after", "/work/wpa2",
			"--include-with-prefix-before", "/work/wpb2",
			"--sysroot", "/work/sysroot",
			"-c", "/work/src/a.c",
		},
		wantArgs: []string{
			"../bin/cc",
			"-I", "../inc",
			"-isystem", "../sys",
			"-isystem-after", "../sysafter",
			"-iquote", "../quote",
			"-idirafter", "../after",
			"-iframework", "../fw",
			"-ivfsoverlay", "../vfs.yaml",
			"-include-pch", "../pch/a.pch",
			"-fmodules-user-build-path", "../modules",
			"--include-directory", "../inc2",
			"--include-directory-after", "../after2",
			"--include-prefix", "../pre2",
			"--include-with-prefix", "../wp2",
			"--include-with-prefix-after", "../wpa2",
			"--include-with-prefix-before", "../wpb2",
			"--sysroot", "../sysroot",
			"-c", "../src/a.c",
		},
	}, {
		// "--name=value" spellings go through the generic "=" branch, so they
		// need no table entry -- but that branch must not touch a
		// header-name prefix or a non-path value.
		name:       "equals-form include flags",
		execRoot:   "/work",
		workingDir: "b",
		args: []string{
			"/work/bin/cc",
			"--include-directory=/work/inc",
			"-fmodules-cache-path=/work/mcache",
			"-fprebuilt-module-path=/work/pcm",
			"-fbuild-session-file=/work/session",
			"--system-header-prefix=/work/boost/",
			"-fmodules-prune-after=600",
			"-c", "/work/src/a.c",
		},
		wantArgs: []string{
			"../bin/cc",
			"--include-directory=../inc",
			"-fmodules-cache-path=../mcache",
			"-fprebuilt-module-path=../pcm",
			"-fbuild-session-file=../session",
			// A header-name prefix, not a filesystem path.
			"--system-header-prefix=/work/boost/",
			"-fmodules-prune-after=600",
			"-c", "../src/a.c",
		},
	}, {
		name:       "relative arguments are untouched",
		execRoot:   "/work",
		workingDir: "b",
		args:       []string{"cc", "-Iinc", "-I", "inc2", "-o", "a.o", "-c", "a.c"},
		wantArgs:   []string{"cc", "-Iinc", "-I", "inc2", "-o", "a.o", "-c", "a.c"},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := &command.Command{
				Identifiers: &command.Identifiers{},
				Args:        test.args,
				ExecRoot:    test.execRoot,
				WorkingDir:  test.workingDir,
			}
			gotArgs, gotOutside, _ := relativizePaths(cmd, test.windowsCross)
			if diff := cmp.Diff(test.wantArgs, gotArgs); diff != "" {
				t.Errorf("relativizePaths() args diff (-want +got):\n%s", diff)
			}
			if len(test.wantOutside) != 0 || len(gotOutside) != 0 {
				if diff := cmp.Diff(test.wantOutside, gotOutside); diff != "" {
					t.Errorf("relativizePaths() outside-root diff (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestRelativizePathsReportsResponseFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("path rewriting is only exercised on posix hosts")
	}
	// The rewriter cannot see inside a response file, so the absolute paths in
	// it survive to the worker. Reporting the file is what stops a clean
	// out-of-root report from being read as "this action is fully relative".
	cmd := &command.Command{
		Identifiers: &command.Identifiers{},
		Args:        []string{"/work/bin/cc", "@/work/b/link.rsp"},
		ExecRoot:    "/work",
		WorkingDir:  "b",
	}
	gotArgs, _, gotRsp := relativizePaths(cmd, false)
	wantArgs := []string{"../bin/cc", "@link.rsp"}
	if diff := cmp.Diff(wantArgs, gotArgs); diff != "" {
		t.Errorf("relativizePaths() args diff (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"/work/b/link.rsp"}, gotRsp); diff != "" {
		t.Errorf("relativizePaths() response files diff (-want +got):\n%s", diff)
	}
}

// TestRelativizePathsClangClWindows exercises clang-cl path rewriting against
// Windows-style paths (drive letters, backslashes): the real shape of a
// windowsCross action, whose client is Windows regardless of which GOOS this
// test binary itself is built for. Unlike TestRelativizePaths, it does not
// skip on a Windows host -- the Windows-path logic (isWindowsAbs,
// cleanWindowsPath, commonWindowsPrefix) is spelled out independently of
// path/filepath specifically so it behaves the same on every GOOS.
func TestRelativizePathsClangClWindows(t *testing.T) {
	tests := []struct {
		name         string
		execRoot     string
		workingDir   string
		args         []string
		windowsCross bool
		wantArgs     []string
		wantOutside  []string
	}{{
		// The shape of a real Windows CMake/ninja-generated clang-cl action:
		// backslashes, a drive-letter exec root, joined and separate flags,
		// and a colon-joined /Fo:. windowsCross=false (a native Windows
		// worker, or none of this session's concern) keeps the backslash
		// separator clang-cl itself would have produced.
		name:       "native windows clang-cl action",
		execRoot:   `C:\t`,
		workingDir: `out`,
		args: []string{
			`clang-cl.exe`,
			`/I` + `C:\t\src\inc`,
			`/I`, `C:\t\third_party\inc`,
			`/imsvc` + `C:\t\sysroot\VC\include`,
			`/winsysroot` + `C:\t\sysroot`,
			`/external:I` + `C:\t\src\ext`,
			`/Fo:` + `C:\t\out\a.obj`,
			`/Fd` + `C:\t\out\a.pdb`,
			`/c`, `src\a.cc`,
		},
		wantArgs: []string{
			`clang-cl.exe`,
			`/I` + `..\src\inc`,
			`/I`, `..\third_party\inc`,
			`/imsvc` + `..\sysroot\VC\include`,
			`/winsysroot` + `..\sysroot`,
			`/external:I` + `..\src\ext`,
			`/Fo:` + `a.obj`,
			`/Fd` + `a.pdb`,
			`/c`, `src\a.cc`,
		},
	}, {
		// windowsCross=true: the worker is Linux, so the rewritten paths use
		// a forward slash. Forward-slash spellings of the same drive-letter
		// paths are accepted too (clang-cl allows either separator).
		name:         "windows cross uses forward slashes",
		execRoot:     `C:\t`,
		workingDir:   `out`,
		windowsCross: true,
		args: []string{
			`clang-cl.exe`,
			`/IC:/t/src/inc`,
			`/winsysroot`, `C:\t\sysroot`,
			`/Fo` + `C:\t\out\a.obj`,
			`/c`, `src/a.cc`,
		},
		wantArgs: []string{
			`clang-cl.exe`,
			`/I../src/inc`,
			`/winsysroot`, `../sysroot`,
			`/Fo` + `a.obj`,
			`/c`, `src/a.cc`,
		},
	}, {
		// What CMake >= 4 + Ninja really emits: the compile runs in the build dir, so
		// most paths are already relative and use backslashes. On a Linux worker those
		// are filename characters, so windowsCross must forward-slash them too.
		name:         "windows cross forward-slashes already-relative paths",
		execRoot:     `C:\m7b`,
		workingDir:   `build`,
		windowsCross: true,
		args: []string{
			`clang-cl.exe`,
			`-clang:-MD`, `-clang:-MTCMakeFiles\m7app.dir\src\a.cpp.obj`,
			`-clang:-MFCMakeFiles\m7app.dir\src\a.cpp.obj.d`,
			`/FoCMakeFiles\m7app.dir\src\a.cpp.obj`,
			`/FdCMakeFiles\m7app.dir\`,
			`/DFOO="a\b"`,
			`-c`, `--`, `src\a.cpp`,
		},
		wantArgs: []string{
			`clang-cl.exe`,
			`-clang:-MD`, `-clang:-MTCMakeFiles\m7app.dir\src\a.cpp.obj`,
			`-clang:-MFCMakeFiles/m7app.dir/src/a.cpp.obj.d`,
			`/FoCMakeFiles/m7app.dir/src/a.cpp.obj`,
			`/FdCMakeFiles/m7app.dir/`,
			`/DFOO="a\b"`,
			`-c`, `--`, `src/a.cpp`,
		},
	}, {
		// Windows paths are case-insensitive: a drive letter or directory
		// spelled with different case than the exec root must still be
		// recognized as being under it.
		name:       "case-insensitive root match",
		execRoot:   `c:\T`,
		workingDir: `Out`,
		args: []string{
			`clang-cl.exe`,
			`/I` + `C:\t\SRC\Inc`,
			`/c`, `src\a.cc`,
		},
		wantArgs: []string{
			`clang-cl.exe`,
			`/I` + `..\SRC\Inc`,
			`/c`, `src\a.cc`,
		},
	}, {
		// A drive-letter path outside the exec root is reported, not
		// rewritten, the same as a POSIX path outside the exec root.
		name:       "drive-letter path outside exec root is reported",
		execRoot:   `C:\t`,
		workingDir: `out`,
		args: []string{
			`clang-cl.exe`,
			`/imsvc`, `D:\other\include`,
			`/c`, `src\a.cc`,
		},
		wantArgs: []string{
			`clang-cl.exe`,
			`/imsvc`, `D:\other\include`,
			`/c`, `src\a.cc`,
		},
		wantOutside: []string{`D:\other\include`},
	}, {
		// The "-" spelling of the same flags, which clang-cl accepts just as
		// well as the "/" spelling.
		name:       "dash-prefixed spellings",
		execRoot:   `C:\t`,
		workingDir: `out`,
		args: []string{
			`clang-cl.exe`,
			`-imsvc`, `C:\t\sysroot\include`,
			`-winsysroot` + `C:\t\sysroot`,
			`-external:I` + `C:\t\src\ext`,
			`/c`, `src\a.cc`,
		},
		wantArgs: []string{
			`clang-cl.exe`,
			`-imsvc`, `..\sysroot\include`,
			`-winsysroot` + `..\sysroot`,
			`-external:I` + `..\src\ext`,
			`/c`, `src\a.cc`,
		},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := &command.Command{
				Identifiers: &command.Identifiers{},
				Args:        test.args,
				ExecRoot:    test.execRoot,
				WorkingDir:  test.workingDir,
			}
			gotArgs, gotOutside, _ := relativizePaths(cmd, test.windowsCross)
			if diff := cmp.Diff(test.wantArgs, gotArgs); diff != "" {
				t.Errorf("relativizePaths() args diff (-want +got):\n%s", diff)
			}
			if len(test.wantOutside) != 0 || len(gotOutside) != 0 {
				if diff := cmp.Diff(test.wantOutside, gotOutside); diff != "" {
					t.Errorf("relativizePaths() outside-root diff (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// TableGen and MLIR source sharding use the same generic rewriting as compiler
// actions, including their bare positional inputs and dependency-file paths.
func TestRelativizeTableGenPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix paths")
	}
	for _, input := range []string{"TestOps.td", "TestOps.cpp"} {
		cmd := &command.Command{ExecRoot: "/checkout", WorkingDir: "build",
			Args: []string{"/checkout/build/bin/mlir-tblgen", "-I/checkout/include", "-I", "/checkout/build/include", "/checkout/src/" + input, "-o", "/checkout/build/out.inc", "-d", "/checkout/build/out.inc.d"}}
		got, outside, responseFiles := relativizePaths(cmd, false)
		want := []string{"bin/mlir-tblgen", "-I../include", "-I", "include", "../src/" + input, "-o", "out.inc", "-d", "out.inc.d"}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatal(diff)
		}
		if len(outside) != 0 || len(responseFiles) != 0 {
			t.Fatalf("unexpected unrewritten paths: %v %v", outside, responseFiles)
		}
	}
}

func TestRelativizePathsClangClForcedIncludes(t *testing.T) {
	tests := []struct {
		nativeWindows bool
		name          string
		execRoot      string
		workingDir    string
		args          []string
		wantArgs      []string
		wantOutside   []string
	}{
		{name: "joined slash", args: []string{`/FIC:/root/tools/anchor.h`}, wantArgs: []string{`/FItools/anchor.h`}},
		{name: "joined dash", args: []string{`-FIC:\root\tools\anchor.h`}, wantArgs: []string{`-FItools/anchor.h`}},
		{name: "separate slash", args: []string{`/FI`, `C:\root\tools\anchor.h`}, wantArgs: []string{`/FI`, `tools/anchor.h`}},
		{name: "separate dash", args: []string{`-FI`, `C:/root/tools/anchor.h`}, wantArgs: []string{`-FI`, `tools/anchor.h`}},
		{name: "relative cross path", args: []string{`/FItools\anchor.h`}, wantArgs: []string{`/FItools/anchor.h`}},
		{name: "working directory", workingDir: "build", args: []string{`/FIC:/root/tools/anchor.h`}, wantArgs: []string{`/FI../tools/anchor.h`}},
		{name: "case insensitive root", args: []string{`/FIc:/ROOT/tools/anchor.h`}, wantArgs: []string{`/FItools/anchor.h`}},
		{name: "native Windows", nativeWindows: true, workingDir: "build", args: []string{`/FIC:\root\tools\anchor.h`, `/FI`, `C:\root\tools\other.h`}, wantArgs: []string{`/FI..\tools\anchor.h`, `/FI`, `..\tools\other.h`}},
		{name: "UNC root", execRoot: `\\server\share\root`, args: []string{`/FI\\server\share\root\tools\anchor.h`}, wantArgs: []string{`/FItools/anchor.h`}},
		{name: "outside root joined", args: []string{`/FID:/outside/anchor.h`}, wantArgs: []string{`/FID:/outside/anchor.h`}, wantOutside: []string{`D:/outside/anchor.h`}},
		{name: "outside root separate", args: []string{`-FI`, `D:\outside\anchor.h`}, wantArgs: []string{`-FI`, `D:\outside\anchor.h`}, wantOutside: []string{`D:\outside\anchor.h`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.execRoot == "" {
				tt.execRoot = `C:\root`
			}
			windowsCross := !tt.nativeWindows
			args := append([]string{"clang-cl.exe"}, tt.args...)
			wantArgs := append([]string{"clang-cl.exe"}, tt.wantArgs...)
			cmd := &command.Command{Identifiers: &command.Identifiers{}, ExecRoot: tt.execRoot, WorkingDir: tt.workingDir, Args: args}
			gotArgs, outside, _ := relativizePaths(cmd, windowsCross)
			if diff := cmp.Diff(wantArgs, gotArgs); diff != "" {
				t.Errorf("args diff (-want +got):\n%s", diff)
			}
			if len(tt.wantOutside) != 0 || len(outside) != 0 {
				if diff := cmp.Diff(tt.wantOutside, outside); diff != "" {
					t.Errorf("outside-root diff (-want +got):\n%s", diff)
				}
			}
			cmd.Args = append([]string(nil), args...)
			err := applyPathRelativization(&action{cmd: cmd, windowsCross: windowsCross})
			wantError := len(tt.wantOutside) > 0
			if errors.Is(err, errPathsOutsideExecRoot) != wantError {
				t.Errorf("applyPathRelativization() = %v; want outside-root error: %v", err, wantError)
			}
		})
	}
}

func TestRelativizeDirectorySuffixes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX paths")
	}
	cmd := &command.Command{ExecRoot: "/root", WorkingDir: "build", Args: []string{
		"/root/bin/clang", "-iprefix", "/root/", "-iwithprefix", "inc",
		"-iprefix/root/build/", "--sysroot=/root/sys/", "-I/root/include/",
	}}
	got, outside, _ := relativizePaths(cmd, false)
	want := []string{"../bin/clang", "-iprefix", "../", "-iwithprefix", "inc",
		"-iprefix./", "--sysroot=../sys/", "-I../include/"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatal(diff)
	}
	if len(outside) != 0 {
		t.Fatalf("unexpected outside paths: %v", outside)
	}
}

func TestRelativizeLiteralOperands(t *testing.T) {
	for _, forwarded := range []bool{false, true} {
		t.Run(map[bool]string{false: "GNU", true: "clang-cl forwarded"}[forwarded], func(t *testing.T) {
			exe, root, source := "clang", "/work", "/work/src/a.c"
			operands := []string{"-MT", "/work/build/a.o", "-MQ", "/outside/target",
				"-MT/work/build/joined.o", "-MQ/outside/joined", "-D", "/outside/definition",
				"--system-header-prefix", "/outside/prefix/"}
			if forwarded {
				exe, root, source = "clang-cl.exe", `C:\work`, `c:\WORK\src\a.c`
				for i := range operands {
					operands[i] = "/clang:" + operands[i]
				}
			} else if runtime.GOOS == "windows" {
				t.Skip("POSIX paths")
			}
			args := append([]string{exe}, operands...)
			args = append(args, "-c", source)
			cmd := &command.Command{Identifiers: &command.Identifiers{}, ExecRoot: root, WorkingDir: "build", Args: args}
			want := append([]string{exe}, operands...)
			want = append(want, "-c", "../src/a.c")
			if err := applyPathRelativization(&action{cmd: cmd, windowsCross: forwarded}); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(want, cmd.Args); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestRelativizeWindowsPaths(t *testing.T) {
	for _, cross := range []bool{false, true} {
		for _, root := range []string{`C:\root`, `\\server\share\root`} {
			t.Run(root+map[bool]string{false: "/native", true: "/cross"}[cross], func(t *testing.T) {
				varied := strings.ToUpper(root)
				args := []string{varied + `\build\clang-cl.exe`, `/Fo` + varied + `\build\`, `/Fd:` + varied + `\build/`,
					"-I", varied + `\include\`, "/c", varied + `\src\a.cc`}
				sep := "/"
				if !cross {
					sep = `\`
				}
				want := []string{"." + sep + "clang-cl.exe", "/Fo." + sep, "/Fd:." + sep,
					"-I", ".." + sep + "include" + sep, "/c", ".." + sep + "src" + sep + "a.cc"}
				cmd := &command.Command{Identifiers: &command.Identifiers{}, ExecRoot: root, WorkingDir: "build", Args: args}
				if err := applyPathRelativization(&action{cmd: cmd, windowsCross: cross}); err != nil {
					t.Fatal(err)
				}
				if diff := cmp.Diff(want, cmd.Args); diff != "" {
					t.Fatal(diff)
				}
			})
		}
	}
}

func TestRelativizeWindowsRootBoundaries(t *testing.T) {
	for _, tt := range []struct {
		root, source, want string
		outside            bool
	}{
		{`C:\root`, `c:\ROOTED\a.cc`, `c:\ROOTED\a.cc`, true},
		{`C:\root`, `D:\root\a.cc`, `D:\root\a.cc`, true},
		{`\\server\share\root`, `\\server\other\root\a.cc`, `\\server\other\root\a.cc`, true},
		{`C:\`, `c:\..\src\a.cc`, `../src/a.cc`, false},
		{`\\server\share`, `\\SERVER\SHARE\..\src\a.cc`, `../src/a.cc`, false},
	} {
		t.Run(tt.source, func(t *testing.T) {
			cmd := &command.Command{Identifiers: &command.Identifiers{}, ExecRoot: tt.root, WorkingDir: "build", Args: []string{"clang-cl.exe", "/c", tt.source}}
			got, outside, _ := relativizePaths(cmd, true)
			if diff := cmp.Diff([]string{"clang-cl.exe", "/c", tt.want}, got); diff != "" {
				t.Fatal(diff)
			}
			if (len(outside) > 0) != tt.outside {
				t.Fatalf("outside paths = %v; want outside=%v", outside, tt.outside)
			}
		})
	}
}
