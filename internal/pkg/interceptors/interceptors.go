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

// Package interceptors contains gRPC server interceptors used to track
// the timestamp of the most recent request.
package interceptors

import (
	"context"
	"sync"
	"time"

	"google.golang.org/grpc"
)

var (
	latestRequestTimestamp = time.Now()
	mu                     sync.Mutex
)

// LatestRequestTimestamp returns the most recent timestamp
// at which a unary / stream RPC was received.
func LatestRequestTimestamp() time.Time {
	mu.Lock()
	defer mu.Unlock()
	return latestRequestTimestamp
}

// UnaryServerInterceptor intercepts unary RPCs and keeps track of the latest request timestamp.
func UnaryServerInterceptor(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	mu.Lock()
	latestRequestTimestamp = max(latestRequestTimestamp, time.Now())
	mu.Unlock()
	return handler(ctx, req)
}

// StreamServerInterceptor intercepts stream RPCs and keeps track of the latest request timestamp.
func StreamServerInterceptor(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	mu.Lock()
	latestRequestTimestamp = max(latestRequestTimestamp, time.Now())
	mu.Unlock()
	return handler(srv, ss)
}

func max(a, b time.Time) time.Time {
	if a.Before(b) {
		return b
	}
	return a
}
