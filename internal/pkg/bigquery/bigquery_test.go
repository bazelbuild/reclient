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

package bigquery

import "testing"

func TestParseResourceSpec(t *testing.T) {
	tests := []struct {
		name                                string
		spec                                string
		defaultProject                      string
		wantProject, wantDataset, wantTable string
		wantErr                             bool
	}{
		{
			name:           "FullSpec",
			spec:           "projectA:dataset123.some-table",
			defaultProject: "anotherProject",
			wantProject:    "projectA",
			wantDataset:    "dataset123",
			wantTable:      "some-table",
		},
		{
			name:           "UseDefaultProject",
			spec:           "dataset123.some-table",
			defaultProject: "anotherProject",
			wantProject:    "anotherProject",
			wantDataset:    "dataset123",
			wantTable:      "some-table",
		},
		{
			name:           "PeriodInsteadOfColon",
			spec:           "projectA.dataset123.some-table",
			defaultProject: "anotherProject",
			wantErr:        true,
		},
		{
			name:           "MissingTable",
			spec:           "projectA:dataset123",
			defaultProject: "anotherProject",
			wantErr:        true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotProject, gotDataset, gotTable, err := parseResourceSpec(tc.spec, tc.defaultProject)
			if tc.wantErr {
				if err == nil {
					t.Errorf(
						"parseResourceSpec(%v,%v) expected to return error but instead returned: project=%v, dataset=%v, table=%v",
						tc.spec, tc.defaultProject, gotProject, gotDataset, gotTable)
				}
				return
			}
			if err != nil {
				t.Errorf("parseResourceSpec(%v,%v) returned unexpected error: %v", tc.spec, tc.defaultProject, err)
			} else if gotProject != tc.wantProject || gotDataset != tc.wantDataset || gotTable != tc.wantTable {
				t.Errorf(
					"parseResourceSpec(%v,%v) expected to return (project=%v, dataset=%v, table=%v), got (project=%v, dataset=%v, table=%v)",
					tc.spec, tc.defaultProject, tc.wantProject, tc.wantDataset, tc.wantTable, gotProject, gotDataset, gotTable)
			}
		})
	}
}
