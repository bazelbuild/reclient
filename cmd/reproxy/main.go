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

// Binary reproxy is a long running server that rewrapper binary talks to
// for fast and efficient remote-execution and caching of various types of actions.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"sync"
	"syscall"
	"time"

	"team/foundry-x/re-client/internal/pkg/auth"
	"team/foundry-x/re-client/internal/pkg/cppdependencyscanner"
	"team/foundry-x/re-client/internal/pkg/ignoremismatch"
	"team/foundry-x/re-client/internal/pkg/interceptors"
	"team/foundry-x/re-client/internal/pkg/ipc"
	"team/foundry-x/re-client/internal/pkg/localresources"
	"team/foundry-x/re-client/internal/pkg/logger"
	"team/foundry-x/re-client/internal/pkg/monitoring"
	"team/foundry-x/re-client/internal/pkg/pathtranslator"
	"team/foundry-x/re-client/internal/pkg/rbeflag"
	"team/foundry-x/re-client/internal/pkg/reproxy"
	"team/foundry-x/re-client/internal/pkg/reproxypid"
	"team/foundry-x/re-client/internal/pkg/stats"
	"team/foundry-x/re-client/internal/pkg/subprocess"
	"team/foundry-x/re-client/pkg/inputprocessor"
	"team/foundry-x/re-client/pkg/version"

	"cloud.google.com/go/profiler"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/client"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/rexec"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/filemetadata"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "team/foundry-x/re-client/api/proxy"

	rflags "github.com/bazelbuild/remote-apis-sdks/go/pkg/flags"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/moreflag"
	log "github.com/golang/glog"
)

var (
	homeDir, _ = os.UserHomeDir()
	labels     = make(map[string]string)
	start      = time.Now()
)

