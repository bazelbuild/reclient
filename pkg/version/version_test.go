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

package version

import (
	"regexp"
	"strings"
	"testing"
)

const (
	undef = "undefined"
)

var (
	expectedVersionFormat = regexp.MustCompile("(\\d+)\\.(\\d+)\\.(\\d+)\\.(([a-z0-9])+)")
)

func TestVersionIsDefined(t *testing.T) {
	if v := CurrentVersion(); strings.Contains(v, undef) {
		t.Fatalf("CurrentVersion()=%v contains %q, expected a git commit sha instead", v, undef)
	}
}

func TestVersionInExpectedFormat(t *testing.T) {
	matches := expectedVersionFormat.FindAllString(CurrentVersion(), -1)
	if len(matches) != 1 {
		t.Fatalf("CurrentVersion()=%v not in expected format, match=%v, want %d matches, got %d", CurrentVersion(), matches, 1, len(matches))
	}
}
