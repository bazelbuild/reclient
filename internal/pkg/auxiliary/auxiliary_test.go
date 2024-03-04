// Copyright 2024 Google LLC
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

package auxiliary

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/bazelbuild/rules_go/go/tools/bazel"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

const (
	// The following consts are field names and message names for the original
	// auxiliary metadata generated from the backend side.
	CPUPeak                     = "cpu_peak_usage"
	CPUAverage                  = "cpu_pct_average"
	MemPeak                     = "mem_peak_usage"
	MemAverage                  = "mem_pct_average"
	Platform                    = "platform"
	Version                     = "version"
	Region                      = "region"
	WorkerUsg                   = "worker_usg"
	BackendAuxiliaryMsgFullName = "WorkerAuxiliaryMetadata"
	BackendUsgMsgFullName       = "WorkerResourceUsage"
	BackendPbFile               = "backend_RBE_specific_api_proto-descriptor-set.proto.bin"

	// Regardless how complex the backend side metadata is, the final log output
	// can be stripped if you mark the unwanted fields to be reserved in this
	// client .pb file.
	ClientPbFile = "client_RBE_specific_api_proto-descriptor-set.proto.bin"
)

func findRunfile(name string) (string, error) {
	runfiles, err := bazel.ListRunfiles()
	if err != nil {
		return "", err
	}
	for _, runfile := range runfiles {
		if strings.HasSuffix(runfile.Path, name) {
			return runfile.Path, nil
		}
	}
	return "", fmt.Errorf("%v not in bazel run files", name)
}

func TestSetMessageDescriptor(t *testing.T) {
	t.Helper()
	clientFile, err := findRunfile(ClientPbFile)
	if err != nil {
		t.Fatalf("Could not find bazel runfile path: %v", err)
	}

	tests := []struct {
		path    string
		wantErr bool
		errMsg  string
	}{
		{path: "", wantErr: true, errMsg: "read .pb file for AuxiliaryMetadata failed"},
		{path: "foo/bar", wantErr: true, errMsg: "read .pb file for AuxiliaryMetadata failed"},
		{path: clientFile, wantErr: false},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			gotErr := SetMessageDescriptor(test.path)
			if test.wantErr {
				if gotErr == nil {
					t.Errorf("SetMessageDescriptor(%v), want error, not got no error", test.path)
				} else if !strings.Contains(gotErr.Error(), test.errMsg) {
					t.Errorf("SetMessageDescriptor(%v), want error %v, got error: %v", test.path, test.errMsg, gotErr)
				}
				if AuxMsgDescriptor != nil {
					t.Errorf("SetMessageDescriptor(%v), want nil descriptor, got descriptor: %v", test.path, AuxMsgDescriptor)
				}
			}
			if !test.wantErr {
				if gotErr != nil {
					t.Errorf("SetMessageDescriptor(%v), want no error, got error: %v", test.path, gotErr)
				}
				if AuxMsgDescriptor == nil {
					t.Errorf("SetMessageDescriptor(%v), want not nil descriptor, got descriptor: %v", test.path, AuxMsgDescriptor)
				}
			}
		})
	}
}

