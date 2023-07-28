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

// Package perfgate aids in uploading experiment metric data to the perfgate tool
// for further analysis and regression detection
package perfgate

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/bazelbuild/reclient/experiments/internal/pkg/gcs"

	log "github.com/golang/glog"
)

// RunUploader will upload rbe_metric data from each experiment trial to perfgate.
func RunUploader(expDefPath string, resBucket string,
	expName string, gcpProject string, perfgatePath string, benchmarkFile string) error {
	ctx := context.Background()
	configs, err := gcs.List(ctx, fmt.Sprintf("gs://%v/%v", resBucket, expName))
	if err != nil {
		return fmt.Errorf("Failed to find directory for experiment %v: %v", expName, err)
	}
	for _, c := range configs {
		downloadMetric(ctx, c, perfgatePath, expDefPath, benchmarkFile)
	}
	return nil
}
func downloadMetric(ctx context.Context, path string, perfgatePath string, expDefPath string,
	benchmarkFile string) {
	trials, err := gcs.List(ctx, path)
	if err != nil {
		log.Warningf("Failed to find trials for experiment %v: %v", path, err)
	}
	for _, t := range trials {
		if err := gcs.Copy(ctx, fmt.Sprintf("%vrbe_metrics.pb", t), "/tmp/rbe_metrics.pb"); err != nil {
			log.Warningf("Couldn't find RBE metrics for %v: %v", t, err)
			continue
		}
		if err := gcs.Copy(ctx, fmt.Sprintf("%vtime.txt", t), "/tmp/time.txt"); err != nil {
			log.Warningf("Couldn't find elapsed time for %v: %v", t, err)
			continue
		}
		uploadToPerfgate(ctx, perfgatePath, expDefPath, benchmarkFile)
	}
}
func uploadToPerfgate(ctx context.Context, perfgatePath string, expDefPath string, benchmarkFile string) {
	oe, err := exec.CommandContext(ctx, perfgatePath,
		"--rbe_metric_file=/tmp/rbe_metrics.pb", "--rbe_time_file=/tmp/time.txt",
		"--exp_def_path="+expDefPath,
		"--benchmark_path="+benchmarkFile).CombinedOutput()
	if err != nil {
		log.Warningf("Failed to upload to perfgate: %v, outerr: %v", err, string(oe))
	}
	log.Infof("Uploaded to perfgate: %v", string(oe))
}
