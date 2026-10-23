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
	"path"
	"path/filepath"
	"strings"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	log "github.com/golang/glog"
)

// optionForm describes syntax and whether the operand is a path. Literal
// operands must be consumed too: -MT /checkout/a.o names a make target, not
// an input file. One table per dialect covers both joined and separate forms.
type optionForm uint8

const (
	separate optionForm = 1 << iota
	joined
	literal
)

var gnuOptions = map[string]optionForm{
	"-I":                           separate | joined,
	"-cxx-isystem":                 separate | joined,
	"-iapinotes-modules":           separate | joined,
	"-idirafter":                   separate | joined,
	"-iframework":                  separate | joined,
	"-iframeworkwithsysroot":       separate | joined,
	"-imacros":                     separate | joined,
	"-include":                     separate | joined,
	"-include-pch":                 separate,
	"-iprefix":                     separate | joined,
	"-iquote":                      separate | joined,
	"-isysroot":                    separate | joined,
	"-isystem":                     separate | joined,
	"-isystem-after":               separate | joined,
	"-ivfsoverlay":                 separate | joined,
	"-iwithprefix":                 separate | joined,
	"-iwithprefixbefore":           separate | joined,
	"-iwithsysroot":                separate | joined,
	"-stdlib++-isystem":            separate | joined,
	"-fmodules-user-build-path":    separate,
	"--include":                    separate | joined,
	"--include-directory":          separate,
	"--include-directory-after":    separate,
	"--include-prefix":             separate,
	"--include-with-prefix":        separate,
	"--include-with-prefix-after":  separate,
	"--include-with-prefix-before": separate,
	"--imacros":                    separate | joined,
	"--sysroot":                    separate,
	"-internal-isystem":            separate,
	"-internal-externc-isystem":    separate,
	"-L":                           separate | joined,
	"-B":                           separate | joined,
	"-F":                           separate | joined,
	"-o":                           separate,
	"-MF":                          separate | joined,
	"-resource-dir":                separate,
	"--serialize-diagnostics":      separate,
	"-vfsoverlay":                  separate,
	"-MT":                          separate | joined | literal,
	"-MQ":                          separate | joined | literal,
	"-D":                           separate | joined | literal,
	"-U":                           separate | joined | literal,
	"-W":                           joined | literal,
	"--system-header-prefix":       separate | literal,
	"--no-system-header-prefix":    separate | literal,
}

// clang-cl accepts either / or - before these names. Output flags also accept
// a colon, but unlike include flags do not take a separate operand.
var clangClOptions = map[string]optionForm{
	"I":          separate | joined,
	"FI":         separate | joined,
	"imsvc":      separate | joined,
	"winsysroot": separate | joined,
	"external:I": separate | joined,
	"Fo":         joined,
	"Fo:":        joined,
	"Fe":         joined,
	"Fe:":        joined,
	"Fd":         joined,
	"Fd:":        joined,
}

// argRewriter maps all path operands, including executables and positional
// inputs, through the same root/working-directory policy. Windows spellings
// work on any host so the cross-worker cases can also be tested on Linux.
type argRewriter struct {
	root, base             string
	windowsCross           bool
	outside, responseFiles []string
	seen                   map[string]bool
}

func newArgRewriter(cmd *command.Command, windowsCross bool) *argRewriter {
	root, base := filepath.Clean(cmd.ExecRoot), filepath.Join(cmd.ExecRoot, cmd.WorkingDir)
	if isWindowsAbs(cmd.ExecRoot) {
		root = cleanWindowsPath(cmd.ExecRoot)
		base = cleanWindowsPath(root + "/" + cmd.WorkingDir)
		if isWindowsAbs(cmd.WorkingDir) {
			base = cleanWindowsPath(cmd.WorkingDir)
		}
	}
	return &argRewriter{root: root, base: base, windowsCross: windowsCross, seen: map[string]bool{}}
}

func isWindowsAbs(p string) bool {
	if len(p) >= 3 && p[1] == ':' && (p[2] == '/' || p[2] == '\\') {
		c := p[0]
		return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
	}
	return strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, "//")
}

