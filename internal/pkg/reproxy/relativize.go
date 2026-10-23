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
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"

	log "github.com/golang/glog"
)

// Build systems such as CMake bake absolute paths into every compile command.
// Those paths cannot exist on a remote worker, and even when they do they
// defeat cache sharing between two checkouts at different locations.
// relativizePaths rewrites every absolute path in cmd.Args that lives
// under the exec root into a path relative to the action's working directory,
// which is how the remote worker will see it.
//
// Paths outside the exec root cannot be expressed in the remote input root at
// all, so they are left alone and reported: an action that depends on one is
// not hermetic, and saying so here is much cheaper than debugging the
// remote-only failure it causes later.

// The include-path-management options are taken from clang's
// ClangCommandLineReference ("Include path management"). Nearly all of them
// name a directory or a file, and missing one means an absolute path silently
// survives into the remote command -- so the list is transcribed from the
// reference rather than assembled from the flags a particular build happens to
// use.
//
// Deliberately absent from both tables:
//
//   --system-header-prefix / --no-system-header-prefix take a header-name
//   prefix, not a filesystem path (see noRewriteEqFlags).
//
//   -MT and -MQ take a make target name. It is usually the output path, but it
//   is written verbatim into the .d file, and the outputs are already relative.
//
//   -fbuild-session-timestamp, -fmodules-prune-after and -fmodules-prune-
//   interval take numbers.
//
// Flags spelled "--name=value" need no entry at all: the "=" branch of the
// rewriter handles any of them generically.

// pathArgFlags are flags whose path argument is a separate argv entry, e.g.
// "-I" "/abs/dir".
var pathArgFlags = map[string]bool{
	"-I":                           true,
	"-cxx-isystem":                 true,
	"-iapinotes-modules":           true,
	"-idirafter":                   true,
	"-iframework":                  true,
	"-iframeworkwithsysroot":       true,
	"-imacros":                     true,
	"-include":                     true,
	"-include-pch":                 true,
	"-iprefix":                     true,
	"-iquote":                      true,
	"-isysroot":                    true,
	"-isystem":                     true,
	"-isystem-after":               true,
	"-ivfsoverlay":                 true,
	"-iwithprefix":                 true,
	"-iwithprefixbefore":           true,
	"-iwithsysroot":                true,
	"-stdlib++-isystem":            true,
	"-fmodules-user-build-path":    true,
	"--include":                    true,
	"--include-directory":          true,
	"--include-directory-after":    true,
	"--include-prefix":             true,
	"--include-with-prefix":        true,
	"--include-with-prefix-after":  true,
	"--include-with-prefix-before": true,
	"--imacros":                    true,
	"--sysroot":                    true,
	"-internal-isystem":            true,
	"-internal-externc-isystem":    true,
	"-L":                           true,
	"-B":                           true,
	"-F":                           true,
	"-o":                           true,
	"-MF":                          true,
	"-resource-dir":                true,
	"--serialize-diagnostics":      true,
	"-vfsoverlay":                  true,
}

// joinedPathFlags are flag prefixes whose path argument may be joined onto the
// flag itself, e.g. "-I/abs/dir". cutJoined takes the longest match, so a flag
// that is a prefix of another (-isystem of -isystem-after, -iwithprefix of
// -iwithprefixbefore, -iframework of -iframeworkwithsysroot) is safe here
// regardless of order.
var joinedPathFlags = []string{
	"-I",
	"-cxx-isystem",
	"-iapinotes-modules",
	"-idirafter",
	"-iframework",
	"-iframeworkwithsysroot",
	"-imacros",
	"-include",
	"-iprefix",
	"-iquote",
	"-isysroot",
	"-isystem",
	"-isystem-after",
	"-ivfsoverlay",
	"-iwithprefix",
	"-iwithprefixbefore",
	"-iwithsysroot",
	"-stdlib++-isystem",
	"--imacros",
	"--include",
	"-L",
	"-B",
	"-F",
	"-MF",
}

