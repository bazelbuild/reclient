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

//go:build linux

package reproxy_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/bazelbuild/rules_go/go/tools/bazel"
)

func TestDynamicDep(t *testing.T) {
	reproxyBin, ok := bazel.FindBinary("cmd/reproxy", "reproxy")
	if !ok {
		t.Fatalf("reproxy binary not found")
	}
	cmd := exec.Command("ldd", reproxyBin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to run ldd %s: %v\n%s", reproxyBin, err, string(out))
	}
	if strings.Contains(string(out), "libstdc++.so") {
		t.Errorf("reproxy depends on libstdc++.so\n%s", string(out))
	}
}
