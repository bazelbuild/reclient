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
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	ppb "github.com/bazelbuild/reclient/api/proxy"
	"github.com/bazelbuild/reclient/internal/pkg/logger"
	"github.com/bazelbuild/reclient/internal/pkg/logger/event"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/filemetadata"
	log "github.com/golang/glog"
	"google.golang.org/protobuf/proto"

	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	cacheName = "reproxy.cache"
)

// Key is the key identifying a command and source file to retrieve its cached
// dependencies.
type Key struct {
	CommandDigest    string
	SrcFilePath      string
	srcFileMinDigest string
}

// Cache is the deps cache.
type Cache struct {
	MaxEntries int
	Logger     *logger.Logger
	depsCache  map[Key][]*ppb.FileInfo
	depsMu     sync.RWMutex
	filesCache cppFileCache
	// last use time of a key
	lutByKey map[Key]time.Time
	lutMu    sync.Mutex

	ready          int32
	testOnlySetLut func()
}

// New creates a new empty deps cache.
func New(fmc filemetadata.Cache) *Cache {
	return &Cache{
		MaxEntries: 100000,
		depsCache:  make(map[Key][]*ppb.FileInfo),
		lutByKey:   make(map[Key]time.Time),
		filesCache: cppFileCache{
			fmc:   fmc,
			files: make(map[string]fileInfo),
		},
	}
}

// LoadFromDir loads the cache from a directory. Recommended to be called in a goroutine as runtime
// could be long depending on the size of the cache.
func (c *Cache) LoadFromDir(dir string) {
	path := filepath.Join(dir, cacheName)
	log.Infof("Loading cache from %v", path)
	st := time.Now()
	defer func() {
		atomic.StoreInt32(&c.ready, 1)
		if c.Logger != nil {
			c.Logger.AddMetricIntToProxyInfo(event.DepsCacheLoadCount, int64(len(c.depsCache)))
		}
	}()
	in, err := os.ReadFile(path)
	if err != nil {
		log.Warningf("Cache file does not exist, will create one")
		return
	}
	db := &ppb.DepsDatabase{}
	if err := proto.Unmarshal(in, db); err != nil {
		log.Errorf("Failed to parse cache file: %v", err)
		return
	}
	c.filesCache.init(db.GetFiles())
	numEntries := 0
	for _, entry := range db.GetDeps() {
		k := Key{
			CommandDigest:    entry.GetCommandDigest(),
			SrcFilePath:      entry.GetSrcFile().GetAbsPath(),
			srcFileMinDigest: entry.GetSrcFile().GetDigest(),
		}
		deps := make([]*ppb.FileInfo, 0)
		valid := true
		for _, d := range entry.GetDepIds() {
			if d >= int64(len(db.Files)) {
				log.Warningf("Could not find file with ID %v in database %v", d, path)
				valid = false
				break
			}
			deps = append(deps, db.Files[d])
		}
		if !valid {
			continue
		}
		c.depsCache[k] = deps
		c.lutByKey[k] = entry.LastUsed.AsTime()
		numEntries++
	}
	log.Infof("Deps cache loaded %v entries", numEntries)
	if c.Logger != nil {
		c.Logger.AddEventTimeToProxyInfo(event.DepsCacheLoad, st, time.Now())
	}
}

// IsReady returns whether the cache is ready to be used. The cache will not
// be ready if its performing a long running operation, such as loading from
// file.
func (c *Cache) IsReady() bool {
	if c == nil {
		return false
	}
	return atomic.LoadInt32(&c.ready) == 1
}

// GetDeps returns deps if exist in cache.
func (c *Cache) GetDeps(k Key) ([]string, bool) {
	if c == nil {
		return nil, false
	}
	if !c.IsReady() {
		return nil, false
	}
	c.depsMu.RLock()
	defer c.depsMu.RUnlock()
	sfi, err := c.filesCache.load(k.SrcFilePath)
	if err != nil {
		log.Warningf("Failed to load %v from files cache: %v", k.SrcFilePath, err)
		return nil, false
	}
	fullKey := Key{
		CommandDigest:    k.CommandDigest,
		SrcFilePath:      k.SrcFilePath,
		srcFileMinDigest: sfi.Digest,
	}
	fInfos, ok := c.depsCache[fullKey]
	if !ok {
		return nil, false
	}
	deps := []string{}
	// We do not cache the below loop as it is not expected to verify the same key twice in the
	// same reproxy run.
	for _, d := range fInfos {
		fi, err := c.filesCache.load(d.AbsPath)
		if err != nil {
			log.Warningf("Failed to load %v from files cache: %v", d.AbsPath, err)
			return nil, false
		}
		if fi.Digest != d.Digest {
			return nil, false
		}
		deps = append(deps, d.AbsPath)
	}
	go func() {
		c.lutMu.Lock()
		c.lutByKey[fullKey] = time.Now()
		c.lutMu.Unlock()
		if c.testOnlySetLut != nil {
			c.testOnlySetLut()
		}
	}()
	return deps, true
}

