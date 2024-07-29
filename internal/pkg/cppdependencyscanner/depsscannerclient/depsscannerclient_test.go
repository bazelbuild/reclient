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

package depsscannerclient

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/bazelbuild/reclient/api/scandeps"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/outerr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

// A stub Executor class that records actions but does not actually execute a command.
type stubExecutor struct {
	// The error (if non-nil) to return from ExecuteInBackground().
	err error
	// Output: the Context passed to ExecuteInBackground().
	ctx context.Context
	// Output: the Command passed to ExecuteInBackground().
	cmd *command.Command
	// output: the OutErr passed to ExecuteInBackground().
	oe outerr.OutErr
	// output: the channel passed to ExecuteInBackground().
	ch chan *command.Result
}

// An object for returning preset values for certain calls during New().
type testService struct {
	// A fake gRPC client to return from connect.
	stubClient *stubClient
	// A fake capabilities response
	capabilities *pb.CapabilitiesResponse
	// The number of times connect should fail before returning stubClient.
	connectDelay time.Time
	// Output: the number of times connect() was called.
	connectCount atomic.Int64
}

// connect returns a preset stubClient.
func (s *testService) connect(ctx context.Context, address string) (pb.CPPDepsScannerClient, *pb.CapabilitiesResponse, error) {
	s.connectCount.Add(1)
	select {
	case <-time.After(time.Until(s.connectDelay)):
		// Sleep, simulate a slow connection that may or may not timeout
	case <-ctx.Done():
		return nil, nil, errors.New("Connection timed out")
	}
	if s.stubClient != nil {
		return s.stubClient, s.capabilities, nil
	}
	return nil, nil, errors.New("Connection not ready yet")
}

// A stub CPPDepsScannerClient.
type stubClient struct {
	// The response to return from ProcessInputs().
	processInputsResponse *pb.CPPProcessInputsResponse
	// The error to return from ProcessInputs().
	processInputsError error
	// A delay to simulate work being done by ProcessInputs().
	processInputsSleep time.Duration
	// The "Status" returned from a Status call or Shutdown call.
	status *pb.StatusResponse
	// The error to return from a Shutdown call.
	shutdownError error
	// The number of times Shutdown has been called.
	shutdownCalled int
	// The response to return from Capabilities().
	capabilitiesResponse *pb.CapabilitiesResponse
	// The error to return from Capabilities().
	capabilitiesError error
}

// Fake ProcessInputs that returns a preset value with optional delay to emulate processing time.
func (c *stubClient) ProcessInputs(ctx context.Context, in *pb.CPPProcessInputsRequest, opts ...grpc.CallOption) (*pb.CPPProcessInputsResponse, error) {
	select {
	case <-time.After(c.processInputsSleep):
		// Sleep
	case <-ctx.Done():
		return nil, errors.New("timeout")
	}
	return c.processInputsResponse, c.processInputsError
}

func (c *stubClient) Status(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*pb.StatusResponse, error) {
	return nil, nil
}

// Fake Shutdown that records a Shutdown attempt but does not actually shutdown anything.
func (c *stubClient) Shutdown(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*pb.StatusResponse, error) {
	c.shutdownCalled++
	return c.status, c.shutdownError
}

func (c *stubClient) Capabilities(ctx context.Context, in *emptypb.Empty, opts ...grpc.CallOption) (*pb.CapabilitiesResponse, error) {
	return c.capabilitiesResponse, c.capabilitiesError
}

// Fake Execution that performs a setup but does not actually run the command.
func (e *stubExecutor) ExecuteInBackground(ctx context.Context, cmd *command.Command, oe outerr.OutErr, ch chan *command.Result) error {
	e.ctx = ctx
	e.cmd = cmd
	e.oe = oe
	e.ch = ch
	return e.err
}

