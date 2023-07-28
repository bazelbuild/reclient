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

// Package downloadmismatch downloads compare build mismatch outputs.
package downloadmismatch

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/client"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/digest"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/tool"
	"google.golang.org/protobuf/proto"

	spb "github.com/bazelbuild/reclient/api/stats"
)

const (
	metricsFile = "rbe_metrics.pb"
	// DownloadDir is the directory name of downloaded mismatched.
	DownloadDir = "reclient_mismatches"
	// LocalOutputDir stores all the local outputs of one mismatch.
	LocalOutputDir = "local"
	// RemoteOutputDir stores all the remote outputs of one mismatch.
	RemoteOutputDir = "remote"
)

func dedup(list []string) []string {
	// key is cleaned path, value is original path.
	// returns original paths.
	fileSet := make(map[string]string)
	for _, f := range list {
		if _, found := fileSet[filepath.Clean(f)]; found {
			continue
		}
		fileSet[filepath.Clean(f)] = f
	}
	var dlist []string
	for _, f := range fileSet {
		dlist = append(dlist, f)
	}
	return dlist
}

func readMismatchesFromFile(fp string) (*spb.Stats, error) {
	blobs, err := ioutil.ReadFile(fp)
	if err != nil {
		return nil, fmt.Errorf("Failed to read mismatches from file: %v: %v", fp, err)
	}
	sPb := &spb.Stats{}
	if e := proto.Unmarshal(blobs, sPb); e != nil {
		return nil, fmt.Errorf("Failed to transform file content into stats proto: %v: %v", fp, e)
	}
	return sPb, nil
}

// DownloadMismatches finds mismatches from rbe_metrics.pb in the outputDir using the instance and service
// provided. The downloaded build outputs will be stored in
// #outputDir/reclient_mismatches/#actionDigest/#local_or_remote/#outputDigest.
func DownloadMismatches(logDir string, outputDir string, grpcClient *client.Client) error {
	mismatchOutputDir := filepath.Join(outputDir, DownloadDir)
	os.RemoveAll(mismatchOutputDir)
	ctx := context.Background()
	stats, err := readMismatchesFromFile(filepath.Join(logDir, metricsFile))
	if err != nil {
		return err
	}
	if stats.Verification == nil {
		return fmt.Errorf("No compare build stats in rbe_metrics.pb, was it a compare build?")
	}

	defer grpcClient.Close()
	toolClient := &tool.Client{GrpcClient: grpcClient}

	for _, mismatch := range stats.Verification.Mismatches {
		actionDg, err := digest.NewFromString(mismatch.ActionDigest)
		if err != nil {
			return err
		}
		actionPath := filepath.Join(mismatchOutputDir, actionDg.Hash)
		outputPath := filepath.Join(actionPath, strings.Replace(mismatch.Path, "/", "_", -1))
		if err := os.MkdirAll(outputPath, os.ModePerm); err != nil {
			return err
		}
		// In old reproxy versions, RemoteDigest/LocalDigest is still used.
		if mismatch.RemoteDigest != "" {
			mismatch.RemoteDigests = dedup(append(mismatch.RemoteDigests, mismatch.RemoteDigest))
		}
		if mismatch.LocalDigest != "" {
			mismatch.LocalDigests = dedup(append(mismatch.LocalDigests, mismatch.LocalDigest))
		}
		for _, dgStr := range mismatch.RemoteDigests {
			if err := downloadToFile(ctx, toolClient, filepath.Join(outputPath, RemoteOutputDir), dgStr); err != nil {
				return err
			}
		}
		for _, dgStr := range mismatch.LocalDigests {
			if err := downloadToFile(ctx, toolClient, filepath.Join(outputPath, LocalOutputDir), dgStr); err != nil {
				return err
			}
		}
	}
	return nil
}

func downloadToFile(ctx context.Context, toolClient *tool.Client, downloadPath, digestStr string) error {
	os.MkdirAll(downloadPath, os.ModePerm)
	digestPb, _ := digest.NewFromString(digestStr)
	blobs, err := toolClient.DownloadBlob(ctx, digestStr, "")
	if err != nil {
		return fmt.Errorf("Download error: %v", err)
	}
	fp := filepath.Join(downloadPath, digestPb.Hash)
	return ioutil.WriteFile(fp, []byte(blobs), os.ModePerm)
}
