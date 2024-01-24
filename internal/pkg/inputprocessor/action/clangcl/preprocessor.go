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

// Package clangcl performs include processing given a valid clangCl action.
package clangcl

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor"
	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/action/cppcompile"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/cache"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"

	log "github.com/golang/glog"
)

type resourceDirInfo struct {
	fileInfo    os.FileInfo
	resourceDir string
}

type version struct {
	major, minor, micro, build int
	text                       string
}

var (
	resourceDirsMu    sync.Mutex
	resourceDirs      = map[string]resourceDirInfo{}
	toAbsArgs         = map[string]bool{}
	virtualInputFlags = map[string]bool{"-I": true, "/I": true, "-imsvc": true, "-winsysroot": true}
	versionRegex      = regexp.MustCompile(`^([0-9]+)(\.([0-9]+)(\.([0-9]+)(\.([0-9]+))?)?)?$`)
)

// Preprocessor is the preprocessor of clang-cl compile actions.
type Preprocessor struct {
	*cppcompile.Preprocessor
	winSDKCache      cache.SingleFlight
	vcToolchainCache cache.SingleFlight
}

// ParseFlags parses the commands flags and populates the ActionSpec object with inferred
// information.
func (p *Preprocessor) ParseFlags() error {
	f, err := parseFlags(p.Ctx, p.Options.Cmd, p.Options.WorkingDir, p.Options.ExecRoot)
	if err != nil {
		p.Err = fmt.Errorf("flag parsing failed. %v", err)
		return p.Err
	}
	p.Flags = f
	p.FlagsToActionSpec()
	return nil
}

// ComputeSpec computes cpp header dependencies.
func (p *Preprocessor) ComputeSpec() error {
	s := &inputprocessor.ActionSpec{InputSpec: &command.InputSpec{}}
	defer p.AppendSpec(s)

	args := p.BuildCommandLine("/Fo", true, toAbsArgs)
	if p.CPPDepScanner.Capabilities().GetExpectsResourceDir() {
		args = p.addResourceDir(args)
	}
	headerInputFiles, err := p.FindDependencies(args)
	if err != nil {
		s.UsedShallowMode = true
		return err
	}

	s.InputSpec = &command.InputSpec{
		Inputs:        headerInputFiles,
		VirtualInputs: cppcompile.VirtualInputs(p.Flags, p),
	}
	return nil
}

// IsVirtualInput returns true if the flag specifies a virtual input to be added to InputSpec.
func (p *Preprocessor) IsVirtualInput(flag string) bool {
	return virtualInputFlags[flag]
}

// AppendVirtualInput appends a virtual input to res. If the flag="-winsysroot" the content of
// the path is processed and Win SDK and VC toolchain paths are added to virtual inputs.
func (p *Preprocessor) AppendVirtualInput(res []*command.VirtualInput, flag, path string) []*command.VirtualInput {
	if flag == "-winsysroot" {
		// clang-cl tries to extract win SDK version and VC toolchain version from within
		// the winsysroot path. Those are used for setting -internal-isystem flag values.
		// We need to reproduce clang's path traversing logic and upload the required paths
		// so clang-cl on a remote worker generates the same -internal-isystem paths as it would
		// locally.
		winsysroot := path
		absWinsysroot := filepath.Join(p.Flags.ExecRoot, path)
		cacheEntry, _ := p.winSDKCache.LoadOrStore(winsysroot, func() (interface{}, error) {
			absDir, err := winSDKDir(absWinsysroot)
			if err != nil {
				// If failed to get Win SDK dir, return winsysroot instead (don't return the error)
				// This will prevent the logic to be re-executed for each action as
				// errors will likely stem from unexpected directories structure on FS.
				log.Warningf("Failed to get Win SDK path for %q. %v", winsysroot, err)
				return winsysroot, nil
			}
			dir, err := filepath.Rel(p.Flags.ExecRoot, absDir)
			if err != nil {
				log.Warningf("Failed to make %q relative to %q. %v", absDir, p.Flags.ExecRoot, err)
				return winsysroot, nil
			}
			return dir, nil
		})
		computedPath := cacheEntry.(string)
		if computedPath != winsysroot {
			res = p.Preprocessor.AppendVirtualInput(res, flag, computedPath)
		}
		cacheEntry, _ = p.vcToolchainCache.LoadOrStore(winsysroot, func() (interface{}, error) {
			absDir, err := vcToolchainDir(absWinsysroot)
			if err != nil {
				log.Warningf("Failed to get VC toolchain path for %q. %v", winsysroot, err)
				return winsysroot, nil
			}
			dir, err := filepath.Rel(p.Flags.ExecRoot, absDir)
			if err != nil {
				log.Warningf("Failed to make %q relative to %q. %v", absDir, p.Flags.ExecRoot, err)
				return winsysroot, nil
			}
			return dir, nil
		})
		computedPath = cacheEntry.(string)
		if computedPath != winsysroot {
			res = p.Preprocessor.AppendVirtualInput(res, flag, computedPath)
		}
		return res
	}
	return p.Preprocessor.AppendVirtualInput(res, flag, path)
}