var (
	directory, _ = os.Getwd()
	filename     = "tests/integ/testdata/test.cpp"
	// This command will not actually be used by any input processor, it is merely a prop.
	compileCommand = []string{
		"tests/integ/testdata/clang/bin/clang++",
		"--sysroot", "tests/integ/testdata/sysroot",
		"-c",
		"-I",
		"tests/integ/testdata/clang/include/c++/v1",
		"-o", "test.obj",
		filename,
	}
	processInputsTimeout = 100 * time.Millisecond
)

// TestNew_ConnectSuccess tests that a call to New() can connect to an already running dependency
// scanner service.
func TestNew_ConnectSuccess(t *testing.T) {
	t.Parallel()
	testService := &testService{
		stubClient: &stubClient{},
	}
	depsScannerClient, err := New(context.Background(), nil, "", 0, false, "", "127.0.0.1:8001", "127.0.0.1:1000", 30*time.Second, testService.connect)
	if err != nil {
		t.Errorf("New() retured unexpected error: %v", err)
	}
	if testService.connectCount.Load() != 1 {
		t.Errorf("New(); expected 1 connection attempt, got %v", testService.connectCount.Load())
	}
	if depsScannerClient == nil {
		t.Error("New(): Expected DepsScannerClient; got nil")
	}
}

// TestNew_ConnectFailure tests that a call to New() will fail if no dependency scanner service is
// running when expected.
func TestNew_ConnectFailure(t *testing.T) {
	t.Parallel()
	testService := &testService{
		stubClient:   nil,
		connectDelay: time.Now().Add(5 * time.Second), // Wait for 5 seconds before "accepting" the connection
	}
	depsScannerClient, err := New(context.Background(), nil, "", 0, false, "", "127.0.0.1:8001", "127.0.0.1:1000", 500*time.Millisecond, testService.connect)
	if err == nil {
		t.Errorf("New() did not return expected error")
	}
	// Windows and mac runs are inconsistent with exactly how many attempts fit in 500ms
	if testService.connectCount.Load() != 1 {
		t.Errorf("New(): expected 1 connection attempt, got %v", testService.connectCount.Load())
	}
	if depsScannerClient != nil {
		t.Error("New(): Returned DepsScannerClient; expected nil")
	}
}

// TestNew_StartSuccess tests that a call to New() will start and connect to a dependency scanner
// service executable.
func TestNew_StartSuccess(t *testing.T) {
	t.Parallel()
	fakeCapabilities := &pb.CapabilitiesResponse{
		Caching:            false,
		ExpectsResourceDir: true,
	}
	testService := &testService{
		stubClient:   &stubClient{},
		capabilities: fakeCapabilities,
	}
	stubExecutor := &stubExecutor{}
	depsScannerClient, err := New(context.Background(), stubExecutor, "", 0, false, "", "exec://test_exec", "127.0.0.1:1000", 30*time.Second, testService.connect)
	if err != nil {
		t.Errorf("New() retured unexpected error: %v", err)
	}
	if testService.connectCount.Load() != 1 {
		t.Errorf("New(); expected 1 connection attempt, got %v", testService.connectCount.Load())
	}
	if depsScannerClient == nil {
		t.Error("New(): Expected DepsScannerClient; got nil")
	}
	if stubExecutor.oe == nil {
		t.Error("New(): Executor did not get an output")
	}
	if stubExecutor.ch == nil {
		t.Error("New(): Executor did not get a channel")
	}
	if stubExecutor.cmd == nil {
		t.Error("New(): Executor did not get a cmd")
	}
	if !strings.HasPrefix(depsScannerClient.address, "127.0.0.1:") {
		t.Errorf("New(): Connected to %v; expected prefix %v", depsScannerClient.address, "127.0.0.1:")
	}
	if depsScannerClient.executable != "test_exec" {
		t.Errorf("New(): Executed %v; expected %v", depsScannerClient.executable, "test_exec")
	}
	select {
	case <-stubExecutor.ctx.Done():
		t.Error("New(): Unexpected Cancel() call")
	default:
		// No Cancel() call. Expected.
	}
	if got := depsScannerClient.Capabilities(); got != fakeCapabilities {
		t.Errorf("Capabilities() returned unexpected value, wanted %v, got %v", fakeCapabilities, got)
	}
}

