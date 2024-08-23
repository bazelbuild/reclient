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

package bigquerytranslator

import (
	"testing"

	"cloud.google.com/go/bigquery"
	"github.com/google/go-cmp/cmp"

	lpb "github.com/bazelbuild/reclient/api/log"

	cpb "github.com/bazelbuild/remote-apis-sdks/go/api/command"
)

func TestItemSave(t *testing.T) {
	i := &Item{
		LogRecord: &lpb.LogRecord{
			Command: &cpb.Command{
				Identifiers: &cpb.Identifiers{
					CommandId:               "a",
					InvocationId:            "b",
					CorrelatedInvocationsId: "build-bucket-id",
					ToolName:                "c",
					ExecutionId:             "d",
				},
				Args:     []string{"a", "b", "c"},
				ExecRoot: "/exec/root",
				Input: &cpb.InputSpec{
					Inputs: []string{"foo.h", "bar.h"},
					ExcludeInputs: []*cpb.ExcludeInput{
						&cpb.ExcludeInput{
							Regex: "*.bla",
							Type:  cpb.InputType_DIRECTORY,
						},
						&cpb.ExcludeInput{
							Regex: "*.blo",
							Type:  cpb.InputType_FILE,
						},
					},
					EnvironmentVariables: map[string]string{
						"k":  "v",
						"k1": "v1",
					},
				},
				Output: &cpb.OutputSpec{
					OutputFiles: []string{"a/b/out"},
				},
			},
			Result: &cpb.CommandResult{
				Status:   cpb.CommandResultStatus_CACHE_HIT,
				ExitCode: 42,
				Msg:      "message",
			},
			RemoteMetadata: &lpb.RemoteMetadata{
				Result: &cpb.CommandResult{
					Status:   cpb.CommandResultStatus_CACHE_HIT,
					ExitCode: 42,
					Msg:      "message",
				},
				CacheHit:            true,
				NumInputFiles:       2,
				NumInputDirectories: 3,
				TotalInputBytes:     4,
				CommandDigest:       "abc/10",
				ActionDigest:        "def/2",
				OutputFileDigests: map[string]string{
					"obj/buildtools/third_party/libc++/libc++/exception.o":   "8b001ac1a6c58572f9ccc8958435182885fa6ce9223313a1903a8c916b3b158f/9560",
					"obj/buildtools/third_party/libc++/libc++/exception.o.d": "639ee908f559ff1ff190cae1c4b16cc1e08c4fef8b1b7e94be19724ee986d3f0/264",
				},
			},
			LocalMetadata: &lpb.LocalMetadata{
				ValidCacheHit:   true,
				ExecutedLocally: false,
				UpdatedCache:    true,
				Verification: &lpb.Verification{
					TotalMismatches: 1,
					TotalVerified:   2,
					Mismatches: []*lpb.Verification_Mismatch{
						&lpb.Verification_Mismatch{
							Path:         "out/soong/.intermediates/packages/modules/Bluetooth/android/app/bluetooth-proto-enums-java-gen/android_common_apex33/javac/anno",
							ActionDigest: "62ad67b5b44a1813bb0e1ba348f1ccb40968a15c457fb7bd76198b8e2780cbad/147",
							LocalDigests: []string{"102b51b9765a56a3e899f7cf0ee38e5251f9c503b357b330a49183eb7b155604/2"},
							Determinism:  lpb.DeterminismStatus_UNKNOWN,
						},
					},
				},
			},
			CompletionStatus: lpb.CompletionStatus_STATUS_CACHE_HIT,
		},
	}

	want := map[string]bigquery.Value{
		"command": map[string]bigquery.Value{
			"identifiers": map[string]bigquery.Value{
				"command_id":                "a",
				"invocation_id":             "b",
				"correlated_invocations_id": "build-bucket-id",
				"tool_name":                 "c",
				"tool_version":              "",
				"execution_id":              "d",
			},
			"exec_root": "/exec/root",
			"input": map[string]bigquery.Value{
				"inputs":         []string{"foo.h", "bar.h"},
				"virtual_inputs": []map[string]bigquery.Value{},
				"exclude_inputs": []map[string]bigquery.Value{
					{
						"regex": "*.bla",
						"type":  cpb.InputType_DIRECTORY,
					},
					{
						"regex": "*.blo",
						"type":  cpb.InputType_FILE,
					},
				},
				"environment_variables": []map[string]bigquery.Value{
					{
						"key":   "k",
						"value": "v",
					},
					{
						"key":   "k1",
						"value": "v1",
					},
				},
			},
			"output": map[string]bigquery.Value{
				"output_files": []string{"a/b/out"},
			},
			"args":              []string{"a", "b", "c"},
			"execution_timeout": int32(0),
			"working_directory": "",
			"platform":          []map[string]bigquery.Value(nil),
		},
		"local_metadata": map[string]bigquery.Value{
			"result": map[string]bigquery.Value{
				"exit_code": int32(0),
				"msg":       "",
				"status":    cpb.CommandResultStatus_UNKNOWN,
			},
			"executed_locally": false,
			"valid_cache_hit":  true,
			"updated_cache":    true,
			"environment":      []map[string]bigquery.Value{},
			"event_times":      []map[string]bigquery.Value{},
			"labels":           []map[string]bigquery.Value{},
			"verification": map[string]bigquery.Value{
				"mismatches": []map[string]bigquery.Value{
					{
						"determinism":    lpb.DeterminismStatus_UNKNOWN,
						"local_digests":  []string{"102b51b9765a56a3e899f7cf0ee38e5251f9c503b357b330a49183eb7b155604/2"},
						"path":           string("out/soong/.intermediates/packages/modules/Bluetooth/android/app/bluetooth-proto-enums-java-gen/android_common_apex33/javac/anno"),
						"remote_digests": []string{},
					},
				},
				"total_mismatches": int32(1),
				"total_verified":   int64(2),
			},
		},
		"remote_metadata": map[string]bigquery.Value{
			"action_digest":            "def/2",
			"cache_hit":                true,
			"command_digest":           "abc/10",
			"event_times":              []map[string]bigquery.Value{},
			"num_input_directories":    int32(3),
			"num_input_files":          int32(2),
			"num_output_directories":   int32(0),
			"num_output_files":         int32(0),
			"logical_bytes_downloaded": int64(0),
			"logical_bytes_uploaded":   int64(0),
			"real_bytes_downloaded":    int64(0),
			"real_bytes_uploaded":      int64(0),
			"stderr_digest":            "",
			"stdout_digest":            "",
			"output_directory_digests": []map[string]bigquery.Value{},
			"output_file_digests": []map[string]bigquery.Value{
				{
					"key":   string("obj/buildtools/third_party/libc++/libc++/exception.o"),
					"value": string("8b001ac1a6c58572f9ccc8958435182885fa6ce9223313a1903a8c916b3b158f/9560"),
				}, {
					"key":   string("obj/buildtools/third_party/libc++/libc++/exception.o.d"),
					"value": string("639ee908f559ff1ff190cae1c4b16cc1e08c4fef8b1b7e94be19724ee986d3f0/264"),
				},
			},
			"result": map[string]bigquery.Value{
				"exit_code": int32(42),
				"msg":       "message",
				"status":    cpb.CommandResultStatus_CACHE_HIT,
			},
			"total_input_bytes":  int64(4),
			"total_output_bytes": int64(0),
		},
		"result": map[string]bigquery.Value{
			"exit_code": int32(42),
			"msg":       "message",
			"status":    cpb.CommandResultStatus_CACHE_HIT,
		},
		"completion_status": lpb.CompletionStatus_STATUS_CACHE_HIT,
	}
	wantID := "d"

	got, gotID, err := i.Save()
	if err != nil {
		t.Errorf("Item.Save() failed: %v", err)
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Item.Save() returned diff, (-want +got): %v", diff)
	}
	if gotID != wantID {
		t.Errorf("Item.Save() returned different IDs, want: %v, got: %v", wantID, gotID)
	}
}