var (
	proxyLogDir                []string
	clangDepScanIgnoredPlugins []string
	serverAddr                 = flag.String("server_address", "127.0.0.1:8000", "The server address in the format of host:port for network, or unix:///file for unix domain sockets.")
	logFormat                  = flag.String("log_format", "reducedtext", "Format of proxy log. Currently only text and reducedtext are supported. Defaults to reducedtext.")
	logPath                    = flag.String("log_path", "", "DEPRECATED. Use proxy_log_dir instead. If provided, the path to a log file of all executed records. The format is e.g. text://full/file/path.")
	mismatchIgnoreConfigPath   = flag.String("mismatch_ignore_config_path", "", "If provided, mismatches will be ignored according to the provided rule config.")
	enableDepsCache            = flag.Bool("enable_deps_cache", false, "Enables the deps cache if --cache_dir is provided")
	cacheDir                   = flag.String("cache_dir", "", "Directory from which to load the cache files at startup and update at shutdown.")
	keepRecords                = flag.Int("num_records_to_keep", 0, "The number of last executed records to keep in memory for serving.")
	// TODO(b/157446611): remove this flag.
	_                     = flag.String("cpp_dependency_scanner_plugin", "", "Deprecated: Location of the CPP dependency scanner plugin.")
	localResourceFraction = flag.Float64("local_resource_fraction", 1, "Number [0,1] indicating how much of the local machine resources are available for local execution, 1 being all of the machine's CPUs and RAM, 0 being no resources available for local execution.")
	cacheSilo             = flag.String("cache_silo", "", "Cache silo key to be used for all the actions. Usually used to segregate cache-hits between various builds.")
	versionCacheSilo      = flag.Bool("version_cache_silo", false, "Indicates whether to add a re-client version as cache-silo key to all remotely-executed actions. Not applicable for actions run in local-execution-remote-cache (LERC) mode.")
	remoteDisabled        = flag.Bool("remote_disabled", false, "Whether to disable all remote operations and run all actions locally.")
	dumpInputTree         = flag.Bool("dump_input_tree", false, "Whether to dump the input tree of received actions to the tmp directory.")
	useUnifiedCASOps      = flag.Bool("use_unified_cas_ops", false, "Deprecated: use_unified_uploads/downloads instead. Whether to use the unified uploader / downloader for deduplicating uploads / downloads.")
	useUnifiedUploads     = flag.Bool("use_unified_uploads", false, "Whether to use the unified uploader for deduplicating uploads.")
	uploadBufferSize      = flag.Int("upload_buffer_size", 10000, "Buffer size to flush unified uploader daemon.")
	uploadTickDuration    = flag.Duration("upload_tick_duration", 50*time.Millisecond, "How often to flush unified uploader daemon.")
	useUnifiedDownloads   = flag.Bool("use_unified_downloads", false, "Whether to use the unified downloader for deduplicating downloads.")
	downloadBufferSize    = flag.Int("download_buffer_size", 10000, "Buffer size to flush unified downloader daemon.")
	downloadTickDuration  = flag.Duration("download_tick_duration", 50*time.Millisecond, "How often to flush unified downloader daemon.")
	compressionThreshold  = flag.Int("compression_threshold", -1, "Threshold size in bytes for compressing Bytestream reads or writes. Use a negative value for turning off compression.")
	useBatches            = flag.Bool("use_batches", true, "Use batch operations for relatively small blobs.")
	logKeepDuration       = flag.Duration("log_keep_duration", 24*time.Hour, "Delete all RE logs older than the specified duration on startup.")
	idleTimeout           = flag.Duration("proxy_idle_timeout", 6*time.Hour, "Inactivity period after which the running reproxy process will be killed. Default is 6 hours. When set to 0, idle timeout is disabled.")
	depsCacheMaxMb        = flag.Int("deps_cache_max_mb", 128, "Maximum size of the deps cache file (for goma input processor only).")
	// TODO(b/233275188): remove this flag.
	_                = flag.Duration("ip_reset_min_delay", 3*time.Minute, "Deprecated. The minimum time after the input processor has been reset before it can be reset again. Negative values disable resetting.")
	ipTimeout        = flag.Duration("ip_timeout", 10*time.Minute, "The maximum time to wait for an input processor action. Zero and negative values disable timeout.")
	metricsProject   = flag.String("metrics_project", "", "If set, action and build metrics are exported to Cloud Monitoring in the specified GCP project")
	metricsPrefix    = flag.String("metrics_prefix", "", "Prefix of metrics exported to Cloud Monitoring")
	metricsNamespace = flag.String("metrics_namespace", "", "Namespace of metrics exported to Cloud Monitoring (e.g. RBE project)")
	failEarlyMinActionCount   = flag.Int64("fail_early_min_action_count", 0, "Minimum number of actions received by reproxy before the fail early mechanism can take effect. 0 indicates fail early is disabled.")
	failEarlyMinFallbackRatio = flag.Float64("fail_early_min_fallback_ratio", 0, "Minimum ratio of fallbacks to total actions above which the build terminates early. Ratio is a number in the range [0,1]. 0 indicates fail early is disabled.")
	racingBias                = flag.Float64("racing_bias", 0.75, "Value between [0,1] to indicate how racing manages the tradeoff of saving bandwidth (0) versus speed (1). The default is to prefer speed over bandwidth.")
	racingTmp                 = flag.String("racing_tmp_dir", os.TempDir(), "Directory where reproxy should store temporary outputs during racing mode. This should be on the same device as the exec root for the build.")

	debugPort   = flag.Int("pprof_port", 0, "Enable pprof http server if not zero")
	cpuProfFile = flag.String("pprof_file", "", "Enable cpu pprof if not empty. Will not work on windows as reproxy shutdowns through an uncatchable sigkill.")
	memProfFile = flag.String("pprof_mem_file", "", "Enable memory pprof if not empty. Will not work on windows as reproxy shutdowns through an uncatchable sigkill.")

	profilerService   = flag.String("profiler_service", "", "Service name to associate with profiles uploaded to Cloud Profiling. If unset, Cloud Profiling is disabled.")
	profilerProjectID = flag.String("profiler_project_id", "", "project id used for cloud profiler")

	cppLinkDeepScan = flag.Bool("clang_depscan_archive", false, "Deep scan .a files for dependencies during clang linking")

	depsScannerAddress = flag.String("depsscanner_address", "", "If set, connects to the given address for C++ dependency scanning instead of the internal dependency scanner; a path with the prefix 'exec://' will start the target executable and connect to it. If set to execrel:// the `scandeps_server` binary in the same folder as reproxy will be used.")

	credsFile          = flag.String("creds_file", "", "Path to file where short-lived credentials are stored. If the file includes a token, reproxy will update the token if it refreshes it. Token refresh is only applicable if use_external_auth_token is used.")
	waitForShutdownRPC = flag.Bool("wait_for_shutdown_rpc", false, "If set, will only shutdown after 3 SIGINT signals")
	useCasNg           = flag.Bool("use_casng", false, "Use casng pkg.")
)