// TestNew_StartFailure tests that a call to New() will fail if an executable is provided but cannot
// be started.
func TestNew_StartFailure(t *testing.T) {
	t.Parallel()
	testService := &testService{
		stubClient: &stubClient{},
	}
	stubExecutor := &stubExecutor{
		err: errors.New("File not found"),
	}
	depsScannerClient, err := New(context.Background(), stubExecutor, "", 0, false, "", "exec://test_exec", "127.0.0.1:1000", 30*time.Second, testService.connect)
	if err == nil {
		t.Errorf("New() did not return expected error")
	}
	if testService.connectCount.Load() != 0 {
		t.Errorf("New(); expected 0 connection attempts, got %v", testService.connectCount.Load())
	}
	if depsScannerClient != nil {
		t.Error("New(): Received DepsScannerClient; expected nil")
	}
	if stubExecutor.oe == nil {
		t.Error("New(): Executor did not get an output")
	}
	if stubExecutor.ch == nil {
		t.Error("New(): Executor did not get a channel")
	}
	if stubExecutor.cmd == nil {
		t.Error("New(): Executor did not get a cmd")
	}
	select {
	case <-stubExecutor.ctx.Done():
		t.Error("New(): Unexpected Cancel() call")
	default:
		// No Cancel() call. Expected.
	}
}

// TestNew_StartNoConnect tests that New() will error if it is able to successfully start a
// dependency scanner service, but is unable to connect to it for any reason.
func TestNew_StartNoConnect(t *testing.T) {
	t.Parallel()
	testService := &testService{
		connectDelay: time.Now().Add(5 * time.Second), // Wait for 5 seconds before "accepting" the connection
	}
	stubExecutor := &stubExecutor{}
	depsScannerClient, err := New(context.Background(), stubExecutor, "", 0, false, "", "exec://test_exec", "127.0.0.1:1000", 500*time.Millisecond, testService.connect)
	if err == nil {
		t.Errorf("New() did not return expected error")
	}
	// Windows and mac runs are inconsistent with exactly how many attempts fit in 500ms
	if testService.connectCount.Load() != 1 {
		t.Errorf("New(): expected 1 connection attempt, got %v", testService.connectCount.Load())
	}
	if depsScannerClient != nil {
		t.Error("New(): DepsScannerClient returned; expected nil")
	}
	if stubExecutor.oe == nil {
		t.Error("New(): Executor did not get an output")
	}
	if stubExecutor.ch == nil {
		t.Error("New(): Executor did not get a channel")
	}
	if stubExecutor.cmd == nil {
		t.Error("New(): Executor did not get a cmd")
	}
}

// TestNew_StartDelayedConnect tests that a call to New() will be successful if the started
// dependency scanner service takes a little while (more than 5 seconds, less than 15) to become
// available.
func TestNew_StartDelayedConnect(t *testing.T) {
	t.Parallel()
	testService := &testService{
		stubClient:   &stubClient{},
		connectDelay: time.Now().Add(5 * time.Second), // Wait for 5 seconds before "accepting" the connection
	}
	stubExecutor := &stubExecutor{}
	depsScannerClient, err := New(context.Background(), stubExecutor, "", 0, false, "", "exec://test_exec", "127.0.0.1:1000", 30*time.Second, testService.connect)
	if err != nil {
		t.Errorf("New() retured unexpected error: %v", err)
	}
	if testService.connectCount.Load() != 1 {
		t.Errorf("New(); expected 1 connection attempt, got %v", testService.connectCount.Load())
	}
	if depsScannerClient == nil {
		t.Error("New(): Expected DepsScannerClient; got nil")
	}
	if stubExecutor.oe == nil {
		t.Error("New(): Executor did not get an output")
	}
	if stubExecutor.ch == nil {
		t.Error("New(): Executor did not get a channel")
	}
	if stubExecutor.cmd == nil {
		t.Error("New(): Executor did not get a cmd")
	}
	select {
	case <-stubExecutor.ctx.Done():
		t.Error("New(): Unexpected Cancel() call")
	default:
		// No Cancel() call. Expected.
	}
}

