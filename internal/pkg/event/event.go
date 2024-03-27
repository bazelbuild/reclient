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

// Package event provides constant names for time metrics.
package event

// These are duration events that we export time metrics on.
const (
	// LERCVerifyDeps: VerifyDeps time for LERC.
	LERCVerifyDeps = "LERCVerifyDeps"

	// LERCWriteDeps: WriteDeps time for LERC.
	LERCWriteDeps = "LERCWriteDeps"

	// LocalCommandExecution: actually running the command locally.
	LocalCommandExecution = "LocalCommandExecution"

	// LocalCommandQueued: duration the command has been queued for local execution.
	LocalCommandQueued = "LocalCommandQueued"

	// ProxyExecution: proxy end-to-end time, regardless of execution strategy.
	ProxyExecution = "ProxyExecution"

	// ProcessInputs: proxy time for IncludeProcessor.ProcessInputs.
	ProcessInputs = "ProcessInputs"

	// ProcessInputsShallow: proxy time for IncludeProcessor.ProcessInputsShallow.
	ProcessInputsShallow = "ProcessInputsShallow"

	// CPPInputProcessor measures the time taken for C++ input processor.
	CPPInputProcessor = "CPPInputProcessor"

	// CPPInputProcessor measures the number of times goma was restarted.
	GomaInputProcessorRestart = "GomaInputProcessorRestart"

	// InputProcessorWait measures the time spent waiting for local resources to start
	// input processing.
	InputProcessorWait = "InputProcessorWait"

	// InputProcessorCacheLookup measures the time spent retrieving inputs from deps cache.
	InputProcessorCacheLookup = "InputProcessorCacheLookup"

	// RacingFinalizationOverhead: time spent finalizing the result of a raced action by
	// cancelling either remote or local, and moving outputs to their correct location in case
	// remote wins.
	RacingFinalizationOverhead = "RacingFinalizationOverhead"

	// AtomicOutputOverhead: time spent writing outputs atomically.
	AtomicOutputOverhead = "AtomicOutputOverhead"

	// PostBuildMetricsUpload: time spent post build to upload metrics to Cloud Monitoring.
	PostBuildMetricsUpload = "PostBuildMetricsUpload"

	// PostBuildMetricsUpload: time spent post build to aggregate rpl file into a stats proto.
	PostBuildAggregateRpl = "PostBuildAggregateRpl"

	// PostBuildMetricsUpload: time spent post build to load an rpl file from disk.
	PostBuildLoadRpl = "PostBuildLoadRpl"

	// PostBuildMismatchesIgnore: time spent marking mismatches as ignored based on the input rule.
	PostBuildMismatchesIgnore = "PostBuildMismatchesIgnore"

	// ProxyUptime is the uptime of the reproxy.
	ProxyUptime = "ProxyUptime"

	// BootstrapStartup is the time taken to run bootstrap to start reproxy.
	BootstrapStartup = "BootstrapStartup"

	// BootstrapShutdown is the time taken to run bootstrap to shutdown reproxy.
	BootstrapShutdown = "BootstrapShutdown"

	// DepsCacheLoad is the load time of the deps cache.
	DepsCacheLoad = "DepsCacheLoad"

	// DepsCacheWrite is the write time of the deps cache.
	DepsCacheWrite = "DepsCacheWrite"

	// DepsCacheLoadCount is the number of deps cache entries loaded at reproxy startup.
	DepsCacheLoadCount = "DepsCacheLoadCount"

	// DepsCacheWriteCount is the number of deps cache entries written at reproxy shutdown.
	DepsCacheWriteCount = "DepsCacheWriteCount"
)
