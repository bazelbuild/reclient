// Copyright 2024 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package logrecordserver

import (
	"testing"
)

func TestLoadLogRecords(t *testing.T) {
	testFilePath := "text://testdata/test.rrpl"

	s := &Server{}
	s.LoadLogRecords(testFilePath)
	if s.loaded() != true {
		t.Errorf("LoadLogRecords() failed: %v", s.loadError())
	}
	want := 2
	if len(s.records) != want {
		t.Errorf("LoadLogRecords() loaded incorrect number of log records, want=%v, got=%v", want, len(s.records))
	}
}