// TestStopService_Success tests that a call to stopService (trivially called by the exported
// Close() function) will properly stop the service.
func TestStopService_Success(t *testing.T) {
	t.Parallel()
	terminateCalled := 0
	stubClient := &stubClient{}
	depsScannerClient := &DepsScannerClient{
		address: "127.0.0.1:8001",
		ctx:     context.Background(),
		terminate: func() {
			terminateCalled++
		},
		executable: "test_exec",
		oe:         outerr.NewRecordingOutErr(),
		ch:         make(chan *command.Result),
		client:     stubClient,
	}
	var wg sync.WaitGroup
	var err error
	wg.Add(1)
	go func() {
		defer wg.Done()
		err = depsScannerClient.stopService(1 * time.Second)
	}()

	select {
	case depsScannerClient.ch <- command.NewResultFromExitCode(0):
		// Succesfully "stopped" the service.
	case <-time.After(1 * time.Second):
		t.Error("stopService was not waiting for shutdown")
	}

	wg.Wait()
	if err != nil {
		t.Errorf("Unexpected error shutting down service: %v", err)
	}
	if stubClient.shutdownCalled != 1 {
		t.Errorf("Expected Shutdown to be called exactly once; called %v times", stubClient.shutdownCalled)
	}
	if terminateCalled != 0 {
		t.Errorf("Terminate called %v times; expected 0 (clean shutdown)", terminateCalled)
	}
}

// TestStopService_Failure tests that stopService (trivially called by the exported Close()
// function) will error if it was unable to verify the service had been shutdown after a fixed
// timeout.
func TestStopService_Failure(t *testing.T) {
	t.Parallel()
	terminateCalled := 0
	stubClient := &stubClient{}
	depsScannerClient := &DepsScannerClient{
		address: "127.0.0.1:8001",
		ctx:     context.Background(),
		terminate: func() {
			terminateCalled++
		},
		executable: "test_exec",
		oe:         outerr.NewRecordingOutErr(),
		ch:         make(chan *command.Result),
		client:     stubClient,
	}
	var wg sync.WaitGroup
	var err error
	wg.Add(1)
	go func() {
		defer wg.Done()
		err = depsScannerClient.stopService(1 * time.Second)
	}()

	wg.Wait()
	if err == nil {
		t.Error("Error expected while shutting down; received none")
	}
	if stubClient.shutdownCalled != 1 {
		t.Errorf("Expected Shutdown to be called exactly once; called %v times", stubClient.shutdownCalled)
	}
	if terminateCalled != 1 {
		t.Errorf("Terminate called %v times; expected 1 (forced shutdown)", terminateCalled)
	}
}

func TestProcessInputs_nocache(t *testing.T) {
	t.Parallel()
	execID := uuid.New().String()
	fileList := []string{
		filename,
		"foo.h",
		"bar.h",
	}
	wantDeps := []string{}
	for _, d := range fileList {
		wantDeps = append(wantDeps, filepath.Join(directory, d))
	}
	stubClient := &stubClient{
		processInputsResponse: &pb.CPPProcessInputsResponse{
			// Set values to verify they are not returned on error
			Dependencies: fileList,
			UsedCache:    false,
		},
	}

	client := &DepsScannerClient{
		ctx:    context.Background(),
		client: stubClient,
	}
	gotDeps, cached, err := client.ProcessInputs(context.Background(), execID, compileCommand, filename, directory, []string{})
	if err != nil {
		t.Errorf("ProcessInputs failed: %v", err)
	}
	if cached != false {
		t.Errorf("ProcessInputs UsedCache == true; expected false")
	}
	if !cmp.Equal(wantDeps, gotDeps) {
		t.Errorf("ProcessInputs(%q)=%q; want %q\ndiff -want +got\n%s", filename, gotDeps, wantDeps, cmp.Diff(wantDeps, gotDeps))
	}
}

