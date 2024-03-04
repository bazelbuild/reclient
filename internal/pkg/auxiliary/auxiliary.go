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
	"os"

	log "github.com/golang/glog"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// FullName is the Auxiliary Metadata message descriptor's full name.
// See Remote API: https://github.com/bazelbuild/remote-apis/blob/7f51b3676b5d19df726783d9a861e45d7389b5ae/build/bazel/remote/execution/v2/remote_execution.proto#L1046
// It's worth noting that the backend can give this proto message any arbitrary
// name; however, the client side proto message should strictly use
// `AuxiliaryMetadata` to receive it. For example, in the unit test, the backend
// send the proto msg out with name `WorkerAuxiliaryMetadata`, and client
// receives it as `AuxiliaryMetadata`.
const FullName = "AuxiliaryMetadata"

// AuxMsgDescriptor is the message descriptor for Auxiliary Metadata.
// This variable should be set via SetMessageDescriptor() in reproxy before
// logger is initialed.
var AuxMsgDescriptor *protoreflect.MessageDescriptor

func getMessageDescriptor(path string, msgFullName string) (protoreflect.MessageDescriptor, error) {
	protoFile, err := os.ReadFile(path)
	if err != nil {
		cwd, _ := os.Getwd()
		return nil, fmt.Errorf("read .pb file for %s failed.\ncwd: %s,\nfile path: %s,\nerror: %v", FullName, cwd, path, err)
	}
	fdPbs := &descriptorpb.FileDescriptorSet{}
	if err = proto.Unmarshal(protoFile, fdPbs); err != nil {
		return nil, fmt.Errorf("unmarshal %v to protobuf failed: %v", path, err)
	}
	fdSets := fdPbs.GetFile()
	if len(fdSets) < 1 {
		return nil, fmt.Errorf("no descriptor protobuf found in file: %v", path)
	}
	fdPb := fdSets[0]
	fd, err := protodesc.NewFile(fdPb, protoregistry.GlobalFiles)
	if err != nil {
		return nil, fmt.Errorf("register %v as file descriptor failed: %v", path, err)
	}
	md := fd.Messages().ByName(protoreflect.Name(msgFullName))
	return md, nil
}

// SetMessageDescriptor reads a .pb file for an auxiliary metadata, parse the file
// to a message descriptor, and returns the message descriptor.
func SetMessageDescriptor(path string) error {
	md, err := getMessageDescriptor(path, FullName)
	if err != nil {
		return err
	}
	AuxMsgDescriptor = &md
	return nil
}

// FlatRawMsg decode raw bytes of a protobuf message according to the given
// message descriptor, returns a flatted map representation of the message.
func FlatRawMsg(rawMsg []byte) map[string]string {
	msg := dynamicpb.NewMessage(*AuxMsgDescriptor)
	proto.Unmarshal(rawMsg, msg)
	return flatJSON(msgToJSON(msg), "")
}

// msgToJSON converts a dynamic type protobuf message into a json object.
func msgToJSON(msg *dynamicpb.Message) interface{} {
	jsonBytes, err := protojson.Marshal(msg)
	if err != nil {
		log.Errorf("error marshalling dynamicpb message to JSON bytes: %v", err)
		return nil
	}
	var jsonObj interface{}
	err = json.Unmarshal(jsonBytes, &jsonObj)
	if err != nil {
		log.Errorf("error unmarshalling to JSON bytes to Json object: %v", err)
		return nil
	}
	return jsonObj
}

// flatJSON flatten a JSON object into a one dimensional map.
// For example, given a JSON object:
//
//	{
//	  "a": 1,
//	  "b": {
//	    "c": 2,
//	    "d": 3
//	  }
//	}
//
// flatJSON will return:
//
//	{
//	  "a": "1",
//	  "b.c": "2",
//	  "b.d": "3"
//	}
//
// It's worth noting that a json object can either be 1) a collection of
// key/value pairs, or 2) an ordered list of values.
// This function is intended to only work with the first case. If one try to
// flat a json object which is a list of values, the result will be a map of one
// entry, where the key is "", and the value is the string representation of
// the flattened list (see the associated unit test for more details).
func flatJSON(data interface{}, prefix string) map[string]string {
	result := make(map[string]string)
	switch value := data.(type) {
	case map[string]interface{}:
		for k, v := range value {
			newPrefix := k
			if prefix != "" {
				newPrefix = prefix + "." + k
			}
			for innerKey, innerValue := range flatJSON(v, newPrefix) {
				result[innerKey] = innerValue
			}
		}
	default:
		result[prefix] = fmt.Sprintf("%v", value)
	}
	return result
}