func verifyFlags() {
	if *localResourceFraction < 0 || *localResourceFraction > 1 {
		log.Exitf("Invalid local_resource_fraction: %v, want [0,1]", *localResourceFraction)
	}
	if *failEarlyMinActionCount < 0 {
		log.Exitf("Invalid fail_early_min_action_acount: %v, want [0,MaxInt64]", *failEarlyMinActionCount)
	}
	if *failEarlyMinFallbackRatio < 0 {
		log.Exitf("Invalid fail_early_min_fallback_ratio: %v, want [0,1]", *failEarlyMinFallbackRatio)
	}
	if *racingBias < 0 || *racingBias > 1 {
		log.Exitf("Invalid racing_bias: %v, want [0,1]", *racingBias)
	}
	if *failEarlyMinActionCount == 0 && *failEarlyMinFallbackRatio > 0 {
		log.Exitf("fail_early_min_fallback_ratio is set to %v while fail_early_min_action_count is disabled", *failEarlyMinFallbackRatio)
	}
	if *failEarlyMinActionCount > 0 && *failEarlyMinFallbackRatio == 0 {
		log.Exitf("fail_early_min_action_count is set to %v while fail_early_min_fallback_ratio is disabled", *failEarlyMinActionCount)
	}
}

func main() {
	flag.Var((*moreflag.StringListValue)(&proxyLogDir), "proxy_log_dir", "If provided, the directory path to a proxy log file of executed records.")
	flag.StringVar(&filemetadata.XattrDigestName, "xattr_digest", "", "Extended file attribute to obtain the digest from, if available, formatted as hash/size. If the value contains the hash only, the file size as reported by stat is used.")
	flag.Var((*moreflag.StringListValue)(&clangDepScanIgnoredPlugins), "clang_depscan_ignored_plugins", `Comma-separated list of plugins that should be ignored by clang dependency scanner.
Use this flag if you're using custom llvm build as your toolchain and your llvm plugins cause dependency scanning failures.`)
	flag.Var((*moreflag.StringMapValue)(&labels), "metrics_labels", "Comma-separated key value pairs in the form key=value. This is used to add arbitrary labels to exported metrics.")
	rbeflag.Parse()
	rbeflag.LogAllFlags(0)
	defer log.Flush()
	defer func() {
		pf, err := reproxypid.ReadFile(*serverAddr)
		if err != nil {
			log.Warningf("Unable to find pid file for deletion: %v", err)
			return
		}
		pf.Delete()
	}()
	if *depsScannerAddress != "" {
		if *depsScannerAddress == "execrel://" {
			scandepsServerPath, err := pathtranslator.BinaryRelToAbs("scandeps_server")
			if err != nil {
				log.Fatalf("Specified --depsscanner_address=execrel:// but `scandeps_server` was not found in the same directory as `reproxy`: %v", err)
			}
			*depsScannerAddress = "exec://" + scandepsServerPath
		}
		cppdependencyscanner.UseGomaDepsScannerService = true
		// If the depsscanner crashes or times out, all actions in flight will be counted as
		// timeouts.  Therefore we bump the number allowed to account for multiple fallbacks from
		// one failure.
		// There will be at most NumCPU actions at any given time; this gives us approximately
		// two failures before aborting the build on the third.
		reproxy.AllowedIPTimeouts += int64(runtime.NumCPU() * 2)
	} else if cppdependencyscanner.IsStub() {
		log.Fatalf("--depsscanner_address must be specified")
	}
	log.Infof("IncludeScanner = %v", cppdependencyscanner.Name())
	log.Flush()
	version.PrintAndExitOnVersionFlag(true)
	verifyFlags()

	if *profilerService != "" {
		log.Infof("Enable cloud profiler: service=%s project=%s", *profilerService, *profilerProjectID)
		err := profiler.Start(profiler.Config{
			Service:        *profilerService,
			ServiceVersion: version.CurrentVersion(),
			MutexProfiling: true,
			ProjectID:      *profilerProjectID,
		})
		if err != nil {
			log.Errorf("Failed to start cloud profiler: %v", err)
		}
	}

	if *debugPort > 0 {
		go func() {
			addr := fmt.Sprintf("127.0.0.1:%d", *debugPort)
			log.Infof("start http server for pprof at %s", addr)
			log.Exit(http.ListenAndServe(addr, nil))
		}()
	} else {
		if *cpuProfFile != "" {
			f, err := os.Create(*cpuProfFile)
			if err != nil {
				log.Fatal("Could not create CPU profile: ", err)
			}
			defer f.Close()
			if err := pprof.StartCPUProfile(f); err != nil {
				log.Fatal("Could not start CPU profile: ", err)
			}
		}
		if *memProfFile != "" {
			f, err := os.Create(*memProfFile)
			if err != nil {
				log.Fatal("Could not create memory profile: ", err)
			}
			defer f.Close()
			if err := pprof.WriteHeapProfile(f); err != nil {
				log.Fatal("Could not start memory profile: ", err)
			}
		}
	}

	listener, err := ipc.Listen(*serverAddr)
	if err != nil {
		log.Exitf("Failed to listen: %v", err)
	}

	logDir := getLogDir()
	var opts []grpc.ServerOption
	truncateInterceptor := interceptors.NewTruncInterceptor(ipc.GrpcMaxMsgSize, logDir)
	opts = append(
		opts,
		grpc.ChainUnaryInterceptor(interceptors.UnaryServerInterceptor, truncateInterceptor),
		grpc.StreamInterceptor(interceptors.StreamServerInterceptor),
	)
	grpcServer := grpc.NewServer(opts...)

	ctx := context.Background()
	var c *auth.Credentials
	if !*remoteDisabled {
		m, err := auth.MechanismFromFlags()
		if err != nil || m == auth.Unknown {
			log.Errorf("Failed to determine auth mechanism: %v", err)
			os.Exit(auth.ExitCodeNoAuth)
		}
		c = mustBuildCredentials(m)
		defer c.SaveToDisk()
	}
	var e *monitoring.Exporter
	var exportActionMetrics logger.ExportActionMetricsFunc
	if *metricsProject != "" {
		e, err = newExporter(c)
		if err != nil {
			log.Warningf("Failed to initialize cloud monitoring: %v", err)
		} else {
			exportActionMetrics = e.ExportActionMetrics
			defer e.Close()
		}
	}
	mi, err := ignoremismatch.New(*mismatchIgnoreConfigPath)
	if err != nil {
		log.Errorf("Failed to create mismatch ignorer: %v", err)
	}
	l, err := initializeLogger(mi, exportActionMetrics)
	if err != nil {
		log.Exitf("%v", err)
	}

	st := filemetadata.NewSingleFlightCache()

	exec := &subprocess.SystemExecutor{}
	resMgr := localresources.NewFractionalDefaultManager(*localResourceFraction)

	initCtx, cancelInit := context.WithCancel(ctx)
	server := &reproxy.Server{
		FileMetadataStore:         st,
		LocalPool:                 reproxy.NewLocalPool(exec, resMgr),
		KeepLastRecords:           *keepRecords,
		CacheSilo:                 *cacheSilo,
		VersionCacheSilo:          *versionCacheSilo,
		RemoteDisabled:            *remoteDisabled,
		DumpInputTree:             *dumpInputTree,
		Forecast:                  &reproxy.Forecast{},
		StartTime:                 start,
		FailEarlyMinActionCount:   *failEarlyMinActionCount,
		FailEarlyMinFallbackRatio: *failEarlyMinFallbackRatio,
		RacingBias:                *racingBias,
		RacingTmp:                 *racingTmp,
		MaxHoldoff:                time.Minute,
		Logger:                    l,
		StartupCancelFn:           cancelInit,
	}
	server.Init()

	ipOpts := &inputprocessor.Options{
		CacheDir:                    *cacheDir,
		EnableDepsCache:             *enableDepsCache,
		LogDir:                      logDir,
		DepsCacheMaxMb:              *depsCacheMaxMb,
		ClangDepsScanIgnoredPlugins: clangDepScanIgnoredPlugins,
		CppLinkDeepScan:             *cppLinkDeepScan,
		IPTimeout:                   *ipTimeout,
		DepsScannerAddress:          *depsScannerAddress,
		ProxyServerAddress:          *serverAddr,
	}
	go func() {
		log.Infof("Setting up input processor")
		ip, cleanup, err := inputprocessor.NewInputProcessor(initCtx, exec, resMgr, st, l, ipOpts)
		if err != nil {
			log.Errorf("Failed to initialize input processor: %+v", err)
			server.SetStartupErr(status.Error(codes.Internal, err.Error()))
			cancelInit()
		} else {
			log.Infof("Finished setting up input processor")
			server.SetInputProcessor(ip, cleanup)
		}
	}()

	if *remoteDisabled {
		server.SetREClient(&rexec.Client{st, nil}, func() {})
	} else {
		// Backward compatibility until useUnifiedCASOps is deprecated:
		if *useUnifiedCASOps {
			*useUnifiedUploads = true
			*useUnifiedDownloads = true
		}
		clientOpts := []client.Opt{
			client.UnifiedUploads(*useUnifiedUploads),
			client.UnifiedUploadBufferSize(*uploadBufferSize),
			client.UnifiedUploadTickDuration(*uploadTickDuration),
			client.UnifiedDownloads(*useUnifiedDownloads),
			client.UnifiedDownloadBufferSize(*downloadBufferSize),
			client.UnifiedDownloadTickDuration(*downloadTickDuration),
			client.UseBatchOps(*useBatches),
			client.CompressedBytestreamThreshold(*compressionThreshold),
			client.UseCASNG(*useCasNg),
		}
		if ts := c.TokenSource(); ts != nil {
			clientOpts = append(clientOpts, &client.PerRPCCreds{Creds: ts})
		}
		go func() {
			log.Infof("Creating a new SDK client")
			grpcClient, err := rflags.NewClientFromFlags(initCtx, clientOpts...)
			if err != nil {
				log.Errorf("Failed to initialize SDK client: %+v", err)
				if ce, ok := err.(*client.InitError); ok {
					err = formatAuthError(c.Mechanism(), ce)
				}
				server.SetStartupErr(err)
				cancelInit()
			} else {
				log.Infof("Finished setting up SDK client")
				server.SetREClient(&rexec.Client{st, grpcClient}, func() { grpcClient.Close() })
			}
		}()
	}
	go server.Forecast.Run(ctx)
	go server.MonitorFailBuildConditions(ctx)
	go reproxy.IdleTimeout(ctx, *idleTimeout)
	// Log all reproxy flags.
	if server.Logger != nil {
		server.Logger.AddFlags(flag.CommandLine)
	} else {
		log.Warningf("nil logger pointer")
	}
	// Delete old logs in the background.
	go reproxy.DeleteOldLogFiles(*logKeepDuration, logDir)
	pb.RegisterCommandsServer(grpcServer, server)
	pb.RegisterStatsServer(grpcServer, server)
	pb.RegisterStatusServer(grpcServer, l)
	log.Infof("RE proxy server listening on %s://%s", listener.Addr().Network(), listener.Addr().String())
	log.Flush()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	wg := sync.WaitGroup{}
	wg.Add(2)
	go func() {
		sigCnt := 1
		if *waitForShutdownRPC {
			sigCnt = 3
		}
		for sigCnt > 0 {
			select {
			case sig := <-sigs:
				sigCnt--
				server.DrainAndReleaseResources() // Start draining server immediately while waiting for Shutdown rpc
				if sigCnt > 0 {
					log.Infof("RE proxy server received %v signal, waiting for Shutdown rpc or %d more signals...", sig, sigCnt)
				} else {
					log.Infof("RE proxy server received %v signal, shutting down...", sig)
				}
			case <-server.WaitForShutdownCommand():
				sigCnt = 0
				log.Infof("RE proxy server received a Shutdown RPC call, shutting down...")
			}
		}
		if *cpuProfFile != "" {
			pprof.StopCPUProfile()
		}
		grpcServer.GracefulStop()
		<-server.WaitForCleanupDone()
		log.Infof("Finished shutting down and wrote log records...")
		log.Flush()
		wg.Done()
	}()
	go func() {
		grpcServer.Serve(listener)
		wg.Done()
	}()
	wg.Wait()
}

