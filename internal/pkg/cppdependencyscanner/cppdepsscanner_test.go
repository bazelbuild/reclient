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

// Package cppdependencyscanner encapsulates a concrete include scanner.
// It can either encapsulate clang-scan-deps or goma's input processor
// depending on build configuration.
// If specified as an argument it will alternatively connect to a remote scanner service.
package cppdependencyscanner

import (
	"context"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"

	pb "github.com/bazelbuild/reclient/api/scandeps"
	"github.com/bazelbuild/reclient/internal/pkg/ipc"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/emptypb"
)

func genRandomUDSAddress() string {
	switch runtime.GOOS {
	case "windows":
		return fmt.Sprintf("pipe://re-client-%s.ipc", uuid.New().String())
	default:
		return fmt.Sprintf("unix:///tmp/%s.sock", uuid.New().String())
	}
}

func TestConnect(t *testing.T) {
	type testCase struct {
		name     string
		err      error
		errCnt   int32
		want     *pb.CapabilitiesResponse
		wantCode codes.Code
	}
	tests := []testCase{
		{
			name: "success",
			want: &pb.CapabilitiesResponse{
				Caching:            true,
				ExpectsResourceDir: true,
			},
		}, {
			name: "retry_code_ctx_timeout_twice_then_success",
			want: &pb.CapabilitiesResponse{
				Caching:            true,
				ExpectsResourceDir: true,
			},
			err:    fmt.Errorf("wrapped %w", context.DeadlineExceeded),
			errCnt: 2,
		}, {
			name: "error_after_too_many_retries",
			want: &pb.CapabilitiesResponse{
				Caching:            true,
				ExpectsResourceDir: true,
			},
			err:      context.DeadlineExceeded,
			errCnt:   11,
			wantCode: codes.DeadlineExceeded,
		}, {
			name: "error_after_internal_error",
			want: &pb.CapabilitiesResponse{
				Caching:            true,
				ExpectsResourceDir: true,
			},
			err:      status.Error(codes.Internal, "boom"),
			errCnt:   1,
			wantCode: codes.Internal,
		},
	}
	for _, code := range []codes.Code{codes.Canceled, codes.Unknown, codes.DeadlineExceeded, codes.Unavailable, codes.ResourceExhausted} {
		tests = append(tests,
			testCase{
				name: fmt.Sprintf("retry_code_%s_twice_then_success", code.String()),
				want: &pb.CapabilitiesResponse{
					Caching:            true,
					ExpectsResourceDir: true,
				},
				err:    status.Error(code, "boom"),
				errCnt: 2,
			})
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			address := genRandomUDSAddress()
			listener, err := ipc.Listen(address)
			if err != nil {
				t.Fatalf("Unable to listen on %q: %v", address, err)
			}
			defer listener.Close()
			service := &stubService{
				capabilities:       tc.want,
				capabilitiesErr:    tc.err,
				capabilitiesErrCnt: atomic.Int32{},
			}
			service.capabilitiesErrCnt.Store(tc.errCnt)
			server := grpc.NewServer()
			pb.RegisterCPPDepsScannerServer(server, service)
			go func() {
				server.Serve(listener)
			}()
			defer server.GracefulStop()
			_, got, err := connect(ctx, address)
			if err != nil && tc.wantCode == codes.OK {
				t.Errorf("connect(%q) returned an unexpected error: %v", address, err)
			} else if st, ok := status.FromError(err); ok && st.Code() != tc.wantCode {
				t.Errorf("connect(%q) returned an unexpected code %s, wanted %s", address, st.Code().String(), tc.wantCode.String())
			} else if err == nil && tc.wantCode == codes.OK && !cmp.Equal(got, tc.want, protocmp.Transform()) {
				t.Errorf("connect(%q) = %v, want %v", address, got, tc.want)
			}
		})
	}
}

type stubService struct {
	capabilities       *pb.CapabilitiesResponse
	capabilitiesErr    error
	capabilitiesErrCnt atomic.Int32
}

func (s *stubService) Capabilities(ctx context.Context, in *emptypb.Empty) (*pb.CapabilitiesResponse, error) {
	if s.capabilitiesErrCnt.Add(-1) < 0 {
		return s.capabilities, nil
	}
	return nil, s.capabilitiesErr
}

func (s *stubService) Shutdown(ctx context.Context, in *emptypb.Empty) (*pb.StatusResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *stubService) Status(ctx context.Context, in *emptypb.Empty) (*pb.StatusResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *stubService) ProcessInputs(ctx context.Context, in *pb.CPPProcessInputsRequest) (*pb.CPPProcessInputsResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