func TestProcessInputs_abspath(t *testing.T) {
	t.Parallel()
	execID := uuid.New().String()
	fileList := []string{
		filename,
		"foo.h",
		"bar.h",
	}
	wantDeps := []string{}
	for _, d := range fileList {
		wantDeps = append(wantDeps, filepath.Join(directory, d))
	}
	absFile := "/path/to/some/file.h"
	if runtime.GOOS == "windows" {
		absFile = filepath.Join("C:/", absFile)
	}
	fileList = append(fileList, absFile)
	wantDeps = append(wantDeps, absFile)

	stubClient := &stubClient{
		processInputsResponse: &pb.CPPProcessInputsResponse{
			// Set values to verify they are not returned on error
			Dependencies: fileList,
		},
	}

	client := &DepsScannerClient{
		ctx:    context.Background(),
		client: stubClient,
	}

	gotDeps, _, err := client.ProcessInputs(context.Background(), execID, compileCommand, filename, directory, []string{})
	if err != nil {
		t.Errorf("ProcessInputs failed: %v", err)
	}
	if !cmp.Equal(wantDeps, gotDeps) {
		t.Errorf("ProcessInputs(%q)=%q; want %q\ndiff -want +got\n%s", filename, gotDeps, wantDeps, cmp.Diff(wantDeps, gotDeps))
	}
}

func TestProcessInputs_cache(t *testing.T) {
	t.Parallel()
	execID := uuid.New().String()
	fileList := []string{
		filename,
		"foo.h",
		"bar.h",
	}
	wantDeps := []string{}
	for _, d := range fileList {
		wantDeps = append(wantDeps, filepath.Join(directory, d))
	}
	stubClient := &stubClient{
		processInputsResponse: &pb.CPPProcessInputsResponse{
			// Set values to verify they are not returned on error
			Dependencies: fileList,
			UsedCache:    true,
		},
	}

	client := &DepsScannerClient{
		ctx:    context.Background(),
		client: stubClient,
	}

	gotDeps, cached, err := client.ProcessInputs(context.Background(), execID, compileCommand, filename, directory, []string{})
	if err != nil {
		t.Errorf("ProcessInputs failed: %v", err)
	}
	if cached != true {
		t.Errorf("ProcessInputs UsedCache == false; expected true")
	}
	if !cmp.Equal(wantDeps, gotDeps) {
		t.Errorf("ProcessInputs(%q)=%q; want %q\ndiff -want +got\n%s", filename, gotDeps, wantDeps, cmp.Diff(wantDeps, gotDeps))
	}
}

func TestProcessInputs_remoteError(t *testing.T) {
	t.Parallel()
	execID := uuid.New().String()
	stubClient := &stubClient{
		processInputsResponse: &pb.CPPProcessInputsResponse{
			// Set values to verify they are not returned on error
			Dependencies: []string{filename},
			UsedCache:    true,
		},
		processInputsError: errors.New("Error"),
	}

	client := &DepsScannerClient{
		ctx:    context.Background(),
		client: stubClient,
	}

	gotDeps, cached, err := client.ProcessInputs(context.Background(), execID, compileCommand, filename, directory, []string{})
	if err == nil {
		t.Errorf("ProcessInputs succeeded; expected error")
	}
	if cached != false {
		t.Errorf("ProcessInputs produced an error, but UsedCache is true (expected false)")
	}
	if gotDeps != nil {
		t.Errorf("ProcessInputs produced an error, but dependencies was not nil:  %v", gotDeps)
	}
}