func formatAuthError(m auth.Mechanism, ce *client.InitError) error {
	if errors.Is(ce.Err, context.Canceled) {
		return ce.Err
	}
	errMsg := "Unable to authenticate with RBE"
	switch ce.AuthUsed {
	case client.ExternalTokenAuth:
		errMsg += ", externally provided auth token was invalid"
	case client.ApplicationDefaultCredsAuth:
		errMsg += ", try restarting the build after running the following command:\n"
		errMsg += "    gcloud auth application-default login --disable-quota-project\n"
		errMsg += "If this is a headless machine, use:\n"
		errMsg += "    gcloud auth application-default login --no-launch-browser --disable-quota-project"
	}
	return status.Errorf(codes.Unauthenticated, errMsg+"\n%s", ce.Error())
}

// mustBuildCredentials either returns a valid auth.Credentials struct or exits
func mustBuildCredentials(m auth.Mechanism) *auth.Credentials {
	c, err := auth.NewCredentials(m, *credsFile, 0)
	if err != nil {
		log.Errorf("Failed to initialize credentials: %v", err)
		if aerr, ok := err.(*auth.Error); ok {
			os.Exit(aerr.ExitCode)
		}
		os.Exit(auth.ExitCodeUnknown)
	}
	return c
}

func initializeLogger(mi *ignoremismatch.MismatchIgnorer, e logger.ExportActionMetricsFunc) (*logger.Logger, error) {
	if len(proxyLogDir) > 0 {
		format, err := logger.ParseFormat(*logFormat)
		if err != nil {
			return nil, fmt.Errorf("error initializing logger: %v", err)
		}
		l, err := logger.New(format, proxyLogDir[0], cppdependencyscanner.Name(), stats.New(), mi, e)
		if err != nil {
			return nil, fmt.Errorf("error initializing logger: %v", err)
		}
		return l, nil
	}

	if *logPath != "" {
		l, err := logger.NewFromFormatFile(*logPath, cppdependencyscanner.Name(), stats.New(), mi, e)
		if err != nil {
			return nil, fmt.Errorf("error initializing log file %v: %v", *logPath, err)
		}
		return l, nil
	}
	return nil, nil
}

func newExporter(creds *auth.Credentials) (*monitoring.Exporter, error) {
	if err := monitoring.SetupViews(labels); err != nil {
		return nil, err
	}
	return monitoring.NewExporter(context.Background(), *metricsProject, *metricsPrefix, *metricsNamespace, creds.TokenSource())
}

func getLogDir() string {
	if len(proxyLogDir) > 0 {
		return proxyLogDir[0]
	}

	if f := flag.Lookup("log_dir"); f != nil && f.Value.String() != "" {
		return f.Value.String()
	}
	return os.TempDir()
}
