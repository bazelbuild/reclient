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

// Package ipc provides IPC functionality between rewrapper(client) and reproxy(server).
package ipc

const (
	// GrpcMaxMsgSize is the max value of gRPC response that can be received by the client (in bytes)
	GrpcMaxMsgSize = 1024 * 1024 * 32 // 32MB (default is 4MB)

	// GrpcMaxListenSize is the max value of the gRPC response that can be listened for by the proxy.
	// This is message size received from rewrapper.
	// Limiting this to a smaller value than reception from RBE due to performance issues on intel macs
	// when this is set to 32MB.
	GrpcMaxListenSize = 1024 * 1024 * 8
)
