// Copyright 2024 Google LLC
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

package tarfs

import (
	"io/ioutil"
	"os"
	"testing"
)

const (
	tarFilePath = "testdata/test.tar"
	indexFile   = "index.html"
)

func TestTarfs(t *testing.T) {
	tarFile, err := os.Open(tarFilePath)
	if err != nil {
		t.Fatalf("Unable to open file %v: %v", tarFilePath, err)
	}
	defer tarFile.Close()
	fileBytes, err := ioutil.ReadAll(tarFile)
	if err != nil {
		t.Fatalf("Unable to read file %v: %v", tarFilePath, err)
	}

	tarFS, err := NewTarFSFromBytes(fileBytes)
	if err != nil {
		t.Fatalf("Unable to create tarfs from bytes: %v", err)
	}
	tf, err := tarFS.Open(indexFile)
	if err != nil {
		t.Errorf("Unable to open file: %v", err)
	}
	defer tf.Close()

	wantContents := "<html>\n</html>\n"
	gotContents, err := ioutil.ReadAll(tf)
	if err != nil {
		t.Fatalf("Unable to read file: %v", err)
	}
	if string(gotContents) != wantContents {
		t.Fatalf("File %v in tar, expected %v, got %v", indexFile, wantContents, string(gotContents))
	}
}
