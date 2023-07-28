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

// Package version is used to define and print a consistent version number across all
// the binaries (reproxy, rewrapper, dumpstats and bootstrap) built from re-client
// repository.
package version

import (
	"flag"
	"fmt"
	"os"

	"github.com/bazelbuild/reclient/internal/pkg/rbeflag"

	log "github.com/golang/glog"
)

// All these variables are over-ridden during link time to set the appropriate
// version number. Refer to README.md for guidelines on when / how to update
// version numbers.
var (
	// versionMajor denotes the major version number.
	versionMajor = "0"

	// versionMinor denotes the minor version number.
	versionMinor = "1"

	// versionPatch denotes the patch version number.
	versionPatch = "1"

	// versionSHA denotes the SHA of the re-client repository from which
	// the current version is built.
	versionSHA = "undefined"

	// versionFlag is a boolean flag to determine whether to print version number or not.
	versionFlag = flag.Bool("version", false, "If provided, print the current binary version and exit.")
)

// PrintAndExitOnVersionFlag checks if the VersionFlag is specified, and if it is, then
// it prints the current version number and exits. If info is true, the version is also printed to the Info log.
func PrintAndExitOnVersionFlag(info bool) {
	rbeflag.Parse()
	v := CurrentVersion()
	if info {
		log.Infof("Version: %s\n", v)
	}
	if *versionFlag {
		fmt.Printf("Version: %s\n", v)
		os.Exit(0)
	}
}

// CurrentVersion returns the current version number in semver format.
func CurrentVersion() string {
	return fmt.Sprintf("%s.%s.%s.%s", versionMajor, versionMinor, versionPatch, versionSHA)
}
