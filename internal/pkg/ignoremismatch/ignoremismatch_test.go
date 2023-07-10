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

package ignoremismatch

import (
	"os"
	"testing"

	lpb "team/foundry-x/re-client/api/log"
)

const textConfig = `
rules {
  output_file_path_rule_spec {
    path_pattern {
      expression: ".*foo.*"
    }
  }
}
rules {
  output_file_path_rule_spec {
    path_pattern {
      expression: ".*bar.*"
    }
  }
}
`

func createTmpConfig(t *testing.T, config string) *os.File {
	t.Helper()
	tmpfile, err := os.CreateTemp("", "example")
	if err != nil {
		t.Fatalf("Unable to create tmp confg file: %v", err)
	}

	if _, err := tmpfile.Write([]byte(config)); err != nil {
		tmpfile.Close()
		os.Remove(tmpfile.Name())
		t.Fatalf("Unable to write config to the tmp file: %v", err)
	}
	return tmpfile
}

func TestCreationSuccess(t *testing.T) {
	_, err := New("test_config.textproto")
	if err != nil {
		t.Errorf("Error creating mismatch ignorer: %v", err)
	}
}

func TestCreationFailure(t *testing.T) {
	_, err := New("non-exist.textproto")
	if err == nil {
		t.Errorf("Expect error while creating mismatch ignorer but succeeded")
	}
}

func TestMismatchProcessing(t *testing.T) {
	f := createTmpConfig(t, textConfig)
	defer os.Remove(f.Name())
	defer f.Close()
	mi, err := New(f.Name())
	if err != nil {
		t.Fatalf("Error creating mismatch ignorer: %v", err)
	}

	var recs []*lpb.LogRecord
	recs = append(recs, &lpb.LogRecord{
		LocalMetadata: &lpb.LocalMetadata{
			Verification: &lpb.Verification{
				Mismatches: []*lpb.Verification_Mismatch{
					&lpb.Verification_Mismatch{
						Path: "bar.o",
					},
					&lpb.Verification_Mismatch{
						Path: "baz.o",
					},
				},
			},
		},
	})
	for _, r := range recs {
		mi.ProcessLogRecord(r)
	}
	if !recs[0].GetLocalMetadata().GetVerification().GetMismatches()[0].Ignored {
		t.Error("1st mismatch is not ignored")
	}
	if recs[0].GetLocalMetadata().GetVerification().GetMismatches()[1].Ignored {
		t.Error("2nd mismatch is unexpectedly ignored")
	}
	if ignored := recs[0].GetLocalMetadata().GetVerification().TotalIgnoredMismatches; ignored != 1 {
		t.Errorf("Total ignored is expected to be 1, got: %v", ignored)
	}

}
