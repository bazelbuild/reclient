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

package main

import (
	"context"
	"flag"
	"log"
	"os"

	"google.golang.org/grpc"

	cpb "github.com/bazelbuild/remote-apis-sdks/go/api/command"
	ppb "team/foundry-x/re-client/api/proxy"
)

var (
	serverAddr = flag.String("server_addr", "127.0.0.1:8000", "The server address in the format of host:port")
)

func main() {
	flag.Parse()
	opts := []grpc.DialOption{grpc.WithInsecure()}
	conn, err := grpc.Dial(*serverAddr, opts...)
	if err != nil {
		log.Fatalf("Fail to dial: %v", err)
	}
	defer conn.Close()

	client := ppb.NewCommandsClient(conn)
	ctx := context.Background()
	wd, _ := os.Getwd()
	res, err := client.RunCommand(ctx, &ppb.RunRequest{
		Labels: map[string]string{"type": "compile", "lang": "cpp", "compiler": "clang"},
		Command: &cpb.Command{
			Args:     []string{"clang", "-c", "-o", "test.o", "examples/proxyclient/test.cpp"},
			ExecRoot: wd,
			Platform: map[string]string{
				"container-image": "docker://gcr.io/foundry-x-experiments/proxy-test@sha256:af0af0a78b2766398b1789c01d1665547b97246b7adc4848df5ebbfa21fab490",
				"jdk-version":     "10",
			},
		},
		ExecutionOptions: &ppb.ProxyExecutionOptions{ExecutionStrategy: ppb.ExecutionStrategy_REMOTE},
	})
	if err != nil {
		log.Fatalf("Failed to run a command: %v", err)
	}
	log.Printf("Res: %+v\n", res)
}
