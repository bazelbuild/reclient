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

package reproxy

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bazelbuild/reclient/internal/pkg/event"
	"github.com/bazelbuild/reclient/internal/pkg/features"
	"github.com/bazelbuild/reclient/internal/pkg/interceptors"
	"github.com/bazelbuild/reclient/internal/pkg/labels"
	"github.com/bazelbuild/reclient/internal/pkg/logger"
	"github.com/bazelbuild/reclient/internal/pkg/pathtranslator"
	"github.com/bazelbuild/reclient/internal/pkg/protoencoding"
	"github.com/bazelbuild/reclient/pkg/inputprocessor"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/digest"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/filemetadata"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/outerr"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/rexec"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/uploadinfo"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lpb "github.com/bazelbuild/reclient/api/log"
	ppb "github.com/bazelbuild/reclient/api/proxy"
	spb "github.com/bazelbuild/reclient/api/stats"

	cpb "github.com/bazelbuild/remote-apis-sdks/go/api/command"

	log "github.com/golang/glog"
)

const (
	// osFamilyKey is the key used to identify the platform OS Family.
	osFamilyKey = "OSFamily"
	// poolKey is the key used to specify the worker pool.
	poolKey = "Pool"
	// labelKeyPrefix is the key prefix to select workers.
	labelKeyPrefix = "label:"
	// platformVersionKey is the key used to identify the reproxy version generating the action.
	platformVersionKey = "reproxy-version"
	// cacheSiloKey is the key used to cache-silo all the actions in the current run of reproxy.
	cacheSiloKey = "cache-silo"
)

const (
	// ReclientTimeoutExitCode is an exit code corresponding to a timeout within reproxy
	ReclientTimeoutExitCode = command.RemoteErrorExitCode + /*SIGALRM=*/ 14
)

var (
	// runtime.GOOS -> RE OSFamily.
	// https://cloud.google.com/remote-build-execution/docs/remote-execution-properties#google_internal_properties
	// not match with https://github.com/bazelbuild/remote-apis/blob/master/build/bazel/remote/execution/v2/platform.md though.
	knownOSFamilies = map[string]string{
		"linux":   "Linux",
		"windows": "Windows",
	}
	pollTime = time.Second * 10
	// AllowedIPTimeouts is the max number of IP timeouts before failing the build
	AllowedIPTimeouts = int64(7)
)

// Server is the server responsible for executing and retrieving results of RunRequests.
type Server struct {
	InputProcessor            *inputprocessor.InputProcessor
	FileMetadataStore         filemetadata.Cache
	REClient                  *rexec.Client
	LocalPool                 *LocalPool
	Logger                    *logger.Logger
	KeepLastRecords           int
	CacheSilo                 string
	VersionCacheSilo          bool
	RemoteDisabled            bool
	DumpInputTree             bool
	Forecast                  *Forecast
	StartTime                 time.Time
	FailEarlyMinActionCount   int64
	FailEarlyMinFallbackRatio float64
	FailEarlyWindow           time.Duration
	RacingBias                float64
	DownloadTmp               string
	MaxHoldoff                time.Duration // Maximum amount of time to wait for downloads before starting racing.
	ReclientVersion           string
	StartupCancelFn           func()
	numActions                *windowedCount
	numFallbacks              *windowedCount
	numIPTimeouts             *atomic.Int64
	numActiveActions          *atomic.Int32
	failBuild                 bool
	failBuildMu               sync.RWMutex
	failBuildErr              error
	activeActions             sync.Map
	records                   []*lpb.LogRecord
	rmu                       sync.Mutex
	wgShutdown                sync.WaitGroup
	shutdownCmd               chan bool
	shutdownOnce              sync.Once
	drain                     chan bool
	cleanupDone               chan bool
	rrOnce                    sync.Once
	stats                     *spb.Stats // Only populated after DrainAndReleaseResources() is called
	started                   chan bool
	startErr                  error
	cleanupFns                []func()
	smu                       sync.Mutex
	maxThreads                int32
}

type ctxWithCause struct {
	context.Context
	cause error
}

var (
	// A UUID of this proxy.
	invocationID = uuid.New().String()
)

func (c ctxWithCause) Err() error {
	if c.cause != nil {
		return c.cause
	}
	return c.Context.Err()
}

func cancelWithCause(ctx context.Context) (context.Context, func(error)) {
	cancelCtx, cancel := context.WithCancel(ctx)
	withCause := &ctxWithCause{Context: cancelCtx}
	return withCause, func(cause error) {
		withCause.cause = cause
		cancel()
	}
}

// Init initializes internal state and should only be called once.
func (s *Server) Init() {
	s.shutdownCmd = make(chan bool)
	s.drain = make(chan bool)
	s.started = make(chan bool)
	s.cleanupDone = make(chan bool)
	s.numActions = &windowedCount{window: s.FailEarlyWindow}
	s.numFallbacks = &windowedCount{window: s.FailEarlyWindow}
	s.numIPTimeouts = &atomic.Int64{}
	s.numActiveActions = &atomic.Int32{}
	prevMaxThreads := debug.SetMaxThreads(10000)
	if prevMaxThreads != 10000 {
		debug.SetMaxThreads(prevMaxThreads)
	}
	s.maxThreads = int32(prevMaxThreads)
}

