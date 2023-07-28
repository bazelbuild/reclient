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
	"sync"

	ppb "github.com/bazelbuild/reclient/api/proxy"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/filemetadata"
	"golang.org/x/sync/singleflight"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fileInfo struct {
	*ppb.FileInfo
	updated bool
}

type cppFileCache struct {
	fmc   filemetadata.Cache
	files map[string]fileInfo
	mu    sync.RWMutex
	g     singleflight.Group
}

func (m *cppFileCache) load(absPath string) (*ppb.FileInfo, error) {
	m.mu.RLock()
	fi, ok := m.files[absPath]
	m.mu.RUnlock()
	if ok {
		if fi.updated {
			return fi.FileInfo, nil
		}
	}
	v, err, _ := m.g.Do(absPath, func() (interface{}, error) {
		md := m.fmc.Get(absPath)
		fi := fileInfo{
			FileInfo: &ppb.FileInfo{
				AbsPath: absPath,
				Digest:  md.Digest.String(),
				Mtime:   timestamppb.New(md.MTime),
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

func (m *cppFileCache) init(fInfos []*ppb.FileInfo) {
	for _, fi := range fInfos {
		m.files[fi.AbsPath] = fileInfo{FileInfo: fi}
	}
}