func TestProcessInputs_timeoutError(t *testing.T) {
	t.Parallel()
	execID := uuid.New().String()
	stubClient := &stubClient{
		processInputsResponse: &pb.CPPProcessInputsResponse{
			// Set values to verify they are not returned on error
			Dependencies: []string{filename},
			UsedCache:    true,
		},
		processInputsSleep: 2 * processInputsTimeout,
	}

	client := &DepsScannerClient{
		ctx:    context.Background(),
		client: stubClient,
	}
	ctx, cancel := context.WithTimeout(context.Background(), processInputsTimeout)
	defer cancel()
	gotDeps, cached, err := client.ProcessInputs(ctx, execID, compileCommand, filename, directory, []string{})
	if err == nil {
		t.Errorf("ProcessInputs succeeded; expected error")
	}
	if cached != false {
		t.Errorf("ProcessInputs produced an error, but UsedCache is true (expected false)")
	}
	if gotDeps != nil {
		t.Errorf("ProcessInputs produced an error, but dependencies was not nil:  %v", gotDeps)
	}
}

func TestFindKeyVal(t *testing.T) {
	tests := []struct {
		name      string
		envStr    string
		knownVars map[string]string
		wantKey   string
		wantVal   string
		wantErr   bool
	}{{
		name:   "Normal",
		envStr: "Key=Val",
		knownVars: map[string]string{
			"Key": "Val",
		},
		wantKey: "Key",
		wantVal: "Val",
	}, {
		name:   "EmptyVal",
		envStr: "Key=",
		knownVars: map[string]string{
			"Key": "",
		},
		wantKey: "Key",
		wantVal: "",
	}, {
		name:   "SpaceInVal",
		envStr: "Key=Val Abc",
		knownVars: map[string]string{
			"Key": "Val Abc",
		},
		wantKey: "Key",
		wantVal: "Val Abc",
	}, {
		name:   "EqualsInKey",
		envStr: "Key=StillTheKey=Val",
		knownVars: map[string]string{
			"Key=StillTheKey": "Val",
		},
		wantKey: "Key=StillTheKey",
		wantVal: "Val",
	}, {
		name:   "EqualsInKeyEmptyVal",
		envStr: "Key=StillTheKey=",
		knownVars: map[string]string{
			"Key=StillTheKey": "",
		},
		wantKey: "Key=StillTheKey",
		wantVal: "",
	}, {
		name:   "EqualsInVal",
		envStr: "Key=Val=StillTheVal",
		knownVars: map[string]string{
			"Key": "Val=StillTheVal",
		},
		wantKey: "Key",
		wantVal: "Val=StillTheVal",
	}, {
		name:   "EqualsInKeyOverlap",
		envStr: "Key=StillTheKey=Val",
		knownVars: map[string]string{
			"Key":             "Val=StillTheVal",
			"Key=StillTheKey": "Val",
		},
		wantKey: "Key=StillTheKey",
		wantVal: "Val",
	}, {
		name:      "MissingEnvVar",
		envStr:    "Key=StillTheKey=Val",
		knownVars: map[string]string{},
		wantKey:   "",
		wantVal:   "",
		wantErr:   true,
	},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			lookupEnvFunc := func(k string) (string, bool) {
				v, ok := test.knownVars[k]
				return v, ok
			}
			gotKey, gotVal, err := findKeyVal(test.envStr, lookupEnvFunc)
			if test.wantErr && err == nil {
				t.Errorf("findKeyVal(%v) did not return expected error", test.envStr)
			}
			if !test.wantErr && err != nil {
				t.Errorf("findKeyVal(%v) returned unexpected error: %v", test.envStr, err)
			}
			if gotKey != test.wantKey || gotVal != test.wantVal {
				t.Errorf("findKeyVal(%v) returned wrong key value pair, wanted key=%v value=%v, got key=%v value=%v", test.envStr, test.wantKey, test.wantVal, gotKey, gotVal)
			}
		})
	}
}