// SetInputProcessor sets the InputProcessor property of Server and then unblocks startup.
// cleanup will be run in Server.DrainAndReleaseResources()
func (s *Server) SetInputProcessor(ip *inputprocessor.InputProcessor, cleanup func()) {
	s.smu.Lock()
	defer s.smu.Unlock()
	if s.startErr != nil {
		cleanup()
		return
	}
	s.InputProcessor = ip
	s.cleanupFns = append(s.cleanupFns, cleanup)
	s.maybeUnblockStartup()
}

// SetREClient sets the REClient property of Server and then unblocks startup.
// If rclient.GrpcClient is not nil then rclient.GrpcClient will be closed in
// Server.DrainAndReleaseResources()
func (s *Server) SetREClient(rclient *rexec.Client, cleanup func()) {
	s.smu.Lock()
	defer s.smu.Unlock()
	if s.startErr != nil {
		cleanup()
		return
	}
	s.REClient = rclient
	s.cleanupFns = append(s.cleanupFns, cleanup)
	s.maybeUnblockStartup()
}

// SetStartupErr saves the first error that happens during async startup and then unblocks startup.
func (s *Server) SetStartupErr(err error) {
	s.smu.Lock()
	defer s.smu.Unlock()
	if s.startErr != nil {
		return
	}
	s.startErr = err
	s.maybeUnblockStartup()
}

func (s *Server) maybeUnblockStartup() {
	select {
	case <-s.started:
		// startup is already unblocked
		return
	default:
	}
	log.Infof("Startup status: Input Processor started? %v, RBE connected (or disabled)? %v, Startup error? %v", s.InputProcessor != nil, s.REClient != nil, s.startErr != nil)
	if s.startErr != nil || (s.InputProcessor != nil && s.REClient != nil) {
		log.Infof("Done startup")
		close(s.started)
	}
}

// MonitorFailBuildConditions monitors fail early conditions. If conditions such as
// ratio of fallbacks to total actions periodically or number of IP timeouts are exceeded,
// it sets failBuild flag and failBuildErr.
// If fail early is disabled the function returns immediately.
// Should be run in a goroutine.
func (s *Server) MonitorFailBuildConditions(ctx context.Context) {
	s.monitorFailBuildConditions(ctx, pollTime)
}

func (s *Server) monitorFailBuildConditions(ctx context.Context, pollTime time.Duration) {
	if s.FailEarlyMinActionCount == 0 || s.FailEarlyMinFallbackRatio == 0 {
		return
	}
	ticker := time.NewTicker(pollTime)
	for {
		select {
		case <-ticker.C:
			s.checkFailBuild()
		case <-ctx.Done():
			return
		}
	}
}

type windowedCount struct {
	cnt    atomic.Int64
	window time.Duration
}

func (wc *windowedCount) Add(inc int64) {
	wc.cnt.Add(inc)
	if wc.window != 0 {
		time.AfterFunc(wc.window, func() {
			wc.cnt.Add(-inc)
		})
	}
}

func (wc *windowedCount) Load() int64 {
	return wc.cnt.Load()
}

func (s *Server) checkFailBuild() {
	nf := s.numFallbacks.Load()
	na := s.numActions.Load()
	nt := s.numIPTimeouts.Load()
	if nt <= AllowedIPTimeouts && na < s.FailEarlyMinActionCount {
		return
	}
	if nt > AllowedIPTimeouts || float64(nf)/float64(na) >= s.FailEarlyMinFallbackRatio {
		s.failBuildMu.Lock()
		defer s.failBuildMu.Unlock()
		// Set the switch to fail all the new actions...
		s.failBuild = true
		s.failBuildErr = s.getFailBuildErr(nf, na, nt)
		// .. and cancel the actions that are already started.
		s.activeActions.Range(func(key, val any) bool {
			if action, ok := val.(*action); ok {
				action.cancelFunc(s.failBuildErr)
			}
			return true
		})
		return
	}
}

func (s *Server) shouldFailBuild() (bool, error) {
	s.failBuildMu.RLock()
	defer s.failBuildMu.RUnlock()
	return s.failBuild, s.failBuildErr
}

func (s *Server) getFailBuildErr(nf, na, nt int64) error {
	if nt > AllowedIPTimeouts {
		return fmt.Errorf("this build has encountered too many action input processing timeouts. Number of timeouts %v > %v",
			nt, AllowedIPTimeouts)
	}
	if float64(nf)/float64(na) >= s.FailEarlyMinFallbackRatio {
		return fmt.Errorf(
			"this build has encountered too many local fallbacks. This means that the ratio of actions that failed remotely is equal to or above the preconfigured threshold of %.1f%%",
			s.FailEarlyMinFallbackRatio*100)
	}
	return nil
}

// WaitForShutdownCommand returns a channel that is closed when a shutdown command is recieved.
func (s *Server) WaitForShutdownCommand() <-chan bool {
	return s.shutdownCmd
}

// WaitForCleanupDone returns a channel that is closed when all cleanup has been completed.
func (s *Server) WaitForCleanupDone() <-chan bool {
	return s.cleanupDone
}

