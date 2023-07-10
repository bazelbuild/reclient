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

package clanglink

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	arFileSignature     = "!<arch>\n"
	bsdFilenamePrefix   = "#1/"
	fileSignatureLen    = int64(len(arFileSignature))
	gnuExtendedFilename = "//"
	headerSize          = 60
	thinFileSignature   = "!<thin>\n"
)

type header struct {
	Name     string
	FileSize int64
}

// This arReader is an implementation of unix's ar used to read archive files.
// For more information on ar file format, see https://en.wikipedia.org/wiki/Ar_(Unix)#File_format_details
type arReader struct {
	thinFilePrefix string
	thin           bool
	path           string
	r              io.Reader
}

// arPath is the absolute path of the archive file.
// thinFilePrefix is the path of the archive file relative to the wd which will be pepended to thin archive file entries.
func newArReader(arPath string, thinFilePrefix string) (*arReader, error) {
	f, err := os.Open(arPath)
	if err != nil {
		return nil, err
	}

	var r io.Reader = f
	// Check if the file signature is valid.
	fs := make([]byte, fileSignatureLen)
	if _, err := io.ReadFull(r, fs); err != nil {
		f.Close()
		return nil, err
	}
	if string(fs) != arFileSignature && string(fs) != thinFileSignature {
		f.Close()
		return nil, fmt.Errorf("%v: file format not recognized", arPath)
	}

	return &arReader{
		thinFilePrefix: thinFilePrefix,
		path:           arPath,
		r:              r,
		thin:           string(fs) == thinFileSignature}, nil
}

// !<arch> files will output the file names as written in the archive.
// !<thin> files will output the files relative to the current working directory.
func (ar *arReader) ReadFileNames() ([]string, error) {
	var files []string
	var gnuFilenames []byte
	var thinFiles []string
	var next int

	for {
		h, err := ar.readHeader()

		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		if h.Name == gnuExtendedFilename {
			d, err := ar.readFullBytes(h.FileSize)
			if err != nil {
				return nil, fmt.Errorf("%v: malformed archive: %w", ar.path, err)
			}
			gnuFilenames = d
			thinFiles = strings.Split(string(gnuFilenames), "\n")
			next = 0
		} else if h.Name == "/" {
			err = ar.skipBytes(h.FileSize)
			if err != nil {
				return nil, fmt.Errorf("%v: malformed archive: %w", ar.path, err)
			}
		} else if strings.HasPrefix(h.Name, "/") {
			if ar.thin {
				file := filepath.Join(ar.thinFilePrefix, thinFiles[next])
				files = append(files, file)
				next++
			} else {
				offset, err := strconv.ParseInt(strings.TrimLeft(h.Name, "/"), 10, 64)
				if err != nil {
					return nil, err
				}

				end := offset
				// "/" Denotes the end of a file name.
				for i := offset; gnuFilenames[i] != '/'; i++ {
					end++
				}
				fn := gnuFilenames[offset:end]
				files = append(files, string(fn))
				// Skip the data section.
				err = ar.skipBytes(h.FileSize)
				if err != nil {
					return nil, fmt.Errorf("%v: malformed archive: %w", ar.path, err)
				}
			}
		} else if strings.HasPrefix(h.Name, bsdFilenamePrefix) {
			d, err := ar.readFullBytes(h.FileSize)
			if err != nil {
				return nil, fmt.Errorf("%v: malformed archive: %w", ar.path, err)
			}

			size, err := strconv.ParseInt(strings.TrimLeft(h.Name, bsdFilenamePrefix), 10, 64)
			if err != nil {
				return nil, err
			}
			files = append(files, string(d[:size]))
		} else {
			files = append(files, h.Name)
			// Skip the data section.
			err = ar.skipBytes(h.FileSize)
			if err != nil {
				return nil, fmt.Errorf("%v: malformed archive: %w", ar.path, err)
			}
		}
	}

	if err := ar.resetOffsetToContentStart(); err != nil {
		return nil, err
	}
	return files, nil
}

func (ar *arReader) readHeader() (*header, error) {
	headerBuf, err := ar.readFullBytes(headerSize)
	if err != nil {
		return nil, err
	}

	fs, err := strconv.ParseInt((strings.TrimSpace(string(headerBuf[48:58]))), 10, 64)
	if err != nil {
		return nil, err
	}
	h := header{
		Name:     strings.TrimSpace(string(headerBuf[0:16])),
		FileSize: fs,
	}

	return &h, nil
}

func (ar *arReader) readFullBytes(length int64) ([]byte, error) {
	data := make([]byte, length)
	if _, err := io.ReadFull(ar.r, data); err != nil {
		return nil, err
	}
	return data, nil
}

func (ar *arReader) skipBytes(length int64) error {
	if seeker, ok := ar.r.(io.Seeker); ok {
		_, err := seeker.Seek(length, io.SeekCurrent)
		return err
	}
	return nil
}

func (ar *arReader) resetOffsetToContentStart() error {
	if seeker, ok := ar.r.(io.Seeker); ok {
		_, err := seeker.Seek(fileSignatureLen, io.SeekStart)
		return err
	}
	return nil
}

func (ar *arReader) Close() error {
	if closer, ok := ar.r.(io.Closer); ok {
		err := closer.Close()
		return err
	}
	return nil
}
