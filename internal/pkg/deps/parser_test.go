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

package deps

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/bazelbuild/reclient/internal/pkg/execroot"
	"github.com/bazelbuild/reclient/internal/pkg/logger"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/digest"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/filemetadata"
	"github.com/google/go-cmp/cmp"

	lpb "github.com/bazelbuild/reclient/api/log"

	cpb "github.com/bazelbuild/remote-apis-sdks/go/api/command"
)

func TestGetDepsParseDFile(t *testing.T) {
	tests := []struct {
		name     string
		dContent []byte
	}{
		{
			name: "one header per line",
			dContent: []byte(`
	     	          foo.o: \
	     	          foo.c \
	     	          foo.h \
	     	       `),
		}, {
			name: "all headers same line",
			dContent: []byte(`
		          foo.o: foo.c foo.h
		       `),
		}, {
			name: "all headers same line with slash",
			dContent: []byte(`
		          foo.o: foo.c foo.h \
		       `),
		}, {
			name: "one same line one next line",
			dContent: []byte(`
		          foo.o: foo.c \
		          foo.h \
		       `),
		}, {
			name: "all next line",
			dContent: []byte(`
		          foo.o: \
		          foo.c foo.h \
		       `),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			existingFiles := map[string][]byte{
				"foo.h": []byte("HEADER"),
				"foo.c": []byte("SOURCE"),
				"foo.d": test.dContent,
			}
			r, cleanup := execroot.Setup(t, nil)
			defer cleanup()
			execroot.AddFilesWithContent(t, r, existingFiles)
			// Prevents parallel tests
			filemetadata.ResetGlobalCache()
			fms := filemetadata.NewSingleFlightCache()
			p := &Parser{ExecRoot: r, DigestStore: fms}

			wantDeps := fmt.Sprintf(
				"foo.c:%s\nfoo.h:%s\n",
				digest.NewFromBlob(existingFiles["foo.c"]),
				digest.NewFromBlob(existingFiles["foo.h"]))
			gotDeps, err := p.GetDeps("foo.d")
			if err != nil {
				t.Errorf(`GetDeps("foo.d") returned error: %v`, err)
			}
			if diff := cmp.Diff(wantDeps, gotDeps); diff != "" {
				t.Errorf(`GetDeps("foo.d") returned diff (-want, +got): %v`, diff)
			}
		})
	}
}

func TestVerifyDepsFile(t *testing.T) {
	r, cleanup := execroot.Setup(t, nil)
	defer cleanup()
	existingFiles := map[string][]byte{
		"foo/foo.h": []byte("HEADER"),
		"foo.c":     []byte("SOURCE"),
		"foo.d":     []byte("foo.o: foo.c foo/foo.h \\"),
	}
	execroot.AddFilesWithContent(t, r, existingFiles)
	// Prevents parallel tests
	filemetadata.ResetGlobalCache()
	fms := filemetadata.NewSingleFlightCache()
	p := &Parser{ExecRoot: r, DigestStore: fms}
	meta := &logger.LogRecord{
		LogRecord: &lpb.LogRecord{
			LocalMetadata: &lpb.LocalMetadata{
				EventTimes: make(map[string]*cpb.TimeInterval),
			},
		},
	}
	p.WriteDepsFile("foo.d", meta)

	ok, err := p.VerifyDepsFile("foo.d", meta)
	if !ok || err != nil {
		t.Errorf("VerifyDepsFile returned <%v, %v>, expected <true, nil>", ok, err)
	}
	time.Sleep(time.Second)
	execroot.AddFileWithContent(t, filepath.Join(r, "foo/foo.h"), []byte("other"))
	filemetadata.ResetGlobalCache()
	ok, err = p.VerifyDepsFile("foo.d", meta)
	if ok || err != nil {
		t.Errorf("VerifyDepsFile returned <%v, %v>, expected <false, nil>", ok, err)
	}
	if _, ok := meta.LocalMetadata.EventTimes[logger.EventLERCWriteDeps]; !ok {
		t.Errorf("WriteDepsFile did not record timing metadata")
	}
	if _, ok := meta.LocalMetadata.EventTimes[logger.EventLERCVerifyDeps]; !ok {
		t.Errorf("VerifyDepsFile did not record timing metadata")
	}
}
