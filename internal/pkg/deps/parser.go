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

// Package deps provides functionality for parsing dependency files and generating .deps files used
// by RE Proxy for invalidating remote cache.
package deps

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"team/foundry-x/re-client/internal/pkg/logger"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/digest"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/filemetadata"

	log "github.com/golang/glog"
)

const (
	// notFoundMarker is the string indicating a dependency was not found.
	notFoundMarker = "NA"
)

var (
	dFileParser = regexp.MustCompile(`\s*([^\s]+?):?\s+\\?`)
	// depsFileParser matches file->digest maps in the deps file.
	// Example: foo.h:2f13aef4e4d0f83bc7d6a5d4e7ab5823107a53ff/140
	depsFileParser = regexp.MustCompile(`\s*(.+?):(.+?\/?.+)\s*`)
)

// Parser parses dependency files into manifests.
type Parser struct {
	// ExecRoot is the exec root under which all file paths are relative.
	ExecRoot string
	// DigestStore is a store for file digests as read from disk.
	DigestStore filemetadata.Cache
}

// WriteDepsFile creates a deps file from the provided d file and include
// directories. The path of the deps file is the same as the provided d file,
// with the .deps extension.
func (p *Parser) WriteDepsFile(dFilePath string, rec *logger.LogRecord) error {
	st := time.Now()
	defer func() {
		rec.RecordEventTime(logger.EventLERCWriteDeps, st)
	}()
	deps, err := p.GetDeps(dFilePath)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(p.ExecRoot, dFilePath+".deps"), []byte(deps), 0644)
}

// VerifyDepsFile compares an existing deps file in the expected <path>.d.deps
// path to the one that would be generated from the provided d file and include
// directories.
func (p *Parser) VerifyDepsFile(dFilePath string, rec *logger.LogRecord) (bool, error) {
	st := time.Now()
	defer func() {
		rec.RecordEventTime(logger.EventLERCVerifyDeps, st)
	}()
	buf, err := os.ReadFile(filepath.Join(p.ExecRoot, dFilePath+".deps"))
	if err != nil {
		return false, err
	}
	matches := depsFileParser.FindAllStringSubmatch(string(buf), -1)
	for _, match := range matches {
		if len(match) < 3 {
			return false, fmt.Errorf(".deps file has an invalid format in line %v", match[0])
		}
		md := p.DigestStore.Get(filepath.Join(p.ExecRoot, match[1]))
		if match[2] == notFoundMarker {
			if md.Err == nil {
				return false, nil
			}
			continue
		}
		dg, err := digest.NewFromString(match[2])
		if err != nil {
			return false, err
		}
		if md.Digest != dg {
			return false, nil
		}
	}
	return true, nil
}

// Similar to filepath.Rel except return an error when the path is not
// under the base directory.
func getRelPath(base, path string) (string, error) {
	relPath, err := filepath.Rel(base, path)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(relPath, "..") {
		return "", fmt.Errorf("%s is not under %s", relPath, base)
	}
	return relPath, nil
}

func (p *Parser) readDFileDeps(dFilePath string) ([]string, error) {
	buf, err := os.ReadFile(filepath.Join(p.ExecRoot, dFilePath))
	if err != nil {
		return nil, err
	}
	matches := dFileParser.FindAllStringSubmatch(string(buf), -1)
	if len(matches) < 2 {
		return nil, fmt.Errorf("%s d file has wrong format", dFilePath)
	}
	var deps []string
	for _, match := range matches[1:] {
		if len(match) < 2 {
			return nil, fmt.Errorf("%s .d file has an invalid format in line %v", match[0], dFilePath)
		}
		relPath := match[1]
		if filepath.IsAbs(match[1]) {
			if relPath, err = getRelPath(p.ExecRoot, match[1]); err != nil {
				log.Warningf("Failed to make path relative to exec root: %v", err)
				continue
			}
		}
		deps = append(deps, relPath)
	}
	return deps, nil
}

func (p *Parser) addDep(relPath string, deps map[string]bool) bool {
	md := p.DigestStore.Get(filepath.Join(p.ExecRoot, relPath))
	if md.Err == nil {
		deps[fmt.Sprintf("%s:%s\n", filepath.Clean(relPath), md.Digest)] = true
		return true
	}
	if e, ok := md.Err.(*filemetadata.FileError); ok && e.IsNotFound {
		deps[fmt.Sprintf("%s:%s\n", filepath.Clean(relPath), notFoundMarker)] = true
	}
	return false
}

// GetDeps returns the deps file content based on the d file and include directories.
// The deps are formatted as sorted unique "<path>:<digest>\n" lines.
// If <digest> is "NA", it means it failed to get digest of the file.
func (p *Parser) GetDeps(dFilePath string) (string, error) {
	dFileDeps, err := p.readDFileDeps(dFilePath)
	if err != nil {
		return "", err
	}
	deps := make(map[string]bool)
	for _, dep := range dFileDeps {
		p.addDep(dep, deps)
	}
	var ds []string
	for d := range deps {
		ds = append(ds, d)
	}
	sort.Strings(ds)
	return strings.Join(ds, ""), nil
}
