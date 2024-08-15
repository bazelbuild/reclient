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

// Package bigquerytranslator translates log records to bigtable values.
package bigquerytranslator

import (
	"sort"

	"cloud.google.com/go/bigquery"
	lpb "github.com/bazelbuild/reclient/api/log"

	cpb "github.com/bazelbuild/remote-apis-sdks/go/api/command"
)

// Item implements the bigquery interface used for bulk inserts.
type Item struct {
	*lpb.LogRecord
}

func platform(p map[string]string) []map[string]bigquery.Value {
	var res []map[string]bigquery.Value
	var keys []string
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := p[k]
		e := map[string]bigquery.Value{
			"key":   k,
			"value": v,
		}
		res = append(res, e)
	}
	return res
}

func collectInputs(input *cpb.InputSpec) map[string]bigquery.Value {
	inputFiles := make([]string, 0)
	if input.GetInputs() != nil {
		inputFiles = input.GetInputs()
	}

	virtualInputs := []map[string]bigquery.Value{}
	for _, vi := range input.GetVirtualInputs() {
		virtualInputs = append(virtualInputs, map[string]bigquery.Value{
			"path":               vi.GetPath(),
			"contents":           vi.GetContents(),
			"is_executable":      vi.GetIsExecutable(),
			"is_empty_directory": vi.GetIsEmptyDirectory(),
		})
	}

	excludeInputs := []map[string]bigquery.Value{}
	for _, ei := range input.GetExcludeInputs() {
		excludeInputs = append(excludeInputs, map[string]bigquery.Value{
			"regex": ei.GetRegex(),
			"type":  ei.GetType(),
		})
	}

	environmentVariables := []map[string]bigquery.Value{}
	var keys []string
	for k := range input.GetEnvironmentVariables() {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := input.GetEnvironmentVariables()[k]
		environmentVariables = append(environmentVariables, map[string]bigquery.Value{
			"key":   k,
			"value": v,
		})
	}
	return map[string]bigquery.Value{
		"inputs":                inputFiles,
		"virtual_inputs":        virtualInputs,
		"exclude_inputs":        excludeInputs,
		"environment_variables": environmentVariables,
	}
}

func collectOutputs(output *cpb.OutputSpec) map[string]bigquery.Value {
	res := map[string]bigquery.Value{}
	if output.GetOutputFiles() != nil {
		res["output_files"] = output.GetOutputFiles()
	}
	if output.GetOutputDirectories() != nil {
		res["output_directories"] = output.GetOutputDirectories()
	}
	return res
}

func collectResult(result *cpb.CommandResult) map[string]bigquery.Value {
	return map[string]bigquery.Value{
		"status":    result.GetStatus(),
		"exit_code": result.GetExitCode(),
		"msg":       result.GetMsg(),
	}
}

func collectRemoteMetadata(rm *lpb.RemoteMetadata) (map[string]bigquery.Value, error) {
	res := map[string]bigquery.Value{
		"result":                   collectResult(rm.GetResult()),
		"cache_hit":                rm.GetCacheHit(),
		"num_input_files":          rm.GetNumInputFiles(),
		"num_input_directories":    rm.GetNumInputDirectories(),
		"total_input_bytes":        rm.GetTotalInputBytes(),
		"num_output_files":         rm.GetNumOutputFiles(),
		"num_output_directories":   rm.GetNumOutputDirectories(),
		"total_output_bytes":       rm.GetTotalOutputBytes(),
		"command_digest":           rm.GetCommandDigest(),
		"action_digest":            rm.GetActionDigest(),
		"logical_bytes_uploaded":   rm.GetLogicalBytesUploaded(),
		"real_bytes_uploaded":      rm.GetRealBytesUploaded(),
		"logical_bytes_downloaded": rm.GetLogicalBytesDownloaded(),
		"real_bytes_downloaded":    rm.GetRealBytesDownloaded(),
		"stderr_digest":            rm.GetStderrDigest(),
		"stdout_digest":            rm.GetStdoutDigest(),
	}
	eventTimes := []map[string]bigquery.Value{}
	var keys []string
	for k := range rm.GetEventTimes() {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		et := rm.GetEventTimes()[k]
		v := map[string]bigquery.Value{}
		if et.GetFrom() != nil && et.GetTo() != nil {
			v = map[string]bigquery.Value{
				"from": et.GetFrom().AsTime(),
				"to":   et.GetTo().AsTime(),
			}
		}
		cur := map[string]bigquery.Value{
			"key":   k,
			"value": v,
		}
		eventTimes = append(eventTimes, cur)
	}
	res["event_times"] = eventTimes

	outputFileDigests := []map[string]bigquery.Value{}
	keys = nil
	for k := range rm.GetOutputFileDigests() {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := rm.GetOutputFileDigests()[k]
		outputFileDigests = append(outputFileDigests, map[string]bigquery.Value{
			"key":   k,
			"value": v,
		})
	}
	res["output_file_digests"] = outputFileDigests

	outputDirectoryDigests := []map[string]bigquery.Value{}
	keys = nil
	for k := range rm.GetOutputDirectoryDigests() {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := rm.GetOutputDirectoryDigests()[k]
		outputDirectoryDigests = append(outputDirectoryDigests, map[string]bigquery.Value{
			"key":   k,
			"value": v,
		})
	}
	res["output_directory_digests"] = outputDirectoryDigests
	return res, nil
}

