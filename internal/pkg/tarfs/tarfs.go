// Copyright 2024 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package tarfs is used to extract a tar file in memory and present it as a
// minimalistic read-only filesystem.
package tarfs

import (
	"archive/tar"
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	log "github.com/golang/glog"
)

// TarFS is a minimal in-memory filesystem constructed from a tar
// archive. It is inefficient due to its copying of data for each
// open file request.
type TarFS struct {
	fs.FS

	tarContents map[string]*FileInTar
}

// Open creates a new open file.
func (t *TarFS) Open(name string) (fs.File, error) {
	if runtime.GOOS == "windows" {
		name = strings.ReplaceAll(name, "/", "\\")
	}
	log.V(3).Infof("TarFS.Open: %s", name)
	if f, ok := t.tarContents[name]; ok {
		return f.NewOpenFile(), nil
	}
	log.V(3).Infof("TarFS.Open: failed to find %v, tar=%v", name, t.tarContents)
	return nil, fs.ErrNotExist
}

// FileInTar represents a combination of fs.File and fs.FileInfo
// interfaces.
type FileInTar struct {
	fs.File
	fs.FileInfo

	// File / Diretory related properties.
	path  string
	data  []byte
	mode  os.FileMode
	mtime time.Time
	isDir bool

	curReadOffset int
}

// NewOpenFile creates a copy of the current file to represent a
// newly opened file.
func (f *FileInTar) NewOpenFile() *FileInTar {
	return &FileInTar{
		path:  f.path,
		data:  f.data,
		mode:  f.mode,
		mtime: f.mtime,
		isDir: f.isDir,
	}
}

// Stat is a fs.File function. Returns the object itself
// since it also implements the FileInfo interface.
func (f *FileInTar) Stat() (fs.FileInfo, error) {
	return f, nil
}

// Read is a fs.File function to read the file contents.
func (f *FileInTar) Read(p []byte) (int, error) {
	if f.curReadOffset >= len(f.data) {
		return 0, io.EOF
	}
	sizeToRead := len(p)
	endIdx := min(len(f.data), f.curReadOffset+sizeToRead)
	sizeRead := copy(p, f.data[f.curReadOffset:endIdx])
	f.curReadOffset += sizeRead
	return sizeRead, nil
}

// Close is a fs.File function.
func (f *FileInTar) Close() error {
	f.curReadOffset = 0
	f.data = nil
	return nil
}

// Name is a fs.FileInfo function. Returns the name of the file.
func (f *FileInTar) Name() string {
	return filepath.Base(f.path)
}

// Size is a fs.FileInfo function. Returns the size of the data in
// memory.
func (f *FileInTar) Size() int64 {
	return int64(len(f.data))
}

// Mode returns the filemode.
func (f *FileInTar) Mode() fs.FileMode {
	return f.mode
}

// ModTime returns the last modified time of the file which corresponds
// to when the file was extracted from the tar archive.
func (f *FileInTar) ModTime() time.Time {
	return f.mtime
}

// IsDir returns true if the current file object represents a directory.
func (f *FileInTar) IsDir() bool {
	return f.isDir
}

// Sys is intended to return the underlying datasource, but it is
// currently unimplemented.
func (f *FileInTar) Sys() interface{} {
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// NewTarFSFromBytes creates a new tar filesystem from the given byte data
// representing a tar archive.
func NewTarFSFromBytes(tarData []byte) (*TarFS, error) {
	tarReader := tar.NewReader(bytes.NewReader(tarData))
	tarContents := make(map[string]*FileInTar)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("could not read tar file: %v", err)
		}

		path := filepath.Clean(header.Name)
		switch header.Typeflag {
		case tar.TypeDir:
			tarContents[path] = &FileInTar{
				path:  filepath.Base(path),
				mode:  os.FileMode(header.Mode),
				mtime: time.Now(),
				isDir: true,
			}
			log.V(2).Infof("NewTarFSFromBytes: adding dir %s", path)

		case tar.TypeReg:
			var b bytes.Buffer
			writer := bufio.NewWriter(&b)
			if _, err := io.Copy(writer, tarReader); err != nil {
				return nil, fmt.Errorf("could not copy file data: %v", err)
			}
			tarContents[path] = &FileInTar{
				path:  filepath.Base(path),
				data:  b.Bytes(),
				mode:  os.FileMode(header.Mode),
				mtime: time.Now(),
			}
			log.V(2).Infof("NewTarFSFromBytes: adding file %s with size %v", path, len(tarContents[path].data))
		default:
			return nil, fmt.Errorf("unknown type: %v in %s", header.Typeflag, header.Name)
		}
	}
	return &TarFS{
		tarContents: tarContents,
	}, nil
}