// SetDeps sets the dependencies of an action defined by the given key.
func (c *Cache) SetDeps(k Key, deps []string) error {
	if c == nil {
		return nil
	}
	if !c.IsReady() {
		return nil
	}
	fInfos := []*ppb.FileInfo{}
	for _, d := range deps {
		if !filepath.IsAbs(d) {
			// Return error here because this indicates there is a problem
			// with the callsite of SetDeps.
			return fmt.Errorf("%v is not an absolute path", d)
		}
		fi, err := c.filesCache.load(d)
		if err != nil {
			log.Warningf("Failed to load %v from files cache: %v", d, err)
			return nil
		}
		fInfos = append(fInfos, fi)
	}
	fi, err := c.filesCache.load(k.SrcFilePath)
	if err != nil {
		log.Warningf("Failed to load %v from files cache: %v", k.SrcFilePath, err)
		return nil
	}
	fullKey := Key{
		CommandDigest:    k.CommandDigest,
		SrcFilePath:      k.SrcFilePath,
		srcFileMinDigest: fi.Digest,
	}
	c.depsMu.Lock()
	c.depsCache[fullKey] = fInfos
	c.depsMu.Unlock()
	c.lutMu.Lock()
	c.lutByKey[fullKey] = time.Now()
	c.lutMu.Unlock()
	if c.testOnlySetLut != nil {
		c.testOnlySetLut()
	}
	return nil
}

// WriteToDisk writes the cache to disk in proto file format.
func (c *Cache) WriteToDisk(outDir string) {
	if c == nil {
		return
	}
	st := time.Now()
	atomic.StoreInt32(&c.ready, 0)
	c.depsMu.Lock()
	defer c.depsMu.Unlock()
	db := &ppb.DepsDatabase{
		Files: make([]*ppb.FileInfo, 0),
	}
	keys := make([]Key, 0, len(c.depsCache))
	for k := range c.depsCache {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		iut := c.lutByKey[keys[i]]
		jut := c.lutByKey[keys[j]]
		return !iut.Before(jut)
	})
	count := 0
	type fiKey struct {
		absPath string
		digest  string
	}
	fiKeyToIdx := make(map[fiKey]int64)
	for _, key := range keys {
		depIds := []int64{}
		fInfos := c.depsCache[key]
		for _, fi := range fInfos {
			idx, ok := fiKeyToIdx[fiKey{fi.AbsPath, fi.Digest}]
			if !ok {
				idx = int64(len(fiKeyToIdx))
				fiKeyToIdx[fiKey{fi.AbsPath, fi.Digest}] = idx
				db.Files = append(db.Files, fi)
			}
			depIds = append(depIds, idx)
		}
		c.lutMu.Lock()
		lut := c.lutByKey[key]
		c.lutMu.Unlock()
		db.Deps = append(db.Deps, &ppb.Deps{
			CommandDigest: key.CommandDigest,
			SrcFile: &ppb.FileInfo{
				AbsPath: key.SrcFilePath,
				Digest:  key.srcFileMinDigest,
			},
			DepIds:   depIds,
			LastUsed: timestamppb.New(lut),
		})
		count++
		if count >= c.MaxEntries {
			break
		}
	}

	out, err := proto.Marshal(db)
	if err != nil {
		log.Errorf("Failed to marshal the deps cache: %v", err)
		return
	}
	file := filepath.Join(outDir, cacheName)
	if err := os.WriteFile(file, []byte(out), 0644); err != nil {
		log.Errorf("Failed to write cache database to file %v: %v", err, file)
	} else {
		log.Infof("Wrote deps cache to %v", file)
	}
	if c.Logger != nil {
		c.Logger.AddEventTimeToProxyInfo(event.DepsCacheWrite, st, time.Now())
		c.Logger.AddMetricIntToProxyInfo(event.DepsCacheWriteCount, int64(count))
	}
}
