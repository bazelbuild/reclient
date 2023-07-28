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

// Sample experiment client for connecting to a scandeps service
package main

import (
	"context"
	"flag"
	"os"
	"runtime"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bazelbuild/reclient/internal/pkg/ipc"

	pb "github.com/bazelbuild/reclient/api/scandeps"

	log "github.com/golang/glog"
	"github.com/google/uuid"

	emptypb "google.golang.org/protobuf/types/known/emptypb"
)

var (
	serverAddr  = "127.0.0.1:8001"
	dialTimeout = 3 * time.Minute
)

func main() {
	flag.StringVar(&serverAddr, "server_address", serverAddr, "The server address in the format of host:port for network, or unix:///file for unix domain sockets.")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()
	conn, err := ipc.DialContext(ctx, serverAddr)
	if err != nil {
		log.Fatalf("Fail to dial %s: %v", serverAddr, err)
	}
	defer conn.Close()

	scandeps := pb.NewCPPDepsScannerClient(conn)

	cmd := []string{}
	directory, _ := os.Getwd()
	filename := ""

	switch runtime.GOOS {
	case "darwin":
		log.Fatalf("Scandeps service on Mac is not yet supported")
	case "linux":
		filename = "tests/integ/testdata/test.cpp"
		cmd = append(cmd,
			// Will be downloaded from GCS by run_integ_tests.sh.
			"tests/integ/testdata/clang/bin/clang++",
			"--sysroot", "tests/integ/testdata/sysroot",
			"-c",
			"-I",
			"tests/integ/testdata/clang/include/c++/v1",
			"-o", "test.obj",
			filename,
		)
	case "windows":
		filename = "tests\\integ\\testdata\\test.cpp"
		cmd = append(cmd,
			// Will be downloaded from GCS by prepare_chromium_integ_tests.bat
			"tests\\integ\\testdata\\chromium\\src\\third_party\\llvm-build\\Release+Asserts\\bin\\clang-cl.exe",
			"/nologo",
			"/showIncludes:user",
			"/TP",
			"-fmsc-version=1916",
			"/Brepro",
			"/c",
			"/winsysroottests\\integ\\testdata\\chromium\\src\\third_party\\depot_tools\\win_toolchain\\vs_files\\20d5f2553f",
			"/Fo", "test.obj",
			filename,
		)
	}

	for {
		processResponse, err := scandeps.ProcessInputs(
			context.Background(),
			&pb.CPPProcessInputsRequest{
				ExecId:    uuid.New().String(),
				Command:   cmd,
				Directory: directory,
				Filename:  filename,
			})
		if err != nil {
			if st, ok := status.FromError(err); ok && st.Code() == codes.Unavailable {
				log.Warningf("Disconnected from service during ProcessInputs; retrying after 10 seconds.")
				time.Sleep(10)
				log.Warningf("Retrying ProcessInputs.")
				continue
			}
			log.Fatalf("ProcessInputs command failed: %v", err)
		}
		log.Infof("Dependencies: %v", processResponse.Dependencies)
		log.Infof("Used cache: %t", processResponse.UsedCache)
		break
	}

	for {
		statusResponse, err := scandeps.Status(context.Background(), &emptypb.Empty{})
		if err != nil {
			log.Errorf("Status command failed: %v", err)
			if st, ok := status.FromError(err); ok && st.Code() == codes.Unavailable {
				log.Warningf("Disconnected from service during Status; retrying after 10 seconds.")
				time.Sleep(10)
				log.Warningf("Retrying Status.")
				continue
			}
			log.Fatalf("Status command failed: %v", err)
		}
		log.Infof("Response from %s (v%s)", statusResponse.GetName(), statusResponse.GetVersion())
		if statusResponse.GetUptime() != nil {
			log.Infof("> Uptime: %d seconds", statusResponse.GetUptime().GetSeconds())
		}
		log.Infof("> Completed: %d", statusResponse.GetCompletedActions())
		log.Infof("> In Progress: %d", statusResponse.GetRunningActions())
		if statusResponse.GetLongestCurrentAction() != nil {
			log.Infof("> Longest action: %d", statusResponse.GetLongestCurrentAction().GetSeconds())
		}
		break
	}
	log.Flush()
}