// DrainAndReleaseResources closes all external resources, cancels all inflight RunCommand rpcs
// and prevents future RunCommand rpcs in preparation for server exit.
// s.stats is populated with aggregated build metrics.
// All calls after the first are noops.
func (s *Server) DrainAndReleaseResources() {
	s.rrOnce.Do(func() {
		select {
		case <-s.started:
		default:
			// Cancel startup and wait for cancellation to propagate before shutting down
			// if startup never finished.
			s.StartupCancelFn()
			if err := s.waitForStarted(context.Background()); err != nil {
				log.Errorf("Error in reproxy startup: %v", err)
			}
		}
		close(s.drain)
		s.wgShutdown.Wait()
		if s.Logger != nil {
			s.Logger.AddEventTimeToProxyInfo(event.ProxyUptime, s.StartTime, time.Now())
		}
		s.stats = s.Logger.CloseAndAggregate()
		go func() {
			defer close(s.cleanupDone)
			for _, cleanup := range s.cleanupFns {
				if cleanup != nil {
					cleanup()
				}
			}
		}()
	})
}

// Shutdown shuts the server down gracefully.
func (s *Server) Shutdown(ctx context.Context, req *ppb.ShutdownRequest) (*ppb.ShutdownResponse, error) {
	s.shutdownOnce.Do(func() {
		close(s.shutdownCmd)
		// Ensure DrainAndReleaseResources has been called to guarentee that s.stats is populated.
		s.DrainAndReleaseResources()
	})
	return &ppb.ShutdownResponse{
		Stats: s.stats,
	}, nil
}

func (s *Server) withServerDrainCancel(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		defer cancel()
		select {
		case <-s.drain:
			return
		case <-ctx.Done():
			return
		}
	}()
	return ctx
}

func (s *Server) waitForStarted(ctx context.Context) error {
	select {
	case <-s.started:
		return s.startErr
	case <-ctx.Done():
		return status.Error(codes.DeadlineExceeded, "timedout waiting for reproxy to start")
	}
}

