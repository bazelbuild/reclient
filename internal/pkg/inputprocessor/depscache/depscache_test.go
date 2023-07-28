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
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bazelbuild/reclient/internal/pkg/execroot"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/filemetadata"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestSetGet(t *testing.T) {
	er, cleanup := execroot.Setup(t, nil)
	defer cleanup()
	files := map[string][]byte{
		filepath.Join(er, "foo"): []byte("foo"),
		filepath.Join(er, "bar"): []byte("bar"),
	}
	execroot.AddFilesWithContent(t, "", files)
	dc := New(filemetadata.NewSingleFlightCache())
	dc.LoadFromDir(er)
	k := Key{
		CommandDigest: "hash/123",
		SrcFilePath:   filepath.Join(er, "foo"),
	}
	wantDeps := []string{filepath.Join(er, "bar")}
	err := dc.SetDeps(k, wantDeps)
	if err != nil {
		t.Errorf("SetDeps() returned error: %v", err)
	}
	gotDeps, ok := dc.GetDeps(k)
	if !ok {
		t.Errorf("GetDeps() failed")
	}
	if diff := cmp.Diff(wantDeps, gotDeps); diff != "" {
		t.Errorf("GetDeps() returned diff, (-want +got): %s", diff)
	}
}

func TestSetGetBeforeLoaded(t *testing.T) {
	er, cleanup := execroot.Setup(t, nil)
	defer cleanup()
	files := map[string][]byte{
		filepath.Join(er, "foo"): []byte("foo"),
		filepath.Join(er, "bar"): []byte("bar"),
	}
	execroot.AddFilesWithContent(t, "", files)
	dc := New(filemetadata.NewSingleFlightCache())
	k := Key{
		CommandDigest: "hash/123",
		SrcFilePath:   filepath.Join(er, "foo"),
	}
	wantDeps := []string{filepath.Join(er, "bar")}
	err := dc.SetDeps(k, wantDeps)
	if err != nil {
		t.Errorf("SetDeps() returned error: %v", err)
	}
	gotDeps, ok := dc.GetDeps(k)
	if ok {
		t.Errorf("GetDeps() returned (%v, %v), wanted (nil, false)", gotDeps, ok)
	}
}
func TestWriteLoad(t *testing.T) {
	er, cleanup := execroot.Setup(t, nil)
	defer cleanup()
	fooPath := filepath.Join(er, "foo")
	barPath := filepath.Join(er, "bar")
	bazPath := filepath.Join(er, "baz")
	quxPath := filepath.Join(er, "qux")
	files := map[string][]byte{
		// Below files will not change between instances of deps cache.
		fooPath: []byte("foo"),
		barPath: []byte("bar"),
		// Below file will change between instance of deps cache in a way
		// that affects dependencies (minimized header changes).
		bazPath: []byte("baz"),
		// Below file will change between instances of deps cache but not
		// affect dependencies (minimized header doesn't change).
		quxPath: []byte("qux"),
	}
	execroot.AddFilesWithContent(t, "", files)
	dc := New(filemetadata.NewSingleFlightCache())
	dc.LoadFromDir(er)
	k1 := Key{
		CommandDigest: "hash/123",
		SrcFilePath:   fooPath,
	}
	err := dc.SetDeps(k1, []string{barPath})
	if err != nil {
		t.Errorf("SetDeps(k1) returned error: %v", err)
	}
	k2 := Key{
		CommandDigest: "hash2/123",
		SrcFilePath:   fooPath,
	}
	err = dc.SetDeps(k2, []string{bazPath})
	if err != nil {
		t.Errorf("SetDeps(k2) returned error: %v", err)
	}
	k3 := Key{
		CommandDigest: "hash3/123",
		SrcFilePath:   fooPath,
	}
	err = dc.SetDeps(k3, []string{quxPath})
	if err != nil {
		t.Errorf("SetDeps(k3) returned error: %v", err)
	}
	dc.WriteToDisk(er)
	time.Sleep(1 * time.Second)
	execroot.AddFileWithContent(t, bazPath, []byte("baz2"))
	execroot.AddFileWithContent(t, quxPath, []byte("qux"))
	// Update mtime to sometime in the past to check whether we erroneously rely on stale
	// mtime of modified files.
	if err := os.Chtimes(bazPath, time.Now().Add(-1*time.Hour), time.Now().Add(-1*time.Hour)); err != nil {
		t.Errorf("Failed to set mtime of file %v: %v", bazPath, err)
	}
	time.Sleep(1 * time.Second)
	filemetadata.ResetGlobalCache()
	dc = New(filemetadata.NewSingleFlightCache())
	dc.LoadFromDir(er)
	deps, ok := dc.GetDeps(k1)
	if !ok {
		t.Errorf("GetDeps() failed")
	}
	if diff := cmp.Diff([]string{barPath}, deps); diff != "" {
		t.Errorf("GetDeps(k1) returned diff, (-want +got): %s", diff)
	}
	deps, ok = dc.GetDeps(k2)
	if ok || deps != nil {
		t.Errorf("GetDeps(k2) returned %v, %v, want nil, false", deps, ok)
	}
	deps, ok = dc.GetDeps(k3)
	if !ok {
		t.Errorf("GetDeps() failed")
	}
	if diff := cmp.Diff([]string{quxPath}, deps); diff != "" {
		t.Errorf("GetDeps(k3) returned diff, (-want +got): %s", diff)
	}
}

