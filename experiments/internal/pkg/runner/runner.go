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

// Package runner is used to create new experiments using experiment texproto files
package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"team/foundry-x/re-client/experiments/internal/pkg/experiment"
	"team/foundry-x/re-client/experiments/internal/pkg/gcs"

	"google.golang.org/protobuf/encoding/prototext"

	epb "team/foundry-x/re-client/experiments/api/experiment"
)

// RunExperiment will create a new experiment based on given experiment textproto
func RunExperiment(expDefPath string, dateSuffix string, gcpProject string, resBucket string, logFrequency int) (string, error) {
	exp, err := os.ReadFile(expDefPath)
	if err != nil {
		return "", fmt.Errorf("Failed to read experiment definition file: %v", err)
	}
	expPb := &epb.Experiment{}
	if err := prototext.Unmarshal(exp, expPb); err != nil {
		return "", fmt.Errorf("Failed to parse experiment definition file: %v", err)
	}
	date := time.Now().Format("20060102-1504")
	if dateSuffix != "" {
		date = dateSuffix
	}
	e, err := experiment.NewExperiment(expPb, filepath.Dir(expDefPath), gcpProject, date, resBucket, logFrequency)
	if err != nil {
		return "", fmt.Errorf("Failed to create experiment: %v", err)
	}
	ctx := context.Background()

	expName := expPb.GetName() + "_" + date
	dest := fmt.Sprintf("gs://%v/%v/", resBucket, expName)
	if err := gcs.Copy(ctx, expDefPath, dest); err != nil {
		return "", fmt.Errorf("Failed to upload experiment definition: %v", err)
	}
	if err := e.Run(ctx); err != nil {
		return "", fmt.Errorf("Failed to run experiment: %v", err)
	}
	return expName, nil
}