// RunCommand runs a command according to the parameters defined in the RunRequest.
func (s *Server) RunCommand(ctx context.Context, req *ppb.RunRequest) (*ppb.RunResponse, error) {
	log.V(1).Infof("Received RunRequest:\n%s", protoencoding.TextWithIndent.Format(req))
	// Intentionally overwriting ctx so that all function calls below will be canceled at server shutdown.
	ctx = s.withServerDrainCancel(ctx)
	select {
	case <-ctx.Done(): // Short circuit if server is already shutting down
		return nil, ctx.Err()
	default:
	}
	if err := s.waitForStarted(ctx); err != nil {
		return nil, err
	}
	s.wgShutdown.Add(1)
	defer s.wgShutdown.Done()
	start := time.Now()
	if req.Command == nil {
		return nil, status.Error(codes.InvalidArgument, "no command provided in the request")
	}
	executionID := uuid.New().String()
	cmd := command.FromProto(req.Command)
	cmd.Identifiers.ExecutionID = executionID
	cmd.Identifiers.ToolName = "re-client"
	cmd.Identifiers.ToolVersion = s.ReclientVersion
	if err := cmd.Validate(); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if cmd.Identifiers.InvocationID == "" {
		cmd.Identifiers.InvocationID = invocationID
	}
	cmd.FillDefaultFieldValues()

	s.addLabelDigestToCommandID(cmd, req)
	if cmd.Platform == nil {
		cmd.Platform = make(map[string]string)
	}
	if (req.GetExecutionOptions().GetExecutionStrategy() == ppb.ExecutionStrategy_LOCAL && !req.GetExecutionOptions().GetLocalExecutionOptions().GetDoNotCache()) || s.VersionCacheSilo {
		// For LERC actions, there's a chance we may have reclient bugs.
		// We want to ensure we don't persist those across actions.
		// We also want to add version if the option to do so is set to true.
		cmd.Platform[platformVersionKey] = s.ReclientVersion
	}

	if len(s.CacheSilo) > 0 {
		cmd.Platform[cacheSiloKey] = s.CacheSilo
	}
	setPlatformOSFamily(cmd)

	localMetadata := &lpb.LocalMetadata{
		EventTimes: map[string]*cpb.TimeInterval{},
		Labels:     req.GetLabels(),
	}
	cmdEnv := req.GetMetadata().GetEnvironment()
	if req.GetExecutionOptions().GetLogEnvironment() {
		localMetadata.Environment = sliceToMap(cmdEnv, "=")
	}
	rec := s.Logger.LogActionStart()
	rec.LocalMetadata = localMetadata
	for k, t := range req.GetMetadata().GetEventTimes() {
		if t.To == nil {
			t.To = command.TimeToProto(start)
		}
		rec.LocalMetadata.EventTimes[k] = t
	}

	compareMode := req.GetExecutionOptions().GetCompareWithLocal()
	numLocalRerun := int(req.GetExecutionOptions().GetNumLocalReruns())
	numRemoteRerun := int(req.GetExecutionOptions().GetNumRemoteReruns())
	if numRemoteRerun == 0 {
		numRemoteRerun = int(req.GetExecutionOptions().GetNumRetriesIfMismatched())
	}

	if compareMode && (numLocalRerun+numRemoteRerun) < 2 {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("%v: Invalid number of reruns. Total reruns of >= 2 expected. Got num_local_reruns=%v and num_remote_reruns=%v", executionID, numLocalRerun, numRemoteRerun))
	}

	reclientTimeout := int(req.GetExecutionOptions().GetReclientTimeout())
	if reclientTimeout <= 0 {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("Invalid reclient_timeout set. Expected a value > 0, got: %v", reclientTimeout))
	}

	ti := req.ToolchainInputs
	rw := req.GetExecutionOptions().GetRemoteExecutionOptions().GetWrapper()
	if rw != "" {
		// Add remote wrapper to toolchain for toolchain input processing. remote wrapper is similar
		// to explicitly passed toolchains in that it may have dependencies and will need to be
		// findable and executable on the remote machine.
		rw = pathtranslator.RelToExecRoot(cmd.ExecRoot, cmd.WorkingDir, rw)
		ti = append(ti, rw)
	}
	aCtx, cancel := cancelWithCause(ctx)
	a := &action{
		cmd:             cmd,
		windowsCross:    isWindowsCross(cmd.Platform),
		rOpt:            req.GetExecutionOptions().GetRemoteExecutionOptions(),
		lOpt:            req.GetExecutionOptions().GetLocalExecutionOptions(),
		execStrategy:    req.GetExecutionOptions().GetExecutionStrategy(),
		compare:         compareMode,
		numLocalReruns:  numLocalRerun,
		numRemoteReruns: numRemoteRerun,
		reclientTimeout: reclientTimeout,
		oe:              outerr.NewRecordingOutErr(),
		fallbackOE:      outerr.NewRecordingOutErr(),
		rec:             rec,
		fmc:             s.FileMetadataStore,
		forecast:        s.Forecast,
		lbls:            req.Labels,
		toolchainInputs: ti,
		cmdEnvironment:  cmdEnv,
		cancelFunc:      cancel,
		racingBias:      s.RacingBias,
		downloadRegex:   req.GetExecutionOptions().GetDownloadRegex(),
		downloadTmp:     s.DownloadTmp,
		atomicDownloads: req.GetExecutionOptions().GetEnableAtomicDownloads(),
	}
	if s.numActiveActions.Load() >= s.maxThreads {
		// By default, go runtime only hold maximum 10K M (machines), Once the num
		// of M is already at max, but there are still more G (goroutines) in any
		// P (processors) that is ready to be executed, go runtime will try to
		// creat a new M and then crash.
		// In this case, rejecting any new commands, and relying on rewrapper to
		// re-try the action would be more graceful.
		// See: https://github.com/bazelbuild/remote-apis-sdks/blob/8a36686a6350d32b9f40c213290b107a59e2ab00/go/pkg/retry/retry.go#L89
		return nil, status.Error(codes.Unavailable, fmt.Sprintf("%v: Reproxy currently has high num of in-flight actions. Please retry with a backoff.", executionID))
	}
	s.activeActions.Store(executionID, a)
	s.numActiveActions.Add(1)
	defer s.numActiveActions.Add(-1)
	defer s.activeActions.Delete(executionID)

	if rand.Intn(100) < features.GetConfig().ExperimentalCacheMissRate {
		a.rOpt.AcceptCached = false
	}

	s.runAction(aCtx, a)
	if a.compare {
		s.rerunAction(aCtx, a)
	}

	s.logRecord(a, start)
	oe := a.oe.(*outerr.RecordingOutErr)
	fallbackOE := a.fallbackOE.(*outerr.RecordingOutErr)
	var logRecord *lpb.LogRecord
	if req.GetExecutionOptions().GetIncludeActionLog() {
		logRecord = a.rec.LogRecord
	}
	fi := FallbackInfo{
		res: &command.Result{
			ExitCode: a.fallbackExitCode,
			Status:   a.fallbackResultStatus,
		},
	}
	if !a.res.IsOk() {
		log.Errorf("%v: Execution failed with %+v", executionID, a.res)
		log.Flush()
		status := a.res.Status

		if status != command.NonZeroExitResultStatus && oe != nil && len(oe.Stdout()) == 0 && len(oe.Stderr()) == 0 {
			if a.res.Err != nil {
				errorMsg := "reclient[" + executionID + "]: " + status.String() + ": " + a.res.Err.Error() + "\n"
				return toResponse(cmd, a.res, nil, []byte(errorMsg), fi, logRecord), nil
			}
			return toResponse(cmd, a.res, nil, nil, fi, logRecord), nil
		}
	}

	var oeStdout, oeStderr []byte
	if oe != nil {
		oeStdout = oe.Stdout()
		oeStderr = oe.Stderr()
	}
	if fallbackOE != nil {
		fi.stdout = fallbackOE.Stdout()
		fi.stderr = fallbackOE.Stderr()
	}

	return toResponse(cmd, a.res, oeStdout, oeStderr, fi, logRecord), nil
}

func isWindowsCross(platformProperty map[string]string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	os, ok := platformProperty[osFamilyKey]
	if !ok {
		return false
	}
	return os != "Windows"
}

// setPlatformOSFamily sets OSFamily in platform if OSFamily, Pool,
// or label:<user-label-key> is not set.
func setPlatformOSFamily(cmd *command.Command) {
	if cmd.Platform == nil {
		cmd.Platform = make(map[string]string)
	}
	_, ok := cmd.Platform[osFamilyKey]
	if ok {
		// use rewrapper specified OSFamily.
		return
	}
	_, ok = cmd.Platform[poolKey]
	if ok {
		// use rewrapper specified Pool.
		return
	}
	for k := range cmd.Platform {
		if strings.HasPrefix(k, labelKeyPrefix) {
			// use rewrapper specified label:<user-label-key>
			return
		}
	}

	osFamily, ok := knownOSFamilies[runtime.GOOS]
	if !ok {
		log.Warningf("%v: no platform OSFamily", cmd.Identifiers.ExecutionID)
		return
	}
	log.V(1).Infof("%v: set platform OSFamily=%q", cmd.Identifiers.ExecutionID, osFamily)
	cmd.Platform[osFamilyKey] = osFamily
}