func TestWriteLoadKeysDependingOnSameFile(t *testing.T) {
	er, cleanup := execroot.Setup(t, nil)
	defer cleanup()
	fooPath := filepath.Join(er, "foo")
	barPath := filepath.Join(er, "bar")
	files := map[string][]byte{
		fooPath: []byte("foo"),
		barPath: []byte("bar"),
	}
	execroot.AddFilesWithContent(t, "", files)
	dc := New(filemetadata.NewSingleFlightCache())
	dc.LoadFromDir(er)
	k1 := Key{
		CommandDigest: "hash/123",
		SrcFilePath:   fooPath,
	}
	err := dc.SetDeps(k1, []string{barPath})
	if err != nil {
		t.Errorf("SetDeps(k1) returned error: %v", err)
	}
	k2 := Key{
		CommandDigest: "hash2/123",
		SrcFilePath:   fooPath,
	}
	err = dc.SetDeps(k2, []string{barPath})
	if err != nil {
		t.Errorf("SetDeps(k2) returned error: %v", err)
	}
	dc.WriteToDisk(er)
	time.Sleep(1 * time.Second)
	execroot.AddFileWithContent(t, barPath, []byte("bar2"))
	time.Sleep(1 * time.Second)
	filemetadata.ResetGlobalCache()
	dc = New(filemetadata.NewSingleFlightCache())
	dc.LoadFromDir(er)
	// bar has changed, we should not get a cache hit.
	deps, ok := dc.GetDeps(k1)
	if ok || deps != nil {
		t.Errorf("GetDeps(k1) returned %v, %v, want nil, false", deps, ok)
	}
	// Update the deps of k1 to refer to the new bar. This should not interfere
	// with the cache entry for k2.
	err = dc.SetDeps(k1, []string{barPath})
	if err != nil {
		t.Errorf("SetDeps(k1) returned error: %v", err)
	}
	dc.WriteToDisk(er)
	time.Sleep(1 * time.Second)
	filemetadata.ResetGlobalCache()
	dc = New(filemetadata.NewSingleFlightCache())
	dc.LoadFromDir(er)
	deps, ok = dc.GetDeps(k2)
	// k2 should still get a cache miss because it refers to the old version of bar.
	if ok || deps != nil {
		t.Errorf("GetDeps(k2) returned %v, %v, want nil, false", deps, ok)
	}
}

func TestEviction(t *testing.T) {
	er, cleanup := execroot.Setup(t, nil)
	defer cleanup()
	fooPath := filepath.Join(er, "foo")
	barPath := filepath.Join(er, "bar")
	bazPath := filepath.Join(er, "baz")
	quxPath := filepath.Join(er, "qux")
	files := map[string][]byte{
		fooPath: []byte("foo"),
		barPath: []byte("bar"),
		bazPath: []byte("baz"),
		quxPath: []byte("qux"),
	}
	execroot.AddFilesWithContent(t, "", files)
	dc := New(filemetadata.NewSingleFlightCache())
	dc.LoadFromDir(er)
	k1 := Key{
		CommandDigest: "hash/123",
		SrcFilePath:   fooPath,
	}
	err := dc.SetDeps(k1, []string{barPath})
	if err != nil {
		t.Errorf("SetDeps(k1) returned error: %v", err)
	}
	k2 := Key{
		CommandDigest: "hash2/123",
		SrcFilePath:   fooPath,
	}
	err = dc.SetDeps(k2, []string{bazPath})
	if err != nil {
		t.Errorf("SetDeps(k2) returned error: %v", err)
	}
	dc.lutByKey = map[Key]time.Time{
		privateKey(dc, k1): time.Date(2020, 01, 01, 0, 0, 0, 0, time.UTC),
		privateKey(dc, k2): time.Date(2020, 01, 02, 0, 0, 0, 0, time.UTC),
	}
	dc.WriteToDisk(er)
	filemetadata.ResetGlobalCache()
	dc = New(filemetadata.NewSingleFlightCache())
	dc.MaxEntries = 2
	dc.LoadFromDir(er)
	wg := sync.WaitGroup{}
	wg.Add(2)
	dc.testOnlySetLut = wg.Done
	deps, ok := dc.GetDeps(k1)
	if !ok {
		t.Errorf("GetDeps() failed")
	}
	if diff := cmp.Diff([]string{barPath}, deps); diff != "" {
		t.Errorf("GetDeps(k1) returned diff, (-want +got): %s", diff)
	}
	k3 := Key{
		CommandDigest: "hash3/123",
		SrcFilePath:   fooPath,
	}
	err = dc.SetDeps(k3, []string{quxPath})
	if err != nil {
		t.Errorf("SetDeps(k3) returned error: %v", err)
	}
	wg.Wait()
	dc.WriteToDisk(er)
	dc = New(filemetadata.NewSingleFlightCache())
	dc.LoadFromDir(er)
	wantKeys := []Key{privateKey(dc, k1), privateKey(dc, k3)}
	gotKeys := make([]Key, 0)
	for k := range dc.depsCache {
		gotKeys = append(gotKeys, k)
	}
	if diff := cmp.Diff(wantKeys, gotKeys, cmpopts.IgnoreUnexported(Key{}), cmpopts.SortSlices(func(x, y interface{}) bool {
		return x.(Key).CommandDigest < y.(Key).CommandDigest
	})); diff != "" {
		t.Errorf("Keys in cache has diff, (-want +got): %s", diff)
	}
}

func privateKey(c *Cache, k Key) Key {
	sfi, err := c.filesCache.load(k.SrcFilePath)
	if err != nil {
		return Key{}
	}
	return Key{
		CommandDigest:    k.CommandDigest,
		SrcFilePath:      k.SrcFilePath,
		srcFileMinDigest: sfi.Digest,
	}
}
