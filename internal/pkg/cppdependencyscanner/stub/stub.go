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

// Package includescanner implements the cppdependencyscanner.DepsScanner with
// a stub implementation that always exits with an error.
// This should only be used in integration tests when an external depsscanner
// service will be used instead.
package includescanner

import (
	"context"
	"fmt"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/filemetadata"

	"github.com/bazelbuild/reclient/internal/pkg/logger"
	log "github.com/golang/glog"
)

// Name of the include scanner. This is overridden at link time by x_defs in BUILD.bazel.
var Name = ""

// IsStub reflects that this is a stub deps scanner.
const IsStub = true

// StubClient is a stub implementation of DepsScanner.
type StubClient struct{}

// New exits with a fatal error as StubClient should never be created.
func New(_ filemetadata.Cache, _, _ string, _ int, _ []string, _ bool, _ *logger.Logger) *StubClient {
	log.Fatalf("Invalid call to New() for StubClient.")
	return nil
}

// Close implements DepsScanner.Close.
// It always exits with a fatal error.
func (ds *StubClient) Close() {
	log.Fatalf("Invalid call to Close on StubClient.Close().")
}

// ProcessInputs implements DepsScanner.ProcessInputs.
// It always returns an error.
func (ds *StubClient) ProcessInputs(_ context.Context, _ string, _ []string, _, _ string, _ []string) ([]string, bool, error) {
	return nil, false, fmt.Errorf("invalid call to StubClient.ProcessInputs()")
}

// ShouldIgnorePlugin implements DepsScanner.ShouldIgnorePlugin.
// It always exits with a fatal error.
func (ds *StubClient) ShouldIgnorePlugin(plugin string) bool {
	log.Fatalf("Invalid call to StubClient.ShouldIgnorePlugin().")
	return false
}

// SupportsCache implements DepsScanner.SupportsCache.
func (ds *StubClient) SupportsCache() bool {
	return false
}