// Use path.Clean for lexical normalization, keeping the volume outside it so
// '..' cannot remove a drive or a UNC server/share. filepath is host-specific.
func cleanWindowsPath(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	if len(p) >= 2 && p[1] == ':' {
		return p[:2] + path.Clean("/"+p[2:])
	}
	if strings.HasPrefix(p, "//") {
		parts := strings.SplitN(p[2:], "/", 3)
		volume := "//" + strings.Join(parts[:min(2, len(parts))], "/")
		tail := ""
		if len(parts) == 3 {
			tail = parts[2]
		}
		return volume + strings.TrimSuffix(path.Clean("/"+tail), "/")
	}
	return path.Clean(p)
}

func winPathComponents(p string) []string {
	return strings.Split(strings.Trim(cleanWindowsPath(p), "/"), "/")
}

func commonWindowsPrefix(a, b []string) int {
	n := 0
	for n < len(a) && n < len(b) && strings.EqualFold(a[n], b[n]) {
		n++
	}
	return n
}

func (r *argRewriter) slashRel(p string) string {
	if r.windowsCross {
		return strings.ReplaceAll(p, `\`, "/")
	}
	return p
}

// rewrite preserves a trailing separator: /Fo. is a filename, whereas /Fo./
// names a directory. The same distinction matters to GNU -iprefix concatenation.
func (r *argRewriter) rewrite(p string) string {
	windows := isWindowsAbs(p)
	if !windows && !filepath.IsAbs(p) {
		return r.slashRel(p)
	}
	clean, rel, sep := filepath.Clean(p), "", string(filepath.Separator)
	inside := false
	if windows {
		clean = cleanWindowsPath(p)
		rootParts, targetParts := winPathComponents(r.root), winPathComponents(clean)
		inside = commonWindowsPrefix(rootParts, targetParts) == len(rootParts)
		if inside {
			baseParts := winPathComponents(r.base)
			n := commonWindowsPrefix(baseParts, targetParts)
			rel = path.Clean(strings.Repeat("../", len(baseParts)-n) + strings.Join(targetParts[n:], "/"))
		}
		sep = `\`
		if r.windowsCross {
			sep = "/"
		}
	} else {
		for _, prefix := range []string{"/proc", "/sys", "/dev"} {
			if clean == prefix || strings.HasPrefix(clean, prefix+"/") {
				return p
			}
		}
		under, err := filepath.Rel(r.root, clean)
		inside = err == nil && under != ".." && !strings.HasPrefix(under, ".."+string(filepath.Separator))
		if inside {
			rel, err = filepath.Rel(r.base, clean)
			if err != nil {
				return p
			}
		}
		if r.windowsCross {
			sep = "/"
		}
	}
	if !inside {
		key := clean
		if windows {
			key = "win:" + strings.ToLower(clean)
		}
		if !r.seen[key] {
			r.seen[key] = true
			if windows {
				r.outside = append(r.outside, p)
			} else {
				r.outside = append(r.outside, clean)
			}
		}
		return p
	}
	if windows || r.windowsCross {
		rel = strings.ReplaceAll(rel, `\`, "/")
		rel = strings.ReplaceAll(rel, "/", sep)
	}
	if (strings.HasSuffix(p, "/") || windows && strings.HasSuffix(p, `\`)) && !strings.HasSuffix(rel, sep) {
		rel += sep
	}
	return rel
}

func (r *argRewriter) rewriteExecutable(p string) string {
	if !filepath.IsAbs(p) && !isWindowsAbs(p) {
		return r.slashRel(p)
	}
	rel := r.rewrite(p)
	// A bare executable name would select the worker's PATH instead of our input.
	if rel != p && !strings.ContainsAny(rel, `/\`) {
		sep := string(filepath.Separator)
		if isWindowsAbs(p) {
			sep = `\`
		}
		if r.windowsCross {
			sep = "/"
		}
		rel = "." + sep + rel
	}
	return rel
}

func isClangClCommand(exe string) bool {
	base := strings.ToLower(path.Base(strings.ReplaceAll(exe, `\`, "/")))
	return base == "clang-cl" || base == "clang-cl.exe"
}

func cutClangPassthrough(arg string) (prefix, rest string, ok bool) {
	for _, p := range []string{"/clang:", "-clang:"} {
		if strings.HasPrefix(arg, p) {
			return p, arg[len(p):], true
		}
	}
	return "", "", false
}

// rewriteOption edits a recognized option in place and returns how many argv
// entries it consumed. Zero means unknown. The same grammar handles GNU,
// clang-cl, and GNU options forwarded through /clang:.
func (r *argRewriter) rewriteOption(args []string, options map[string]optionForm, equals bool) int {
	arg := args[0]
	if form, ok := options[arg]; ok {
		if form&separate != 0 && len(args) > 1 {
			if form&literal == 0 {
				args[1] = r.rewrite(args[1])
			}
			return 2
		}
		return 1
	}
	flag := ""
	for name, form := range options {
		if form&joined != 0 && len(arg) > len(name) && strings.HasPrefix(arg, name) && len(name) > len(flag) {
			flag = name
		}
	}
	if options[flag]&literal != 0 {
		return 1
	}
	if equals {
		if flag, value, ok := strings.Cut(arg, "="); ok {
			// Prefix-map old=new pairs and definitions are not filesystem paths.
			if options[flag]&literal == 0 && !strings.Contains(value, "=") {
				args[0] = flag + "=" + r.rewrite(value)
			}
			return 1
		}
	}
	if flag == "" {
		return 0
	}
	args[0] = flag + r.rewrite(strings.TrimSpace(arg[len(flag):]))
	return 1
}

// relativizePaths rewrites paths under the execution root before input
// processing. Response-file contents are not rewritten; report their presence.
func relativizePaths(cmd *command.Command, windowsCross bool) (args []string, outsideRoot []string, responseFiles []string) {
	r := newArgRewriter(cmd, windowsCross)
	out := append([]string(nil), cmd.Args...)
	if len(out) == 0 {
		return out, nil, nil
	}
	clangCl := isClangClCommand(out[0])
	out[0] = r.rewriteExecutable(out[0])
	for i := 1; i < len(out); {
		arg := out[i]
		if strings.HasPrefix(arg, "@") && len(arg) > 1 {
			rsp := arg[1:]
			if filepath.IsAbs(rsp) || isWindowsAbs(rsp) {
				r.responseFiles = append(r.responseFiles, rsp)
			}
			out[i] = "@" + r.rewrite(rsp)
		} else if prefix, rest, ok := cutClangPassthrough(arg); clangCl && ok {
			forwarded := []string{rest}
			nextPrefix := ""
			if i+1 < len(out) {
				if p, value, ok := cutClangPassthrough(out[i+1]); ok {
					nextPrefix = p
					forwarded = append(forwarded, value)
				}
			}
			n := r.rewriteOption(forwarded, gnuOptions, true)
			out[i] = prefix + forwarded[0]
			if n == 2 {
				out[i+1] = nextPrefix + forwarded[1]
				i += 2
				continue
			}
		} else {
			if clangCl && len(arg) > 1 && (arg[0] == '/' || arg[0] == '-') {
				values := []string{arg[1:]}
				if i+1 < len(out) {
					values = append(values, out[i+1])
				}
				if n := r.rewriteOption(values, clangClOptions, false); n > 0 {
					out[i] = arg[:1] + values[0]
					if n == 2 {
						out[i+1] = values[1]
					}
					i += n
					continue
				}
			}
			if strings.HasPrefix(arg, "-") {
				if n := r.rewriteOption(out[i:], gnuOptions, true); n > 0 {
					i += n
					continue
				}
			} else if !(clangCl && strings.HasPrefix(arg, "/")) && !strings.ContainsAny(arg, `"'=`) {
				out[i] = r.rewrite(arg)
			}
		}
		i++
	}
	return out, r.outside, r.responseFiles
}

// Strict outside-root failures must remain distinguishable from remote errors
// so REMOTE_LOCAL_FALLBACK cannot silently turn them into successful builds.
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
