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
	"testing"

	lpb "github.com/bazelbuild/reclient/api/log"
	ppb "github.com/bazelbuild/reclient/api/proxy"
)

func TestMismatchIgnoring(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		inverted    bool
		path        string
		wantIgnored bool
	}{
		{
			name:        "patten matched mismatch ignored",
			pattern:     ".*foo.*",
			inverted:    false,
			path:        "foo.o",
			wantIgnored: true,
		},
		{
			name:        "patten matched inverted mismatch not ignored",
			pattern:     ".*foo.*",
			inverted:    true,
			path:        "foo.o",
			wantIgnored: false,
		},
		{
			name:        "patten not matched mismatch not ignored",
			pattern:     ".*foo.*",
			inverted:    false,
			path:        "bar.o",
			wantIgnored: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := outputFilePathRule{
				spec: &ppb.OutputFilePathRuleSpec{
					PathPattern: &ppb.RegexPattern{
						Expression: test.pattern,
						Inverted:   test.inverted,
					},
				},
			}

			m := &lpb.Verification_Mismatch{
				Path: test.path,
			}

			ignored, err := r.shouldIgnore(m)
			if err != nil {
				t.Fatalf("Error creating mismatch ignorer: %v", err)
			}
			if ignored != test.wantIgnored {
				t.Fatalf("Unexpected rule checking result, expected %v, got %v", test.wantIgnored, ignored)
			}
		})
	}
}
