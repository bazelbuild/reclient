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

package interceptors

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	ppb "team/foundry-x/re-client/api/proxy"

	cpb "github.com/bazelbuild/remote-apis-sdks/go/api/command"
)

// testing whether truncate interceptor behaves as
// a passthrough for unexpected response and error types
func TestTruncateInterceptorHandleUnexpectedResponseTypes(t *testing.T) {
	tests := []struct {
		resp interface{}
		err  error
	}{
		{resp: nil, err: errors.New("want-error")},
		{resp: 23, err: nil},
		{resp: "foo", err: errors.New("bar")},
		{resp: nil, err: nil},
		{resp: &ppb.RunResponse{ExecutionId: "abc"}, err: nil},
	}

	interceptor := NewTruncInterceptor(102, os.TempDir())

	for _, tc := range tests {
		gotResp, gotErr := interceptor(context.Background(), tc.resp, nil, passThroughHandler(tc.err))
		if gotResp != tc.resp || gotErr != tc.err {
			t.Errorf("Invalid outputs, wanted: (resp: %+v, err: %+v), got: (resp: %+v, err: %+v)",
				tc.resp, tc.err, gotResp, gotErr)
		}
	}
}

// testing whether truncate interceptor truncate response as expected
// with various combinations of field lengths
func TestTruncateInterceptorTruncateResponses(t *testing.T) {
	maxSize := 5 * 1024

	tests := []struct {
		lengths       []int // lengths of Stderr, Stdout, and Result.Msg in RunResponse before the truncate operation
		wantedLengths []int // expected lengths of Stderr, Stdout, and Result.Msg in RunResponse after the truncate operation
	}{
		// lengths are in a following order: (1) Stderr, (2) Stdout, (3) Result.Msg
		{lengths: []int{2000, 3000, 7000}, wantedLengths: []int{2000, 2044, 1024}}, // Stderr should be truncated to min size, then Stdout
		{lengths: []int{4000, 2000, 10}, wantedLengths: []int{4000, 1059, 10}},     // ..but if Stderr is to short, it should be left untouched
		{lengths: []int{6000, 100, 100}, wantedLengths: []int{4870, 100, 100}},     // truncate only Result.Msg
		{lengths: []int{2000, 2000, 5000}, wantedLengths: []int{2000, 2000, 1068}}, // truncate only Stderr
		{lengths: []int{6000, 6000, 6000}, wantedLengths: []int{3020, 1024, 1024}}, // all fields over limit
		{lengths: []int{1500, 1500, 2000}, wantedLengths: []int{1500, 1500, 2000}}, // response smaller than max size (no truncation needed)
		{lengths: []int{0, 6000, 0}, wantedLengths: []int{0, 5075, 0}},             // don't truncate Result.Msg if it's below the limit
	}

	interceptor := NewTruncInterceptor(maxSize, os.TempDir())
	for _, tc := range tests {
		runResp := createResponse(tc.lengths[0], tc.lengths[1], tc.lengths[2])
		_, err := interceptor(context.Background(), runResp, nil, passThroughHandler(nil))
		desc := fmt.Sprintf("len(RunResponse) = [Result.Msg: %d, Stdout: %d, Stderr: %d]", tc.lengths[0], tc.lengths[1], tc.lengths[2])
		if err != nil {
			t.Errorf("%s: got error %q", desc, err)
		}

		if proto.Size(runResp) > maxSize {
			t.Errorf("%s: Invalid response size, wanted <= %d, got: %d", desc, maxSize, proto.Size(runResp))
		}
		gotLengths := []int{len(runResp.Result.Msg), len(runResp.Stdout), len(runResp.Stderr)}
		if !reflect.DeepEqual(tc.wantedLengths, gotLengths) {
			t.Errorf("%s: Invalid field lengths, wanted: %v, got: %v", desc, tc.wantedLengths, gotLengths)
		}
	}
}

func createResponse(msgLen, outLen, errLen int) *ppb.RunResponse {
	return &ppb.RunResponse{
		ExecutionId: "df989d09-a028-4286-964d-385ff2ca5c57",
		Result: &cpb.CommandResult{
			ExitCode: 0,
			Status:   1,
			Msg:      string(createBytesSlice(msgLen)),
		},
		Stdout: createBytesSlice(outLen),
		Stderr: createBytesSlice(errLen),
	}
}

func passThroughHandler(err error) grpc.UnaryHandler {
	return func(_ context.Context, req interface{}) (interface{}, error) {
		return req, err
	}
}

func createBytesSlice(length int) []byte {
	text := "foobar"
	resp := make([]byte, length)
	for i := 0; i < length; i++ {
		resp[i] = byte(text[i%len(text)])
	}
	return resp
}