// addLabelDigestToCommandID adds a digest of the request labels to commandID so that the action type
// can be identified in remote-execution logs.
func (s *Server) addLabelDigestToCommandID(cmd *command.Command, req *ppb.RunRequest) error {
	labelDigest, err := labels.ToDigest(req.GetLabels())
	if err != nil {
		return err
	}
	cmd.Identifiers.CommandID = fmt.Sprintf("%s-%s", labelDigest, cmd.Identifiers.CommandID)
	return nil
}

// GetRecords returns the latest saved LogRecords.
func (s *Server) GetRecords(ctx context.Context, req *ppb.GetRecordsRequest) (*ppb.GetRecordsResponse, error) {
	return &ppb.GetRecordsResponse{Records: s.records}, nil
}

// AddProxyEvents saves the provided event times to the proxy level metrics.
// This method returns immediately and event times are added asynchronously to not block startup.
func (s *Server) AddProxyEvents(ctx context.Context, req *ppb.AddProxyEventsRequest) (*ppb.AddProxyEventsResponse, error) {
	m := make(map[string]*cpb.TimeInterval, len(req.EventTimes))
	for k, v := range req.EventTimes {
		m[k] = v
	}
	// Run asynchronously so that startup is not blocked on obtaining the lock in Logger
	go s.Logger.AddEventTimesToProxyInfo(m)
	return &ppb.AddProxyEventsResponse{}, nil
}

func (s *Server) logRecord(a *action, start time.Time) {
	a.rec.Command = command.ToProto(a.cmd)
	if a.res != nil {
		a.rec.Result = command.ResultToProto(a.res)
	} else if a.rec.LocalMetadata.GetResult() != nil {
		a.rec.Result = a.rec.LocalMetadata.Result
	} else if a.rec.RemoteMetadata.GetResult() != nil {
		a.rec.Result = a.rec.RemoteMetadata.Result
	}
	a.rec.RecordEventTime(event.ProxyExecution, start)
	logger.AddCompletionStatus(a.rec, a.execStrategy)
	s.Logger.Log(a.rec)
	if s.KeepLastRecords > 0 {
		s.rmu.Lock()
		defer s.rmu.Unlock()
		s.records = append(s.records, a.rec.LogRecord)
		if len(s.records) > s.KeepLastRecords {
			s.records = s.records[1:len(s.records)]
		}
	}
	if s.Forecast != nil {
		s.Forecast.RecordSample(a)
	}
}

func (s *Server) populateCommandIO(ctx context.Context, a *action) (err error) {
	if err = a.populateCommandIO(ctx, s.InputProcessor); errors.Is(err, inputprocessor.ErrIPTimeout) {
		s.numIPTimeouts.Add(1)
	}
	return err
}

func (s *Server) runAction(ctx context.Context, a *action) {
	if failBuild, failBuildErr := s.shouldFailBuild(); failBuild {
		if failBuildErr != nil {
			a.res = command.NewLocalErrorResult(failBuildErr)
			a.oe.WriteOut([]byte(failBuildErr.Error()))
		}
		return
	}
	defer s.numActions.Add(1)
	if s.RemoteDisabled {
		a.runLocal(ctx, s.LocalPool)
		return
	}
	log.V(2).Infof("%v: Execution strategy: %v", a.cmd.Identifiers.ExecutionID, a.execStrategy)
	// b/261368381: A local fallback might update the in-out file without invalidating its cache entry
	// in file metadata cache. There are more subtlties to this invalidation (e.g racing). The safest
	// option for now is to always invalidate cache entries for in-out files at the negligible cost of
	// redigesting them (there aren't many of such files in a build).
	defer a.clearInputOutputFileCache()
	if a.rOpt.GetCanonicalizeWorkingDir() {
		a.cmd.RemoteWorkingDir = toRemoteWorkingDir(a.cmd.WorkingDir)
	}
	switch a.execStrategy {
	case ppb.ExecutionStrategy_LOCAL:
		if err := a.downloadVirtualInputs(ctx, s.REClient); err != nil {
			log.Warningf("%v: Failed to download virtual inputs before LERC run: %v", a.cmd.Identifiers.ExecutionID, err)
		}
		s.runLERC(ctx, a)
		if !a.res.IsOk() && a.res.Status != command.NonZeroExitResultStatus {
			log.Warningf("%v: LERC failed with %+v, falling back to local.", a.cmd.Identifiers.ExecutionID, a.res)
			a.runLocal(ctx, s.LocalPool)
			s.numFallbacks.Add(1)
		}
		return
	case ppb.ExecutionStrategy_REMOTE:
		// TODO: add support for compare mode in remote execution.
		s.runRemote(ctx, a)
		return
	case ppb.ExecutionStrategy_REMOTE_LOCAL_FALLBACK:
		s.runRemote(ctx, a)
		if !a.res.IsOk() {
			roe := a.oe.(*outerr.RecordingOutErr)
			log.Warningf("%v: Remote execution failed with %+v, falling back to local.\n stdout: %s\n stderr: %s",
				a.cmd.Identifiers.ExecutionID, a.res, roe.Stdout(), roe.Stderr())
			a.fallbackOE = a.oe
			a.fallbackExitCode = a.res.ExitCode
			a.fallbackResultStatus = a.res.Status
			a.oe = outerr.NewRecordingOutErr()
			if err := a.downloadVirtualInputs(ctx, s.REClient); err != nil {
				log.Warningf("%v: Failed to download virtual inputs before local fallback: %v", a.cmd.Identifiers.ExecutionID, err)
			}
			a.runLocal(ctx, s.LocalPool)
			s.numFallbacks.Add(1)
		}
		return
	case ppb.ExecutionStrategy_RACING:
		s.runRacing(ctx, a)
		return
	}
	a.res = command.NewLocalErrorResult(fmt.Errorf("%v: invalid execution strategy %v", a.cmd.Identifiers.ExecutionID, a.execStrategy))
}

