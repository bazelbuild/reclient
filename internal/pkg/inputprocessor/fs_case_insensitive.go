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

//go:build windows || darwin

package inputprocessor

// Handle cache insensitive file-system.
// On such file-system, filename "foo" and "Foo" are considered as the same
// file, but remote-apis-sdks don't unity them. Thus, remote apis backend (RBE)
// would fail with ExitCode:45 code=Invalid Argument, desc=failed to populate
// working directory: failed to download inputs: already exists.
//
// To avoid such error, use the file name stored on the disk.
// b/171018900

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	log "github.com/golang/glog"
)

type dirEntCache struct {
	mu sync.Mutex
	m  map[string]*dirEnt
}

// process global cache.
var dirCache = dirEntCache{
	m: make(map[string]*dirEnt),
}

type dirEnt struct {
	dir     os.FileInfo
	entries []os.FileInfo
}

// get gets dirEnt of dir.
func (c *dirEntCache) get(dir string) *dirEnt {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = make(map[string]*dirEnt)
	}
	de := c.m[dir]
	if de == nil {
		de = &dirEnt{}
		c.m[dir] = de
	}
	de.update(dir)
	return de.clone()
}

// update updates dirEnt for dir.
// it checks dir is updated since last update, and if so, update entries.
func (de *dirEnt) update(dir string) {
	fi, err := os.Stat(dir)
	if err != nil {
		log.Errorf("stat %q: %v", dir, err)
		return
	}
	if de.dir != nil && os.SameFile(fi, de.dir) && fi.ModTime().Equal(de.dir.ModTime()) {
		// not changed. no need to update
		return
	}
	// dir is new, or has been changed?
	f, err := os.Open(dir)
	if err != nil {
		log.Errorf("open %q: %v", dir, err)
		return
	}
	defer f.Close()
	entries, err := f.Readdir(-1)
	if err != nil {
		log.Errorf("readdir %q: %v", dir, err)
	}
	de.dir = fi
	de.entries = entries
}

func (de *dirEnt) clone() *dirEnt {
	nde := &dirEnt{
		dir:     de.dir,
		entries: make([]os.FileInfo, len(de.entries)),
	}
	copy(nde.entries, de.entries)
	return nde
}

type pathNormalizer interface {
	normalize(execRoot, pathname string) (string, error)
}

type pathNormalizerNative struct {
	// m keeps normalized filename for a filename.
	// key is filename used in inputprocessor.
	// value is filename stored in the disk for the key's filename.
	// filename is relative to execRoot.
	m map[string]string

	// dirs keeps *dirEnt fo a directory.
	// key is directory name.
	// value is *dirEnt for the directory.
	dirs map[string]*dirEnt
}

func newPathNormalizer(cross bool) pathNormalizer {
	if cross {
		return pathNormalizerCross{
			dirs: make(map[string]string),
		}
	}
	return pathNormalizerNative{
		m:    make(map[string]string),
		dirs: make(map[string]*dirEnt),
	}
}

func (p pathNormalizerNative) normalize(execRoot, pathname string) (string, error) {
	segs := strings.Split(filepath.Clean(pathname), string(filepath.Separator))
	var nsegs []string
	var pathBuilder strings.Builder
loop:
	for _, seg := range segs {
		if seg == "" {
			// ignore empty seg. e.g. "//path/name".
			// http://b/170593203 http://b/171203933
			continue
		}
		dir := pathBuilder.String()
		if pathBuilder.Len() > 0 {
			pathBuilder.WriteByte(filepath.Separator)
		}
		fmt.Fprintf(&pathBuilder, seg)
		pathname := pathBuilder.String()
		s, ok := p.m[pathname]
		if ok {
			// actual name of pathname's base on disk is known to be `s`.
			nsegs = append(nsegs, s)
			continue loop
		}
		absDir := filepath.Join(execRoot, dir)
		de, ok := p.dirs[absDir]
		if !ok {
			// first visit to dir.
			de = dirCache.get(absDir)
			p.dirs[pathname] = de
			// populate actual name of pathname on disk in `p.m`.
			for _, ent := range de.entries {
				canonicalPathname := filepath.Join(dir, ent.Name())
				p.m[canonicalPathname] = ent.Name()
			}
		}
		// check again if we can find it in `p.m` by updating with `de`.
		s, ok = p.m[pathname]
		if ok {
			nsegs = append(nsegs, s)
			continue loop
		}
		// it is not the same name on the disk.
		fi, err := os.Stat(filepath.Join(execRoot, pathname))
		if err != nil {
			return "", fmt.Errorf("stat %q: %v", pathname, err)
		}
		// find the same file and use the name on the disk.
		for _, ent := range de.entries {
			if os.SameFile(fi, ent) {
				p.m[pathname] = ent.Name()
				nsegs = append(nsegs, ent.Name())
				continue loop
			}
		}
		// not found on the filesystem? use given name as is.
		nsegs = append(nsegs, seg)
	}
	return filepath.Join(nsegs...), nil
}

type pathNormalizerCross struct {
	dirs map[string]string // normalized -> dir name to use
}

func (p pathNormalizerCross) normalize(execRoot, pathname string) (string, error) {
	f := strings.TrimLeft(filepath.Clean(pathname), string(filepath.Separator))
	_, err := os.Stat(filepath.Join(execRoot, f))
	if err != nil {
		return f, err
	}
	dir, base := filepath.Split(f)
	// workaround for https://bugs.chromium.org/p/chromium/issues/detail?id=1207754
	key := strings.ToLower(dir)
	d, ok := p.dirs[key]
	if ok {
		dir = d
	} else {
		p.dirs[key] = dir
	}
	return filepath.Join(dir, base), err
}