func (p *Preprocessor) addResourceDir(args []string) []string {
	for _, arg := range args {
		if arg == "-resource-dir" || strings.HasPrefix(arg, "-resource-dir=") {
			return args
		}
	}
	resourceDir := p.resourceDir(args)
	if resourceDir != "" {
		return append(args, "-resource-dir", resourceDir)
	}
	return args
}

func (p *Preprocessor) resourceDir(args []string) string {
	return p.ResourceDir(args, "--version", func(stdout string) (string, error) {
		if !strings.HasPrefix(stdout, "clang version ") {
			return "", fmt.Errorf("unexpected version string of %s: %q", args[0], stdout)
		}
		version := strings.TrimPrefix(stdout, "clang version ")
		i := strings.IndexByte(version, ' ')
		if i < 0 {
			return "", fmt.Errorf("unexpected version string of %s: %q", args[0], stdout)
		}
		version = version[:i]
		resourceDir := filepath.Join(filepath.Dir(args[0]), "..", "lib", "clang", version)
		log.Infof("%s --version => %q => %q => resource-dir:%q", args[0], stdout, version, resourceDir)
		return resourceDir, nil
	})
}

func winSDKDir(winsysroot string) (string, error) {
	// Reproduces the behavior from
	// https://github.com/llvm/llvm-project/blob/main/llvm/lib/WindowsDriver/MSVCPaths.cpp#L108
	path, err := getMaxVersionDir(filepath.Join(winsysroot, "Windows Kits"))
	if err != nil {
		return "", err
	}
	if path, err = getMaxVersionDir(filepath.Join(path, "Include")); err != nil {
		return "", err
	}
	return path, nil
}

func vcToolchainDir(winsysroot string) (string, error) {
	path, err := getMaxVersionDir(filepath.Join(winsysroot, "VC", "Tools", "MSVC"))
	if err != nil {
		return "", err
	}
	return path, nil
}

func getMaxVersionDir(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	maxVersion, err := getMaxVersionFromPath(path)
	if err != nil {
		return "", fmt.Errorf("No version dir under %q", path)
	}
	return filepath.Join(path, maxVersion), nil
}

func getMaxVersionFromPath(path string) (string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", err
	}
	maxVersion := version{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if tuple, err := newVersion(entry.Name()); err == nil && tuple.gt(maxVersion) {
			maxVersion = tuple
		}
	}
	if maxVersion.isEmpty() {
		return "", fmt.Errorf("No version dir under %q", path)
	}
	return maxVersion.text, nil
}

func newVersion(text string) (version, error) {
	match := versionRegex.FindStringSubmatch(text)
	if len(match) == 0 {
		return version{}, fmt.Errorf("%q is not a valid version string", text)
	}
	tuple := version{text: text}
	tuple.major, _ = strconv.Atoi(match[1])
	tuple.minor, _ = strconv.Atoi(match[3])
	tuple.micro, _ = strconv.Atoi(match[5])
	tuple.build, _ = strconv.Atoi(match[7])
	return tuple, nil
}

func (a *version) gt(b version) bool {
	if a.major != b.major {
		return a.major > b.major
	}
	if a.minor != b.minor {
		return a.minor > b.minor
	}
	if a.micro != b.micro {
		return a.micro > b.micro
	}
	return a.build > b.build
}

func (a *version) isEmpty() bool {
	return len(a.text) == 0
}