func (s *Server) rerunAction(ctx context.Context, a *action) {
	local := []*lpb.RerunMetadata{}
	remote := []*lpb.RerunMetadata{}
	var rerunWaiter sync.WaitGroup

	localActionDupes := a.duplicate(a.numLocalReruns)
	remoteActionDupes := a.duplicate(a.numRemoteReruns)
	c := make(chan *lpb.RerunMetadata, a.numRemoteReruns)
	rerunWaiter.Add(a.numRemoteReruns)
	for i := 0; i < a.numRemoteReruns; i++ {
		act := remoteActionDupes[i]
		act.rOpt.AcceptCached = false
		act.rOpt.DoNotCache = true
		act.rOpt.DownloadOutputs = false
		attemptNum := i + 1
		go func() {
			defer rerunWaiter.Done()
			act.runRemote(ctx, s.REClient)
			if !act.res.IsOk() {
				log.Warningf("%v: Execution failed during remote rerun attempt:%v with %v", act.cmd.Identifiers.ExecutionID, attemptNum, act.res)
			}
			remoteRerunMetaData := &lpb.RerunMetadata{
				Attempt:                int64(attemptNum),
				Result:                 command.ResultToProto(act.res),
				NumOutputFiles:         act.rec.RemoteMetadata.NumOutputFiles,
				NumOutputDirectories:   act.rec.RemoteMetadata.NumOutputDirectories,
				TotalOutputBytes:       act.rec.RemoteMetadata.TotalOutputBytes,
				OutputFileDigests:      act.rec.RemoteMetadata.OutputFileDigests,
				OutputDirectoryDigests: act.rec.RemoteMetadata.OutputDirectoryDigests,
				LogicalBytesDownloaded: act.rec.RemoteMetadata.LogicalBytesDownloaded,
				RealBytesDownloaded:    act.rec.RemoteMetadata.RealBytesDownloaded,
				EventTimes:             act.rec.RemoteMetadata.EventTimes,
			}
			c <- remoteRerunMetaData
		}()
	}
	rerunWaiter.Wait()
	close(c)

	for i := range c {
		remote = append(remote, i)
	}
	sort.Slice(remote, func(i, j int) bool {
		return remote[i].Attempt < remote[j].Attempt
	})

	restoreInOutFiles := a.stashInputOutputFiles()
	for i := 0; i < a.numLocalReruns; i++ {
		act := localActionDupes[i]
		attemptNum := i + 1
		act.clearOutputsCache()
		if a.compare {
			restoreInOutFiles()
			restoreInOutFiles = a.stashInputOutputFiles()
		}
		act.runLocal(ctx, s.LocalPool)

		if !act.res.IsOk() {
			log.Warningf("%v: Execution failed during local rerun attempt:%v with %+v", act.cmd.Identifiers.ExecutionID, attemptNum, act.res)
		}
		act.rec.EndAllEventTimes()
		var outPaths []string
		for _, file := range act.cmd.OutputFiles {
			outPaths = append(outPaths, file)
		}
		for _, dir := range act.cmd.OutputDirs {
			outPaths = append(outPaths, dir)
		}
		blobs, resPb, err := s.REClient.GrpcClient.ComputeOutputsToUpload(act.cmd.ExecRoot, act.cmd.WorkingDir, outPaths, filemetadata.NewSingleFlightCache(), act.cmd.InputSpec.SymlinkBehavior, act.cmd.InputSpec.InputNodeProperties)
		if err != nil {
			log.Warningf("Error computing output directory digests during local rerun attempt:%v with %v", attemptNum, err)
		}
		toUpload := []*uploadinfo.Entry{}
		for _, ch := range blobs {
			toUpload = append(toUpload, ch)
		}
		_, _, err = s.REClient.GrpcClient.UploadIfMissing(ctx, toUpload...)
		if err != nil {
			log.Warningf("Error uploading artifacts during local rerun attempt:%v with %v", attemptNum, err)
		}
		fileDgs := make(map[string]string)
		for _, file := range resPb.OutputFiles {
			dg := digest.NewFromProtoUnvalidated(file.Digest)
			fileDgs[file.Path] = dg.String()
		}
		dirDgs := make(map[string]string)
		for _, dir := range resPb.OutputDirectories {
			dg := digest.NewFromProtoUnvalidated(dir.TreeDigest)
			dirDgs[dir.Path] = dg.String()
		}
		localRerunMetaData := &lpb.RerunMetadata{
			Attempt:                int64(attemptNum),
			Result:                 command.ResultToProto(act.res),
			EventTimes:             act.rec.LocalMetadata.EventTimes,
			OutputFileDigests:      fileDgs,
			OutputDirectoryDigests: dirDgs,
		}
		local = append(local, localRerunMetaData)
	}

	a.rec.LocalMetadata.RerunMetadata = local
	if a.rec.RemoteMetadata != nil {
		a.rec.RemoteMetadata.RerunMetadata = remote
	}
	compareAction(ctx, s, a)
}