func collectLocalMetadata(lm *lpb.LocalMetadata) (map[string]bigquery.Value, error) {
	res := map[string]bigquery.Value{
		"result":           collectResult(lm.GetResult()),
		"executed_locally": lm.GetExecutedLocally(),
		"valid_cache_hit":  lm.GetValidCacheHit(),
		"updated_cache":    lm.GetUpdatedCache(),
	}

	mismatches := []map[string]bigquery.Value{}
	for _, mismatch := range lm.GetVerification().GetMismatches() {
		remoteDgs := make([]string, 0)
		if mismatch.GetRemoteDigests() != nil {
			remoteDgs = mismatch.GetRemoteDigests()
		}
		localDgs := make([]string, 0)
		if mismatch.GetLocalDigests() != nil {
			localDgs = mismatch.GetLocalDigests()
		}
		mismatches = append(mismatches, map[string]bigquery.Value{
			"path":           mismatch.GetPath(),
			"remote_digests": remoteDgs,
			"local_digests":  localDgs,
			"determinism":    mismatch.GetDeterminism(),
		})
	}
	res["verification"] = map[string]bigquery.Value{
		"mismatches":       mismatches,
		"total_mismatches": lm.GetVerification().GetTotalMismatches(),
		"total_verified":   lm.GetVerification().GetTotalVerified(),
	}

	eventTimes := []map[string]bigquery.Value{}
	var keys []string
	for k := range lm.GetEventTimes() {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		et := lm.GetEventTimes()[k]
		v := map[string]bigquery.Value{}
		if et.GetFrom() != nil && et.GetTo() != nil {
			v = map[string]bigquery.Value{
				"from": et.GetFrom().AsTime(),
				"to":   et.GetTo().AsTime(),
			}
		}
		cur := map[string]bigquery.Value{
			"key":   k,
			"value": v,
		}
		eventTimes = append(eventTimes, cur)
	}
	res["event_times"] = eventTimes

	environment := []map[string]bigquery.Value{}
	keys = nil
	for k := range lm.GetEnvironment() {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := lm.GetEnvironment()[k]
		environment = append(environment, map[string]bigquery.Value{
			"key":   k,
			"value": v,
		})
	}
	res["environment"] = environment

	labels := []map[string]bigquery.Value{}
	keys = nil
	for k := range lm.GetLabels() {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := lm.GetLabels()[k]
		labels = append(labels, map[string]bigquery.Value{
			"key":   k,
			"value": v,
		})
	}
	res["labels"] = labels
	return res, nil
}

// Save returns the item to be inserted from the given log record
// and a unique insertId that can be used to de-duplicate inserts for upto
// 60 seconds.
func (i *Item) Save() (map[string]bigquery.Value, string, error) {
	inputs := collectInputs(i.GetCommand().GetInput())
	outputs := collectOutputs(i.GetCommand().GetOutput())
	result := collectResult(i.GetResult())
	remoteMetadata, err := collectRemoteMetadata(i.GetRemoteMetadata())
	if err != nil {
		return nil, "", err
	}
	localMetadata, err := collectLocalMetadata(i.GetLocalMetadata())
	if err != nil {
		return nil, "", err
	}
	args := i.GetCommand().GetArgs()
	if args == nil {
		args = make([]string, 0)
	}
	return map[string]bigquery.Value{
		"command": map[string]bigquery.Value{
			"identifiers": map[string]bigquery.Value{
				"command_id":                i.GetCommand().GetIdentifiers().GetCommandId(),
				"invocation_id":             i.GetCommand().GetIdentifiers().GetInvocationId(),
				"correlated_invocations_id": i.GetCommand().GetIdentifiers().GetCorrelatedInvocationsId(),
				"tool_name":                 i.GetCommand().GetIdentifiers().GetToolName(),
				"tool_version":              i.GetCommand().GetIdentifiers().GetToolVersion(),
				"execution_id":              i.GetCommand().GetIdentifiers().GetExecutionId(),
			},
			"exec_root":         i.GetCommand().GetExecRoot(),
			"input":             inputs,
			"output":            outputs,
			"args":              args,
			"execution_timeout": i.GetCommand().GetExecutionTimeout(),
			"working_directory": i.GetCommand().GetWorkingDirectory(),
			"platform":          platform(i.GetCommand().GetPlatform()),
		},
		"result":            result,
		"remote_metadata":   remoteMetadata,
		"local_metadata":    localMetadata,
		"completion_status": i.CompletionStatus,
	}, i.GetCommand().GetIdentifiers().GetCommandId(), nil
}
