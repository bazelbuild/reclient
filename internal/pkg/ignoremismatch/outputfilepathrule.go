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
	"fmt"
	"regexp"

	lpb "github.com/bazelbuild/reclient/api/log"
	ppb "github.com/bazelbuild/reclient/api/proxy"
)

type outputFilePathRule struct {
	spec *ppb.OutputFilePathRuleSpec
}

func (r outputFilePathRule) shouldIgnore(mismatch *lpb.Verification_Mismatch) (bool, error) {
	re, err := regexp.Compile(r.spec.GetPathPattern().Expression)
	if err != nil {
		return false, fmt.Errorf("invalid regular expression %v", r.spec.GetPathPattern().Expression)
	}
	matched := re.MatchString(mismatch.Path)
	return r.spec.PathPattern.Inverted != matched, nil
}