// noRewriteEqFlags are "-x=y" style flags whose value must not be rewritten
// even when it looks like a path: preprocessor definitions end up in the
// compiled source, where a working-directory-relative path means something
// different than it does on the command line.
// --system-header-prefix and its negation take a header-name prefix such as
// "boost/", matched against the spelling in an #include directive. It is not a
// filesystem path and relativizing it would silently change which headers are
// treated as system headers.
var noRewriteEqFlags = []string{
	"-D", "-U", "-W",
	"--system-header-prefix",
	"--no-system-header-prefix",
}

// pseudoFsPrefixes are kernel-virtual filesystems. Nothing under them can be
// uploaded into an input root, and nothing under them needs to be: they exist
// on the worker too, and mean the same thing there. Rewriting them is actively
// harmful -- "-working-directory=/proc/self/cwd" is a standard way to make a
// build's recorded paths location-independent, and turning it into a relative
// path pointing into the input root breaks resolution of every other relative
// path on the command line. They only look rewritable when the exec root is /.
var pseudoFsPrefixes = []string{"/proc", "/sys", "/dev"}

func isPseudoFsPath(p string) bool {
	for _, prefix := range pseudoFsPrefixes {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
}

// argRewriter rewrites absolute paths in a single action's arguments.
type argRewriter struct {
	// root is the exec root: paths underneath it can be expressed relative to
	// the working directory and reached in the remote input root.
	root string
	// rootPrefix is root with a trailing separator, so that a prefix test does
	// not match a sibling like "/works". Kept separate because root may already
	// end in a separator when it is "/".
	rootPrefix string
	// base is the absolute working directory the remote command runs in.
	base string
	// winRoot and winBase are the Windows-style (drive-letter or UNC,
	// backslash-or-forward-slash) equivalents of root and base, used only for
	// clang-cl (MSVC-style) path arguments. They are computed independently of
	// root/base and of path/filepath: filepath's behaviour is native-GOOS, and
	// a Windows-style path such as "C:\t" is not recognized as absolute by
	// filepath built for a POSIX GOOS, while a POSIX path is meaningless to
	// filepath built for Windows. relativize.go must get this right regardless
	// of which GOOS it is compiled for (windowsCross means the *client*, not
	// necessarily this process, is Windows), so clang-cl path handling is
	// spelled out with its own "/"-normalized string logic instead. See
	// isWindowsAbs, cleanWindowsPath, winUnderRoot, relWinPath below.
	winRoot string
	winBase string
	// windowsCross is true when the remote worker's OS differs from the
	// client's (reproxy/server.go:isWindowsCross): today that only fires for a
	// Windows client and a non-Windows worker. It controls the separator of a
	// rewritten clang-cl path: a forward slash for the Linux worker to
	// understand, a backslash otherwise (matching what clang-cl itself would
	// have produced).
	windowsCross bool
	// outside collects absolute paths that are not under root, in the order
	// they were first seen.
	outside []string
	// responseFiles collects @file arguments. Their *contents* are not
	// rewritten, so an action that puts absolute paths in one is not made
	// remote-safe by this pass and must not be reported as though it were.
	responseFiles []string
	seen          map[string]bool
}

func newArgRewriter(cmd *command.Command, windowsCross bool) *argRewriter {
	root := filepath.Clean(cmd.ExecRoot)
	sep := string(filepath.Separator)
	rootPrefix := root
	if !strings.HasSuffix(rootPrefix, sep) {
		rootPrefix += sep
	}
	winRoot := cleanWindowsPath(cmd.ExecRoot)
	winBase := winRoot
	if cmd.WorkingDir != "" {
		if isWindowsAbs(cmd.WorkingDir) {
			winBase = cleanWindowsPath(cmd.WorkingDir)
		} else {
			winBase = cleanWindowsPath(winRoot + "/" + cmd.WorkingDir)
		}
	}
	return &argRewriter{
		root:         root,
		rootPrefix:   rootPrefix,
		base:         filepath.Join(root, cmd.WorkingDir),
		winRoot:      winRoot,
		winBase:      winBase,
		windowsCross: windowsCross,
		seen:         map[string]bool{},
	}
}

// underRoot reports whether p, which must be absolute and clean, is inside the
// exec root.
func (r *argRewriter) underRoot(p string) bool {
	if p == r.root {
		return true
	}
	return strings.HasPrefix(p, r.rootPrefix)
}

// rewrite returns the working-directory-relative form of the absolute path p,
// or p unchanged if it cannot be expressed in the remote input root.
func (r *argRewriter) rewrite(p string) string {
	if !filepath.IsAbs(p) {
		return r.slashRel(p)
	}
	clean := filepath.Clean(p)
	if isPseudoFsPath(clean) {
		log.V(1).Infof("leaving pseudo-filesystem path %s alone", clean)
		return p
	}
	if !r.underRoot(clean) {
		if !r.seen[clean] {
			r.seen[clean] = true
			r.outside = append(r.outside, clean)
		}
		return p
	}
	rel, err := filepath.Rel(r.base, clean)
	if err != nil {
		log.Warningf("failed to make %s relative to %s: %v", clean, r.base, err)
		return p
	}
	if r.windowsCross {
		// filepath.Rel used this process's native separator. When this
		// process is itself the Windows client of a windowsCross action (the
		// only way windowsCross is ever true, see isWindowsCross), that
		// separator is a backslash -- but the worker is Linux, and a bare
		// argument such as the source file at the end of the command line
		// (not tied to any recognized flag, so it reaches this function
		// rather than the clang-cl-specific rewriteClangClPath) needs a
		// forward slash to be found there. A no-op when this process's
		// native separator is already "/".
		rel = filepath.ToSlash(rel)
	}
	return rel
}

// slashRel converts a relative path's backslashes to forward slashes when the
// action is windowsCross. A Windows client (CMake, ninja) already emits paths
// relative to the build dir -- "/FoCMakeFiles\\t.dir\\a.obj",
// "-clang:-MFCMakeFiles\\t.dir\\a.obj.d", "src\\a.cpp" -- and nothing else
// rewrites them, but on the Linux worker "\\" is an ordinary filename
// character: clang wrote a file literally named `CMakeFiles\\t.dir\\a.obj`
// that reclient never found (the action "succeeded" with no outputs).
func (r *argRewriter) slashRel(p string) string {
	if r.windowsCross {
		return strings.ReplaceAll(p, `\`, "/")
	}
	return p
}

// rewriteExecutable rewrites argv[0].
//
// It differs from rewrite in one way that matters: a relative path with no
// separator in it is not a path at all to an exec()-style launcher, it is a
// name to look up in PATH. Rewriting /work/bin/clang++ to "clang++" for an
// action whose working directory is /work/bin would therefore silently swap in
// whatever compiler the worker happens to have installed -- the exact
// hermeticity failure this rewriting exists to prevent. Keep it a path.
func (r *argRewriter) rewriteExecutable(p string) string {
	if !filepath.IsAbs(p) {
		return p
	}
	rel := r.rewrite(p)
	// rewrite may have returned a forward-slash path (windowsCross), which
	// does not contain this process's native filepath.Separator even though
	// it plainly has directory structure -- check for either.
	if rel == p || strings.ContainsRune(rel, filepath.Separator) || strings.ContainsRune(rel, '/') {
		return rel
	}
	sep := string(filepath.Separator)
	if r.windowsCross {
		sep = "/"
	}
	return "." + sep + rel
}

// isClangClCommand reports whether argv[0] names clang-cl, which takes
// MSVC-style options.
func isClangClCommand(exe string) bool {
	base := strings.ToLower(filepath.Base(exe))
	return base == "clang-cl" || base == "clang-cl.exe"
}

// isWindowsAbs reports whether p is an absolute Windows path: a drive letter
// ("C:\..." or "C:/...") or a UNC path ("\\server\share..." or
// "//server/share..."). It exists because Windows paths appear verbatim in a
// clang-cl action's arguments regardless of which GOOS relativize.go itself
// is built for (a windowsCross action's client is Windows even when this
// binary, and the worker, are not), and neither spelling is recognized by
// path/filepath built for the other OS.
func isWindowsAbs(p string) bool {
	if len(p) >= 3 {
		c := p[0]
		isLetter := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
		if isLetter && p[1] == ':' && (p[2] == '\\' || p[2] == '/') {
			return true
		}
	}
	return strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, "//")
}

// cleanWindowsPath normalizes backslashes to forward slashes and lexically
// cleans the result (collapsing "." and ".." elements), preserving a leading
// "//" as a UNC marker. It is spelled out rather than delegated to
// path/filepath so that it behaves identically regardless of the host GOOS:
// path/filepath's separator and drive-letter handling are native, and
// clang-cl paths need Windows semantics even when this code runs, or is
// tested, on a POSIX host.
func cleanWindowsPath(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	unc := strings.HasPrefix(p, "//")
	var out []string
	for _, part := range strings.Split(p, "/") {
		switch part {
		case "", ".":
			continue
		case "..":
			if len(out) > 0 && out[len(out)-1] != ".." {
				out = out[:len(out)-1]
			}
			// Climbing above an absolute root is silently dropped, matching
			// filepath.Clean.
		default:
			out = append(out, part)
		}
	}
	prefix := ""
	if unc {
		prefix = "//"
	}
	return prefix + strings.Join(out, "/")
}

// winPathComponents splits an already-or-not-yet-cleaned Windows-style path
// into its "/"-separated components (the drive letter, e.g. "C:", is the
// first one), dropping any leading UNC marker.
func winPathComponents(p string) []string {
	clean := strings.TrimPrefix(cleanWindowsPath(p), "//")
	if clean == "" {
		return nil
	}
	return strings.Split(clean, "/")
}

// winUnderRoot reports whether p is inside root, comparing components
// case-insensitively: Windows paths are case-insensitive, and a build system
// is not guaranteed to spell a drive letter or directory name with the same
// case every time it appears on a command line.
func winUnderRoot(root, p string) bool {
	rootParts := winPathComponents(root)
	pParts := winPathComponents(p)
	if len(pParts) < len(rootParts) {
		return false
	}
	for i, rp := range rootParts {
		if !strings.EqualFold(rp, pParts[i]) {
			return false
		}
	}
	return true
}

// relWinPath returns target relative to base, both Windows-style absolute
// paths, joined with "/". The common prefix is matched case-insensitively for
// the same reason as winUnderRoot; components beyond the divergence point
// keep their original spelling.
func relWinPath(base, target string) string {
	baseParts := winPathComponents(base)
	targetParts := winPathComponents(target)
	n := 0
	for n < len(baseParts) && n < len(targetParts) && strings.EqualFold(baseParts[n], targetParts[n]) {
		n++
	}
	parts := make([]string, 0, len(baseParts)-n+len(targetParts)-n)
	for i := n; i < len(baseParts); i++ {
		parts = append(parts, "..")
	}
	parts = append(parts, targetParts[n:]...)
	if len(parts) == 0 {
		return "."
	}
	return strings.Join(parts, "/")
}

// rewriteWindowsPath is rewrite's clang-cl counterpart: p is a Windows-style
// absolute path (isWindowsAbs(p) is true). It is made relative to the
// action's working directory when it falls under the exec root, with a
// forward-slash separator for a windowsCross action (the worker is Linux) or
// a backslash otherwise (matching clang-cl's own spelling).
func (r *argRewriter) rewriteWindowsPath(p string) string {
	clean := cleanWindowsPath(p)
	if !winUnderRoot(r.winRoot, clean) {
		key := "win:" + strings.ToLower(clean)
		if !r.seen[key] {
			r.seen[key] = true
			r.outside = append(r.outside, p)
		}
		return p
	}
	rel := relWinPath(r.winBase, clean)
	if r.windowsCross {
		return rel
	}
	return strings.ReplaceAll(rel, "/", `\`)
}

// rewriteClangClPath rewrites the path argument of a clang-cl (MSVC-style)
// flag. The argument may be Windows-style (a drive letter or UNC root -- the
// common case for a real Windows client) or POSIX-style (clang-cl invoked on
// Linux to cross-compile for a Windows target against a sysroot copied onto
// the same host): isWindowsAbs distinguishes the two, and the POSIX case
// reuses the ordinary rewrite logic.
func (r *argRewriter) rewriteClangClPath(p string) string {
	if isWindowsAbs(p) {
		return r.rewriteWindowsPath(p)
	}
	return r.rewrite(p)
}

// clangClSeparateFlags are clang-cl (MSVC-style) flags whose path argument is
// a separate argv entry, e.g. "/I" "C:\abs\dir". clang-cl's CLOption table
// accepts either a "/" or a "-" prefix for all of them, so both spellings are
// recognized (clangClPrefix strips whichever one is present).
var clangClSeparateFlags = map[string]bool{
	"FI":         true,
	"I":          true,
	"imsvc":      true,
	"winsysroot": true,
	"external:I": true,
}

// clangClJoinedFlags are the same flags' prefixes, whose path argument may
// instead be joined directly onto the flag, e.g. "/IC:\abs\dir". None of
// these is a prefix of another entry in this table, so unlike cutJoined no
// longest-match handling is needed, but the loop is written the same way in
// case the table grows.
var clangClJoinedFlags = []string{
	"FI",
	"I",
	"imsvc",
	"winsysroot",
	"external:I",
}

// clangClOutputFlags are clang-cl's output-path flags. Only the joined
// ("/Fo<path>") and colon-joined ("/Fo:<path>") spellings are valid cl
// syntax -- a separate-argument "/Fo" "<path>" makes the path a second input
// file instead -- so, unlike clangClSeparateFlags, there is no separate-arg
// form to recognize here.
var clangClOutputFlags = []string{"Fo", "Fe", "Fd"}

// clangClPrefix reports whether arg begins with a clang-cl option prefix ("/"
// or "-") and returns the rest of the argument.
func clangClPrefix(arg string) (body string, ok bool) {
	if len(arg) < 2 {
		return "", false
	}
	if arg[0] != '/' && arg[0] != '-' {
		return "", false
	}
	return arg[1:], true
}

// cutClangClJoined splits an argument like "/IC:\abs\dir" into its flag
// (with the original prefix character and case) and its value.
func cutClangClJoined(arg string) (flag, val string, ok bool) {
	body, prefixOK := clangClPrefix(arg)
	if !prefixOK {
		return "", "", false
	}
	for _, f := range clangClJoinedFlags {
		if len(body) > len(f) && strings.HasPrefix(body, f) {
			return arg[:1+len(f)], body[len(f):], true
		}
	}
	return "", "", false
}

// cutClangClOutput splits an output-path argument such as "/Fo<path>" or
// "/Fo:<path>" into its flag (colon included, when present) and its value.
func cutClangClOutput(arg string) (flag, val string, ok bool) {
	body, prefixOK := clangClPrefix(arg)
	if !prefixOK {
		return "", "", false
	}
	for _, f := range clangClOutputFlags {
		if !strings.HasPrefix(body, f) {
			continue
		}
		rest := body[len(f):]
		flag = arg[:1+len(f)]
		if strings.HasPrefix(rest, ":") {
			flag += ":"
			rest = rest[1:]
		}
		if rest == "" {
			// "/Fo" alone with nothing joined on: not valid cl syntax, and not
			// the flag this function recognizes.
			continue
		}
		return flag, rest, true
	}
	return "", "", false
}

// cutClangPassthrough splits "/clang:<rest>" or "-clang:<rest>" into its
// prefix and the pass-through argument, which clang-cl forwards to the clang
// driver verbatim -- so a GNU-style path flag such as "-I<dir>" can appear
// nested inside it.
func cutClangPassthrough(arg string) (prefix, rest string, ok bool) {
	for _, p := range [...]string{"/clang:", "-clang:"} {
		if strings.HasPrefix(arg, p) {
			return p, arg[len(p):], true
		}
	}
	return "", "", false
}

// relativizePaths returns cmd.Args with every absolute path under the exec
// root replaced by its working-directory-relative equivalent, the absolute
// paths it could not rewrite because they fall outside the exec root, and any
// response files it encountered, whose contents it cannot rewrite.
//
// windowsCross selects the separator used for a rewritten clang-cl path (see
// rewriteWindowsPath); it has no effect on GNU-style rewriting, which always
// uses the host's native separator via path/filepath, unchanged from before
// clang-cl support was added.
func relativizePaths(cmd *command.Command, windowsCross bool) (args []string, outsideRoot []string, responseFiles []string) {
	r := newArgRewriter(cmd, windowsCross)
	in := cmd.Args
	out := make([]string, 0, len(in))
	// clang-cl takes MSVC-style options that begin with '/', and on a posix
	// host filepath.IsAbs("/std:c++17") is true. Treating those as paths
	// mangles them and reports them as out-of-root hermeticity problems.
	clangCl := len(in) > 0 && isClangClCommand(in[0])
	for i := 0; i < len(in); i++ {
		arg := in[i]
		// The executable itself must be named the way the worker will see it.
		if i == 0 {
			out = append(out, r.rewriteExecutable(arg))
			continue
		}
		// "-I" "/abs/dir": the path is the next argument.
		if pathArgFlags[arg] && i+1 < len(in) {
			out = append(out, arg, r.rewrite(in[i+1]))
			i++
			continue
		}
		// "@file": rewrite where the response file lives so the worker can
		// open it. Its contents are a separate problem, tracked below.
		if strings.HasPrefix(arg, "@") && len(arg) > 1 {
			rsp := arg[1:]
			if filepath.IsAbs(rsp) {
				r.responseFiles = append(r.responseFiles, rsp)
			}
			out = append(out, "@"+r.rewrite(rsp))
			continue
		}
		// clang-cl (MSVC-style) path flags. This must run before the GNU-style
		// "-" branch below: several of these flags (-imsvc, -winsysroot,
		// -external:I) are not in the GNU tables, and would otherwise fall
		// through that branch's final "leave it alone" case unrewritten. A flag
		// that already has a GNU-table entry under its "-" spelling (-I) is
		// intercepted earlier, at the pathArgFlags/cutJoined sites shared with
		// non-clang-cl commands, so reaching here with body=="I" only happens
		// for the "/I" spelling.
		if clangCl {
			// "/clang:-I<dir>" (joined) or "/clang:-I" "/clang:<dir>" (separate):
			// clang: forwards the rest of the argument to the clang driver
			// verbatim, so a GNU-style path flag can appear nested inside it. The
			// two-token separate form mirrors how this codebase's own clang-cl
			// flag parser treats "/clang:-MF" "/clang:<path>"
			// (clangCLState.handleClangCLFlags in
			// inputprocessor/action/clangcl/flagsparser.go).
			if cprefix, rest, ok := cutClangPassthrough(arg); ok {
				if flag, val, ok := cutJoined(rest); ok {
					out = append(out, cprefix+flag+r.rewriteClangClPath(strings.TrimSpace(val)))
					continue
				}
				if pathArgFlags[rest] && i+1 < len(in) {
					if nprefix, nrest, ok := cutClangPassthrough(in[i+1]); ok {
						out = append(out, arg, nprefix+r.rewriteClangClPath(nrest))
						i++
						continue
					}
				}
				out = append(out, arg)
				continue
			}
			// "/I" "dir", "-I" "dir", "/imsvc" "dir", "-imsvc" "dir", "/winsysroot"
			// "dir", "-winsysroot" "dir", "/external:I" "dir", "-external:I"
			// "dir": the path is the next argument.
			if body, ok := clangClPrefix(arg); ok && clangClSeparateFlags[body] && i+1 < len(in) {
				out = append(out, arg, r.rewriteClangClPath(in[i+1]))
				i++
				continue
			}
			// "/Idir", "/imsvcdir", "/winsysrootdir", "/external:Idir" (and the
			// "-" spellings): the path joined onto the flag.
			if flag, val, ok := cutClangClJoined(arg); ok {
				out = append(out, flag+r.rewriteClangClPath(val))
				continue
			}
			// "/Fo<path>", "/Fo:<path>", "/Fe<path>", "/Fe:<path>", "/Fd<path>",
			// "/Fd:<path>": output paths.
			if flag, val, ok := cutClangClOutput(arg); ok {
				out = append(out, flag+r.rewriteClangClPath(val))
				continue
			}
		}
		if strings.HasPrefix(arg, "-") {
			if flag, val, ok := strings.Cut(arg, "="); ok {
				// A value containing another '=' is a pair, not a path:
				// -ffile-prefix-map=/old=/new, -fdebug-prefix-map=...,
				// -fmodule-file=Name=/abs/x.pcm. Relativizing the whole thing
				// yields "../old=/new", a mapping that matches nothing, which
				// silently changes recorded paths rather than failing.
				if noRewriteEq(flag) || strings.Contains(val, "=") {
					if filepath.IsAbs(val) || strings.Contains(val, "=") {
						log.V(1).Infof("not rewriting value of %s", arg)
					}
					out = append(out, arg)
					continue
				}
				out = append(out, flag+"="+r.rewrite(val))
				continue
			}
			// "-I/abs/dir": the path is joined onto the flag.
			if flag, val, ok := cutJoined(arg); ok {
				out = append(out, flag+r.rewrite(strings.TrimSpace(val)))
				continue
			}
			out = append(out, arg)
			continue
		}
		if clangCl && strings.HasPrefix(arg, "/") {
			// An MSVC-style option not recognized above, such as /std:c++17.
			// It is left alone rather than guessed at: only the specific
			// path-taking flags handled above are known to name a filesystem
			// path.
			out = append(out, arg)
			continue
		}
		// A bare argument, e.g. the source file. (Not a path if it carries quoting or
		// an assignment: leave those exactly as given.)
		if strings.ContainsAny(arg, `"'=`) {
			out = append(out, arg)
			continue
		}
		out = append(out, r.rewrite(arg))
	}
	return out, r.outside, r.responseFiles
}

func noRewriteEq(flag string) bool {
	for _, p := range noRewriteEqFlags {
		if strings.HasPrefix(flag, p) {
			return true
		}
	}
	return false
}

// cutJoined splits an argument like "-I/abs/dir" into its flag and value. It
// returns ok=false when no known path flag is joined onto the argument.
//
// The longest matching flag wins. Several of these flags are prefixes of each
// other, and taking the first match would split "-isystem-after/dir" into
// "-isystem" and "-after/dir" -- which is not an absolute path, so the
// directory would be left un-rewritten with no diagnostic at all.
func cutJoined(arg string) (flag, val string, ok bool) {
	for _, f := range joinedPathFlags {
		if len(arg) > len(f) && strings.HasPrefix(arg, f) && len(f) > len(flag) {
			flag, val, ok = f, arg[len(f):], true
		}
	}
	return flag, val, ok
}

// errPathsOutsideExecRoot marks a failure caused by
// -fail_on_paths_outside_exec_root. It is distinguishable so that the action
// is not then retried locally: falling back would turn "this action is not
// hermetic" into a green build, which is the opposite of what the flag is for.
var errPathsOutsideExecRoot = errors.New("command references absolute paths outside the exec root")

// applyPathRelativization rewrites absolute paths in the action's arguments and
// reports any that could not be rewritten. It returns an error when the action
// is configured to treat those as fatal, so that a non-hermetic action fails
// here rather than mysteriously on a worker.
func applyPathRelativization(a *action, failOnOutside bool) error {
	args, outside, responseFiles := relativizePaths(a.cmd, a.windowsCross)
	a.cmd.Args = args
	if len(responseFiles) > 0 {
		// Say this even when nothing else looks wrong: the rewriting pass
		// cannot see inside a response file, so a clean report here does not
		// mean the action's paths are all relative.
		log.Warningf("%v: response file contents are not rewritten; absolute paths inside %v will not resolve on a worker",
			a.cmd.Identifiers.ExecutionID, responseFiles)
	}
	if len(outside) == 0 {
		return nil
	}
	msg := fmt.Sprintf("%v: command references absolute paths outside exec root %s, which cannot be sent to a remote worker: %v",
		a.cmd.Identifiers.ExecutionID, a.cmd.ExecRoot, outside)
	if failOnOutside {
		return fmt.Errorf("%s: %w", msg, errPathsOutsideExecRoot)
	}
	log.Warning(msg)
	return nil
}
