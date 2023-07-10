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

package downloadmismatch

import (
	"bytes"
	"context"
	"google.golang.org/protobuf/proto"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/digest"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/fakes"
	lpb "team/foundry-x/re-client/api/log"
	spb "team/foundry-x/re-client/api/stats"
)

func TestReading(t *testing.T) {
	numRecords := int64(2)
	invocationIds := []string{"random id"}
	fileName := "test_read.pb"
	stats := &spb.Stats{
		NumRecords:    numRecords,
		InvocationIds: invocationIds,
	}
	f, err := os.Create(fileName)
	if err != nil {
		t.Errorf("error creating test file")
	}
	blob, _ := proto.Marshal(stats)
	f.Write(blob)
	f.Close()

	readStats, err := readMismatchesFromFile(fileName)
	if proto.Equal(readStats, stats) {
		return
	}
	t.Errorf("protobuf mismatch, expected: %v, received: %v", stats, readStats)
}

func TestDownload(t *testing.T) {
	server, _ := fakes.NewServer(t)
	grpcClient, _ := server.NewTestClient(context.Background())
	actionDg := &digest.Digest{
		Hash: "2ac66acc7116b6555eebfc5f578530dd1d1842b5c18729aedc63776ebf010377",
		Size: 147,
	}
	content1 := []byte("blaa")
	content2 := []byte("aba")
	contentHash1 := server.CAS.Put(content1)
	contentHash2 := server.CAS.Put(content2)
	stats := &spb.Stats{
		Verification: &lpb.Verification{
			Mismatches: []*lpb.Verification_Mismatch{&lpb.Verification_Mismatch{
				ActionDigest:  actionDg.String(),
				RemoteDigests: []string{contentHash1.String()},
				LocalDigest:   contentHash2.String(),
			}},
		},
	}
	blob, _ := proto.Marshal(stats)
	f, _ := os.Create("rbe_metrics.pb")
	f.Write(blob)
	f.Close()

	if err := DownloadMismatches(".", ".", grpcClient); err != nil {
		t.Errorf("DownloadMismatches encountered unexpected error: %v", err)
	}
	remoteFp := filepath.Join("reclient_mismatches", actionDg.Hash, "remote", contentHash1.Hash)
	localFp := filepath.Join("reclient_mismatches", actionDg.Hash, "local", contentHash2.Hash)
	verifyContent(remoteFp, t, content1)
	verifyContent(localFp, t, content2)
}

func verifyContent(fp string, t *testing.T, content []byte) {
	blobs, err := ioutil.ReadFile(fp)
	if err != nil {
		t.Errorf("Can not read the wanted file %v, error: %v", fp, err)
	}
	if bytes.Compare(blobs, content) != 0 {
		t.Errorf("downloaded content is different from original. Wanted: %v\n Received: %v\n", content, blobs)
	}
}
