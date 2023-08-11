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

package toolchain

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"team/foundry-x/re-client/internal/pkg/pathtranslator"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/cache"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/filemetadata"
	log "github.com/golang/glog"
)

var (
	toolchainFilesCache cache.SingleFlight
)

const (
	remoteToolchainInputs = "remote_toolchain_inputs"
)

func doesExist(fmc filemetadata.Cache, fp string) (bool, error) {
	if fmc != nil {
		md := fmc.Get(fp)
		if md.Err == nil {
			return true, nil
		}
		if e, ok := md.Err.(*filemetadata.FileError); ok && e.IsNotFound {
			return false, nil
		}
		return false, md.Err
	}
	_, err := os.Stat(fp)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func loadOrStoreToolchainCache(rp string, baseDir string, execRoot string, fmc filemetadata.Cache) (interface{}, error) {
	rf, err := os.Open(rp)
	if err != nil {
		return nil, nil
	}
	defer rf.Close()

	scanner := bufio.NewScanner(rf)
	fileList := []string{}
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimLeft(line, " \t")
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		fp := filepath.Join(baseDir, line)
		exists, err := doesExist(fmc, fp)
		if err != nil {
			log.Warningf("Error while checking whether %q exists. err: %v", err)
		}
		if exists {
			fileList = append(fileList, pathtranslator.RelToExecRoot(execRoot, "", fp))
		} else {
			log.V(2).Infof("File %q defined in %q does not exist", fp, rp)
		}
	}
	return fileList, nil
}

// processToolchainInputFiles uses the "<toolchain>_remote_toolchain_inputs" file that is
// checked-in alongside the executable used to run the command, to determine the list
// of files that constitute the toolchain inputs. If that file doesn't exist, it looks
// for "remote_toolchain_inputs". Contents of either file should be clean paths. Paths
// in the toolchains list should be relative to the exec root.
func (p *InputProcessor) processToolchainInputFiles(execRoot string, toolchains []string, fmc filemetadata.Cache) (*command.InputSpec, error) {
	toolchainInputFiles := []string{}
	seenFiles := make(map[string]bool)
	for _, tc := range toolchains {
		if tc == "" {
			continue
		}
		tcRel := pathtranslator.RelToExecRoot(execRoot, "", tc)
		toolchainInputFiles = append(toolchainInputFiles, tcRel)

		baseDir := filepath.Join(execRoot, filepath.Dir(tcRel))
		rp := filepath.Join(baseDir, filepath.Base(tc)+"_"+remoteToolchainInputs)
		if _, ok := seenFiles[rp]; ok {
			continue
		}

		cache, err := toolchainFilesCache.LoadOrStore(rp, func() (interface{}, error) {
			return loadOrStoreToolchainCache(rp, baseDir, execRoot, fmc)
		})
		if err == nil && cache == nil {
			rp = filepath.Join(baseDir, remoteToolchainInputs)
			if _, ok := seenFiles[rp]; !ok {
				cache, err = toolchainFilesCache.LoadOrStore(rp, func() (interface{}, error) {
					return loadOrStoreToolchainCache(rp, baseDir, execRoot, fmc)
				})
			}
		}
		seenFiles[rp] = true

		if err != nil {
			log.Warningf("Can't find toolchain inputs file %v for %v, this may be expected: %v", rp, tc, err)
		}
		if cache != nil {
			toolchainInputFiles = append(toolchainInputFiles, cache.([]string)...)
		}
	}
	return &command.InputSpec{Inputs: toolchainInputFiles}, nil
}