func (s *Server) runLERC(ctx context.Context, a *action) {
	if a.downloadRegex != "" {
		a.rOpt.DownloadOutputs = false
	}
	if err := a.createExecContext(ctx, s.REClient); err != nil {
		// This cannot really happen, as the only error it checks for is cmd.Validate.
		a.res = command.NewLocalErrorResult(err)
		return
	}
	if err := s.populateCommandIO(ctx, a); err != nil {
		return
	}
	log.V(1).Infof("%v: Inputs: %v", a.cmd.Identifiers.ExecutionID, a.cmd.InputSpec.Inputs)
	log.V(1).Infof("%v: Outputs: %v", a.cmd.Identifiers.ExecutionID, a.cmd.OutputFiles)
	log.V(1).Infof("%v: dFile: %v", a.cmd.Identifiers.ExecutionID, a.dFile)
	// Since this is LERC mode, add dependency file outputs to the command since we will
	// generate it locally.
	a.addDepsFileOutput()
	if s.DumpInputTree {
		if err := dumpInputs(a.cmd); err != nil {
			log.Errorf("%v: Failed to dump inputs: %+v", a.cmd.Identifiers.ExecutionID, err)
		}
	}
	if !a.lOpt.GetAcceptCached() {
		a.runLocal(ctx, s.LocalPool)
		a.cacheLocal()
		return
	}
	a.getCachedResult(ctx)
	if a.res == nil || !a.res.IsOk() {
		a.runLocal(ctx, s.LocalPool)
		a.cacheLocal()
		return
	}
}

func (s *Server) runRacing(ctx context.Context, a *action) {
	if features.GetConfig().CleanIncludePaths {
		a.cmd.Args = cleanIncludePaths(a.cmd)
	}

	if err := s.populateCommandIO(ctx, a); err != nil {
		return
	}
	log.V(1).Infof("%v: Inputs: %v, Outputs: %+v", a.cmd.Identifiers.ExecutionID, a.cmd.InputSpec.Inputs, a.cmd.OutputFiles)

	a.race(ctx, s.REClient, s.LocalPool, s.numFallbacks, s.MaxHoldoff)
	if a.res == nil {
		a.res = command.NewLocalErrorResult(fmt.Errorf("racing did not produce a result"))
	}
	if _, ok := a.oe.(*outerr.RecordingOutErr); !ok {
		log.Warningf("%v: Racing produced a nil OutErr.", a.cmd.Identifiers.ExecutionID)
		a.oe = outerr.NewRecordingOutErr()
	}
}

func (s *Server) runRemote(ctx context.Context, a *action) {
	rCtx, cancel := cancelWithCause(ctx)
	done := make(chan struct{})
	defer close(done)
	var errReclientTimeout = errors.New("remote action timed out by reclient timeout")
	defer func() {
		if errors.Is(a.res.Err, errReclientTimeout) || (errors.Is(a.res.Err, context.Canceled) && errors.Is(rCtx.Err(), errReclientTimeout)) {
			a.res = &command.Result{
				Status:   command.TimeoutResultStatus,
				Err:      errReclientTimeout,
				ExitCode: ReclientTimeoutExitCode,
			}
			a.rec.RemoteMetadata.Result = command.ResultToProto(a.res)
		}
	}()
	reclientTimeout := time.Duration(a.reclientTimeout) * time.Second
	shutdownTimeout := 2 * reclientTimeout
	if features.GetConfig().ExperimentalExitOnStuckActions {
		go func() {
			select {
			case <-time.After(shutdownTimeout):
				if a.rec != nil && a.rec.LogRecord != nil {
					log.Errorf("%v: LogRecord: %v", a.cmd.Identifiers.ExecutionID, a.rec.LogRecord)
				} else {
					log.Errorf("%v: Command: [%v] timed out", a.cmd.Identifiers.ExecutionID, a.cmd.Args)
				}
				log.Fatalf("%v: Remote execution didn't finish after %v. Shutting down reproxy.", a.cmd.Identifiers.ExecutionID, shutdownTimeout)
			case <-done:
			}
		}()
	}
	go func() {
		select {
		case <-time.After(reclientTimeout):
			log.V(1).Infof("%v: Exceeded reclient_timeout of %v. Cancelling execution context.", a.cmd.Identifiers.ExecutionID, reclientTimeout)
			cancel(errReclientTimeout)
		case <-done:
		}
	}()

	if features.GetConfig().CleanIncludePaths {
		a.cmd.Args = cleanIncludePaths(a.cmd)
	}
	if err := s.populateCommandIO(rCtx, a); err != nil {
		return
	}
	log.V(1).Infof("%v: Inputs: %v", a.cmd.Identifiers.ExecutionID, a.cmd.InputSpec.Inputs)
	var restoreInOutFiles restoreInOutFilesFn
	if a.compare {
		restoreInOutFiles = a.stashInputOutputFiles()
	}
	a.runRemote(rCtx, s.REClient)

	// We need not run compare mode if the remote result is already a failure.
	if !a.res.IsOk() || !a.compare {
		return
	}
	if a.compare {
		a.removeAllOutputs()
		restoreInOutFiles()
	}
}

