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

// Package ignoremismatch contains to logic to mark mismatches in log records as
// ignored based on provided rules.
package ignoremismatch

import (
	"fmt"
	"io/ioutil"

	log "github.com/golang/glog"
	"google.golang.org/protobuf/encoding/prototext"

	lpb "team/foundry-x/re-client/api/log"
	ppb "team/foundry-x/re-client/api/proxy"
)

// MismatchIgnorer checkes mismatches in the log records and mark a mismatch as ignored if any mismatch ignoring rule matches it.
type MismatchIgnorer struct {
	rules []ignoreRule
}

// New creates a mismatch ignorer from a textproto file containing the rule config.
func New(configPath string) (*MismatchIgnorer, error) {
	if configPath == "" {
		return nil, nil
	}
	config, err := readMismatchIgnoreConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create mismatch ignore config from file %v, error: %v", configPath, err)
	}
	return fromConfig(config)
}

// ProcessLogRecord marks a mismatch in the log record as ignored if any mismatch ignoring rule matches it.
func (mi *MismatchIgnorer) ProcessLogRecord(r *lpb.LogRecord) {
	if mi == nil {
		return
	}
	var ignored int32
	if r.GetLocalMetadata().GetVerification().GetMismatches() == nil {
		return
	}
	for _, m := range r.GetLocalMetadata().GetVerification().GetMismatches() {
		if mi.shouldIgnore(m) {
			m.Ignored = true
			ignored++
		}
	}
	r.GetLocalMetadata().GetVerification().TotalIgnoredMismatches = ignored
}

func readMismatchIgnoreConfig(configPath string) (*ppb.MismatchIgnoreConfig, error) {
	blob, err := ioutil.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read mismatch ignore config from %v, error: %v", configPath, err)
	}
	config := &ppb.MismatchIgnoreConfig{}
	if err = prototext.Unmarshal(blob, config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal text proto to MismatchIgnoreConfig proto, text: %v error: %v", string(blob), err)
	}
	return config, nil
}

func fromConfig(conf *ppb.MismatchIgnoreConfig) (*MismatchIgnorer, error) {
	mi := &MismatchIgnorer{}
	for _, rpb := range conf.GetRules() {
		r, err := createFromRuleProto(rpb)
		if err != nil {
			return nil, fmt.Errorf("failed to create rule from %+v", rpb)
		}
		mi.rules = append(mi.rules, r)
	}
	return mi, nil
}

func (mi *MismatchIgnorer) shouldIgnore(m *lpb.Verification_Mismatch) bool {
	for _, r := range mi.rules {
		ignored, err := r.shouldIgnore(m)
		if err != nil {
			log.Warningf("failed to process mismatch with rule, rule: %+v, mismatch: %+v, error: %v", r, m, err)
			continue
		}
		if ignored {
			return true
		}
	}
	return false
}
