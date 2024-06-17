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

package depscache

import (
	"bufio"
	"encoding/hex"
	"os"
	"strings"
	"sync"
	"time"

	ppb "github.com/bazelbuild/reclient/api/proxy"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/cache"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/digest"
	"golang.org/x/sync/singleflight"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Similar to the cppfilecache, but instead of using the digest for the
// contents of the entire file, this cache only digests lines that start
// with #.  The idea being that changes to the file that don't impact
// dependencies won't be considered.

type fileInfo struct {
	*ppb.FileInfo
	updated bool
}

type minimalFileCache struct {
	minfc cache.SingleFlight
	files map[string]fileInfo
	mu    sync.RWMutex
	g     singleflight.Group
}

type minDigestInfo struct {
	digest digest.Digest
	mtime  time.Time
}

func (m *minimalFileCache) load(absPath string) (*ppb.FileInfo, error) {
	m.mu.RLock()
	fi, ok := m.files[absPath]
	m.mu.RUnlock()
	if ok {
		if fi.updated {
			return fi.FileInfo, nil
		}
	}
	v, err, _ := m.g.Do(absPath, func() (interface{}, error) {
		res, err := m.minfc.LoadOrStore(absPath, func() (interface{}, error) {
			return minimalDigest(absPath)
		})
		if err != nil {
			return nil, err
		}
		mdinfo := res.(*minDigestInfo)

		fi := fileInfo{
			FileInfo: &ppb.FileInfo{
				AbsPath: absPath,
				Digest:  mdinfo.digest.String(),
				Mtime:   timestamppb.New(mdinfo.mtime),
			},
			updated: true,
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		m.files[absPath] = fi
		return fi, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(fileInfo).FileInfo, nil
}

func (m *minimalFileCache) init(fInfos []*ppb.FileInfo) {
	for _, fi := range fInfos {
		m.files[fi.AbsPath] = fileInfo{FileInfo: fi}
	}
}

// minimalDigest computes a digest for just preprocessor directives of
// a file.
//
// One caveat, it will also digest lines that look like preprocessor
// directives that are included in /* */ comment blocks.  This will only
// cause possible cache misses, which doesn't affect overall correctness.
func minimalDigest(absPath string) (*minDigestInfo, error) {
	fstat, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Scan through the file, and only append lines to the main string that start with #
	lsc := bufio.NewScanner(f)
	h := digest.HashFn.New()
	sz := 0

	continuedLine := false
	for lsc.Scan() {
		// See https://cplusplus.com/doc/tutorial/preprocessor/ for a brief rundown on
		// preprocessor directives and how they are structured.
		txt := strings.TrimSpace(lsc.Text())
		if strings.HasPrefix(txt, "#") || continuedLine {
			continuedLine = false
			h.Write([]byte(txt))
			sz += len(txt)

			// If the line ends in a backslash the directive continues on the following
			// line.
			if strings.HasSuffix(txt, "\\") {
				continuedLine = true
			}
		}
	}
	if err := lsc.Err(); err != nil {
		return nil, err
	}

	arr := h.Sum(nil)
	mdinfo := &minDigestInfo{
		digest: digest.Digest{Hash: hex.EncodeToString(arr[:]), Size: int64(sz)},
		mtime:  fstat.ModTime(),
	}

	return mdinfo, nil
}