// fileList returns the absolute path of all the files in the given list
// and all the files in the list of given directories.
func fileList(execRoot string, files []string, dirs []string) []string {
	var res []string
	for _, f := range files {
		path := f
		if !filepath.IsAbs(f) {
			path = filepath.Join(execRoot, f)
		}
		res = append(res, path)
	}
	for _, d := range dirs {
		filepath.Walk(filepath.Join(execRoot, d), func(path string, info os.FileInfo, err error) error {
			if err != nil {
				log.Warningf("Error when listing directory for files: %v", err)
				return nil
			}
			if info.IsDir() {
				return nil
			}
			res = append(res, path)
			return nil
		})
	}
	return res
}

type FallbackInfo struct {
	stdout []byte
	stderr []byte
	res    *command.Result
}

func toResponse(cmd *command.Command, res *command.Result, stdout, stderr []byte, fi FallbackInfo, logRecord *lpb.LogRecord) *ppb.RunResponse {
	var fallbackInfo *ppb.RemoteFallbackInfo
	if fi.stdout != nil || fi.stderr != nil ||
		(fi.res != nil && fi.res.ExitCode != 0) {
		cmdRes := *command.ResultToProto(fi.res)
		fallbackInfo = &ppb.RemoteFallbackInfo{
			ExitCode:     cmdRes.ExitCode,
			Stdout:       fi.stdout,
			Stderr:       fi.stderr,
			ResultStatus: cmdRes.Status,
		}
	}

	return &ppb.RunResponse{
		ExecutionId:        cmd.Identifiers.ExecutionID,
		Result:             command.ResultToProto(res),
		Stdout:             stdout,
		Stderr:             stderr,
		RemoteFallbackInfo: fallbackInfo,
		ActionLog:          logRecord,
	}
}

func dedup(list []string) []string {
	// key is cleaned path, value is original path.
	// returns original paths.
	fileSet := make(map[string]string)
	for _, f := range list {
		if _, found := fileSet[filepath.Clean(f)]; found {
			continue
		}
		fileSet[filepath.Clean(f)] = f
	}
	var dlist []string
	for _, f := range fileSet {
		dlist = append(dlist, f)
	}
	return dlist
}

func execOptionsFromProto(opt *ppb.RemoteExecutionOptions) *command.ExecutionOptions {
	if opt == nil {
		return command.DefaultExecutionOptions()
	}
	return &command.ExecutionOptions{
		AcceptCached:                 opt.AcceptCached,
		DoNotCache:                   opt.DoNotCache,
		DownloadOutErr:               true,
		DownloadOutputs:              opt.DownloadOutputs,
		PreserveUnchangedOutputMtime: opt.PreserveUnchangedOutputMtime,
	}
}

// It is temporary feature for nest build. b/157442013
func cleanIncludePaths(cmd *command.Command) []string {
	cleanArgs := []string{}
	for i := 0; i < len(cmd.Args); i++ {
		arg := cmd.Args[i]
		var dir string
		if arg == "-I" {
			dir = cmd.Args[i+1]
			i++
		} else if strings.HasPrefix(arg, "-I") {
			dir = strings.TrimSpace(strings.TrimPrefix(arg, "-I"))
		} else {
			cleanArgs = append(cleanArgs, arg)
			continue
		}

		if !filepath.IsAbs(dir) {
			cleanArgs = append(cleanArgs, "-I"+dir)
			continue
		}

		relDir, err := filepath.Rel(filepath.Join(cmd.ExecRoot, cmd.WorkingDir), dir)
		if err != nil {
			log.Warningf("failed to make %s relative to working directory", dir)
			cleanArgs = append(cleanArgs, "-I"+dir)
			continue
		}
		cleanArgs = append(cleanArgs, "-I"+relDir)
	}
	return cleanArgs
}

// DeleteOldLogFiles removes RE tooling log files older than d from dir.
func DeleteOldLogFiles(d time.Duration, dir string) {
	maxAge := time.Now().Add(-d)
	log.V(1).Infof("Deleting RE logs older than %v from %s...", maxAge.Format("2006-01-02 15:04:05"), dir)
	files, err := os.ReadDir(dir)
	if err != nil {
		log.V(1).Infof("Failed listing %s: %v", dir, err)
		return
	}

	filenamePatterns := []string{`^reproxy.*`, `^rewrapper.*`, `^bootstrap.*`, interceptors.DumpNameRegexPattern}
	filenameRegex, err := regexp.Compile(strings.Join(filenamePatterns, "|"))
	if err != nil {
		log.Fatalf("Error compiling regex %s: %v", filenameRegex, err)
	}
	var toDelete []string
	for _, f := range files {
		if info, err := f.Info(); err == nil && filenameRegex.MatchString(f.Name()) && info.ModTime().Before(maxAge) {
			toDelete = append(toDelete, f.Name())
		}
	}
	log.V(1).Infof("About to delete %d old logs from %s", len(toDelete), dir)
	delCount, errCount := 0, 0
	for _, name := range toDelete {
		path := filepath.Join(dir, name)
		log.V(3).Infof("Deleting %s...", path)
		if err := os.Remove(path); err != nil {
			errCount++
			log.V(3).Infof("Error deleting %s :%v", path, err)
		} else {
			delCount++
		}
	}
	log.V(1).Infof("Successfully deleted %d old logs from %s, %d errors.", delCount, dir, errCount)
}

func sliceToMap(slice []string, separator string) map[string]string {
	result := make(map[string]string)
	for _, item := range slice {
		pair := strings.SplitN(item, separator, 2)
		result[pair[0]] = pair[1]
	}
	return result
}
