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

	lpb "github.com/bazelbuild/reclient/api/log"
	ppb "github.com/bazelbuild/reclient/api/proxy"
)

type ignoreRule interface {
	shouldIgnore(mismatch *lpb.Verification_Mismatch) (bool, error)
}

func createFromRuleProto(rpb *ppb.Rule) (ignoreRule, error) {
	switch rpb.RuleSpec.(type) {
	case *ppb.Rule_OutputFilePathRuleSpec:
		return outputFilePathRule{
			spec: rpb.GetOutputFilePathRuleSpec(),
		}, nil
	}
	return nil, fmt.Errorf("couldn't find a rule implementation for rule spec %v", rpb.GetRuleSpec())
}