func TestFlatRawMsg(t *testing.T) {
	t.Helper()
	clientFile, err := findRunfile(ClientPbFile)
	if err != nil {
		t.Fatalf("Could not find bazel runfile path: %v", err)
	}
	backendFile, err := findRunfile(BackendPbFile)
	if err != nil {
		t.Fatalf("Could not find bazel runfile path: %v", err)
	}
	// Construct a message protobuf from the backend auxiliary metadata proto.
	// This simulates the protobuf message sent from the RBE service.
	// Note that the RBE backend might send us a very complicated auxiliary
	// metadata, and we only want to collect a subset of its fields.
	auxMsgDescriptor, err := getMessageDescriptor(backendFile, BackendAuxiliaryMsgFullName)
	if err != nil {
		t.Fatalf("getMessageDescriptor() error: %v", err)
	}
	auxMsgType := dynamicpb.NewMessageType(auxMsgDescriptor)
	usgMsgDescriptor, err := getMessageDescriptor(backendFile, BackendUsgMsgFullName)
	if err != nil {
		t.Fatalf("getMessageDescriptor() error: %v", err)
	}
	usgMsgType := dynamicpb.NewMessageType(usgMsgDescriptor)
	cpuPk := usgMsgType.Descriptor().Fields().ByName(CPUPeak)
	memPk := usgMsgType.Descriptor().Fields().ByName(MemPeak)
	cpuAve := usgMsgType.Descriptor().Fields().ByName(CPUAverage)
	memAve := usgMsgType.Descriptor().Fields().ByName(MemAverage)
	usgMsg := usgMsgType.New()
	usgMsg.Set(cpuPk, protoreflect.ValueOfFloat64(0.888))
	usgMsg.Set(memPk, protoreflect.ValueOfFloat64(0.666))
	usgMsg.Set(cpuAve, protoreflect.ValueOfFloat64(0.123))
	usgMsg.Set(memAve, protoreflect.ValueOfFloat64(0.456))
	usage := auxMsgType.Descriptor().Fields().ByName(WorkerUsg)
	platform := auxMsgType.Descriptor().Fields().ByName(Platform)
	version := auxMsgType.Descriptor().Fields().ByName(Version)
	region := auxMsgType.Descriptor().Fields().ByName(Region)
	auxMsg := auxMsgType.New()
	auxMsg.Set(usage, protoreflect.ValueOfMessage(usgMsg))
	auxMsg.Set(platform, protoreflect.ValueOfString("some platform value not going to log in log file"))
	auxMsg.Set(version, protoreflect.ValueOfString("some version value not going to log in log file"))
	auxMsg.Set(region, protoreflect.ValueOfString("some region value not going to log in log file"))

	pbMsg, err := proto.Marshal(auxMsg.Interface())
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}

	// Set client side Message Descriptor. This is the logic how reproxy is going
	// to log the aux pb message obtained from backend at runtime.
	err = SetMessageDescriptor(clientFile)
	if err != nil {
		t.Fatalf("SetMessageDescriptor(%v), want no error, got error: %v", clientFile, err)
	}

	// Flat the raw msg and assert the flattened map.
	// Note that user can pass in their customized AuxMsgDescriptor via reproxy
	// flag --auxiliary_metadata_path, regardless of the actual API backend worker
	// used. Any non reserved field under AuxiliaryMetadata Message will be
	// flattened, and the data will be collected into reproxy log.
	got := FlatRawMsg(pbMsg)
	want := map[string]string{
		"usage.cpuPeak": "0.888",
		"usage.memPeak": "0.666",
		"usage.cpuPct":  "0.123",
		"usage.memPct":  "0.456",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("FlatRawMsghad diff in result: (-want +got)\n%s", diff)
	}
}

func TestFlatJSON(t *testing.T) {
	t.Helper()

	type Student struct {
		Name      string `json:"name"`
		StudentID int    `json:"id"`
		Friends   []int  `json:"friends"`
	}
	type SwimmingClub struct {
		Lead Student `json:"lead"`
	}

	student1 := Student{
		Name:      "Alice",
		StudentID: 1,
		Friends:   []int{3, 4},
	}

	student2 := Student{
		Name:      "Bob",
		StudentID: 2,
		Friends:   []int{5, 6},
	}

	swimmingClub := SwimmingClub{
		Lead: student1,
	}

	tests := []struct {
		input interface{}
		want  map[string]string
	}{
		{input: student1, want: map[string]string{"name": "Alice", "id": "1", "friends": "[3 4]"}},
		{input: swimmingClub, want: map[string]string{"lead.name": "Alice", "lead.id": "1", "lead.friends": "[3 4]"}},
		// This flatJson method is not intended to be used where the input is a list
		// of json objects. In this case, the result will be a map of
		// "" -> "string representation of the flattened list"
		{input: []Student{student1, student2}, want: map[string]string{"": "[map[friends:[3 4] id:1 name:Alice] map[friends:[5 6] id:2 name:Bob]]"}},
	}

	for _, test := range tests {
		jsonBytes, err := json.Marshal(test.input)
		if err != nil {
			t.Fatalf("json.Marshal() error: %v", err)
		}
		var jsonObj interface{}
		err = json.Unmarshal(jsonBytes, &jsonObj)
		if err != nil {
			t.Fatalf("json.Unmarshal() error: %v", err)
		}
		got := flatJSON(jsonObj, "")
		if diff := cmp.Diff(test.want, got); diff != "" {
			t.Fatalf("FlatRawMsghad diff in result: (-want +got)\n%s", diff)
		}
	}
}
