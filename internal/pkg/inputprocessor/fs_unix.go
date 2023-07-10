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

//go:build !windows && !darwin

package inputprocessor

import (
	"os"
	"path/filepath"
	"strings"
)

type pathNormalizer struct{}

// cross is need for fs_case_insensitive.go, but not used here.
func newPathNormalizer(cross bool) pathNormalizer {
	return pathNormalizer{}
}

func (pathNormalizer) normalize(execRoot, pathname string) (string, error) {
	f := strings.TrimLeft(filepath.Clean(pathname), string(filepath.Separator))
	_, err := os.Stat(filepath.Join(execRoot, f))
	return f, err
}