func TestBuildAddress(t *testing.T) {
	tests := []struct {
		name           string
		platforms      []string
		serverAddr     string
		openPortFunc   func() (int, error)
		wantAddrPrefix string
	}{
		{
			name:           "UnixSocketReplacereproxy",
			platforms:      []string{"linux", "darwin"},
			serverAddr:     "unix:///some/dir/reproxy_123.sock",
			openPortFunc:   func() (int, error) { return 111, nil },
			wantAddrPrefix: "unix:///some/dir/depscan_123.sock",
		},
		{
			name:           "UnixSocketAbsOnUnix",
			platforms:      []string{"linux", "darwin"},
			serverAddr:     "unix:///some/dir/somesocket.sock",
			openPortFunc:   func() (int, error) { return 111, nil },
			wantAddrPrefix: "unix:///some/dir/ds_somesocket.sock",
		},
		{
			name:           "UnixSocketAbsNoDirsOnUnix",
			platforms:      []string{"linux", "darwin"},
			serverAddr:     "unix:///somesocket.sock",
			openPortFunc:   func() (int, error) { return 111, nil },
			wantAddrPrefix: "unix:///ds_somesocket.sock",
		},
		{
			name:           "UnixSocketRelNoDirsOnUnix",
			platforms:      []string{"linux", "darwin"},
			serverAddr:     "unix://somesocket.sock",
			openPortFunc:   func() (int, error) { return 111, nil },
			wantAddrPrefix: "unix://ds_somesocket.sock",
		},
		{
			name:           "UnixSocketRelOnUnix",
			platforms:      []string{"linux", "darwin"},
			serverAddr:     "unix://a/b/somesocket.sock",
			openPortFunc:   func() (int, error) { return 111, nil },
			wantAddrPrefix: "unix://a/b/ds_somesocket.sock",
		},
		{
			name:           "UnixSocketOnWindows",
			platforms:      []string{"windows"},
			serverAddr:     "unix://C:\\src\\somesocket.sock",
			openPortFunc:   func() (int, error) { return 222, nil },
			wantAddrPrefix: "unix:C:\\src\\ds_somesocket.sock",
		},
		{
			name:           "WindowsPipe",
			platforms:      []string{"windows"},
			serverAddr:     "pipe://pipename.pipe",
			openPortFunc:   func() (int, error) { return 333, nil },
			wantAddrPrefix: fmt.Sprintf("unix:%s", filepath.Join(os.TempDir(), "ds")),
		},
		{
			name:           "TCP",
			platforms:      []string{"linux", "darwin", "windows"},
			serverAddr:     "127.0.0.1:8000",
			openPortFunc:   func() (int, error) { return 444, nil },
			wantAddrPrefix: "127.0.0.1:444",
		},
		{
			name:           "AnotherTCPAddress",
			platforms:      []string{"linux", "darwin", "windows"},
			serverAddr:     "192.168.1.1:8000",
			openPortFunc:   func() (int, error) { return 555, nil },
			wantAddrPrefix: "192.168.1.1:555",
		},
	}
	for _, test := range tests {
		test := test
		runForPlatforms(t, test.name, test.platforms, func(t *testing.T) {
			t.Parallel()
			gotAddr, err := buildAddress(test.serverAddr, test.openPortFunc)
			if err != nil {
				t.Errorf("buildAddress(%v) returned unexpected error: %v", test.serverAddr, err)
			}
			if !strings.HasPrefix(gotAddr, test.wantAddrPrefix) {
				t.Errorf("buildAddress(%v) returned wrong address, wanted prefix '%v', got '%v'", test.serverAddr, test.wantAddrPrefix, gotAddr)
			}
		})
	}
}

func runForPlatforms(t *testing.T, name string, platforms []string, test func(t *testing.T)) {
	t.Helper()
	for _, platform := range platforms {
		if runtime.GOOS == platform {
			t.Run(name, test)
			return
		}
	}
}
