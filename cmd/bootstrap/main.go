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

// Package main bootstraps the reproxy service.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	lpb "team/foundry-x/re-client/api/log"
	spb "team/foundry-x/re-client/api/stats"
	"team/foundry-x/re-client/internal/pkg/auth"
	"team/foundry-x/re-client/internal/pkg/bigquery"
	"team/foundry-x/re-client/internal/pkg/bootstrap"
	"team/foundry-x/re-client/internal/pkg/logger"
	"team/foundry-x/re-client/internal/pkg/monitoring"
	"team/foundry-x/re-client/internal/pkg/rbeflag"
	"team/foundry-x/re-client/internal/pkg/stats"
	"team/foundry-x/re-client/pkg/version"

	cpb "github.com/bazelbuild/remote-apis-sdks/go/api/command"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/moreflag"
	log "github.com/golang/glog"
)

// bootstrapStart saves the start time of the bootstrap binary.
// Must be the first variable to ensure it is the earliest possible timestamp
var bootstrapStart = time.Now()

var (
	homeDir, _   = os.UserHomeDir()
	labels       = make(map[string]string)
	gcertErrMsg  = fmt.Sprintf("\nTry restarting the build after running %q\n", "gcert")
	gcloudErrMsg = fmt.Sprintf("\nTry restarting the build after running %q\n", "gcloud auth login")
	logDir       = os.TempDir()
)

var (
	proxyLogDir          []string
	serverAddr           = flag.String("server_address", "127.0.0.1:8000", "The server address in the format of host:port for network, or unix:///file for unix domain sockets.")
	reProxy              = flag.String("re_proxy", filepath.Join(homeDir, "rbe", bootstrap.Proxyname), "Location of the reproxy binary")
	waitSeconds          = flag.Int("reproxy_wait_seconds", 20, "Number of seconds to wait for reproxy to start")
	shutdown             = flag.Bool("shutdown", false, "Whether to shut down the proxy and dump the stats.")
	shutdownSeconds      = flag.Int("shutdown_seconds", 60, "Number of seconds to wait for reproxy to shutdown")
	logFormat            = flag.String("log_format", "text", "Format of proxy log. Currently only text and reducedtext are supported.")
	logPath              = flag.String("log_path", "", "DEPRECATED. Use proxy_log_dir instead. If provided, the path to a log file of all executed records. The format is e.g. text://full/file/path.")
	fastLogCollection    = flag.Bool("fast_log_collection", false, "Enable optimized log aggregation pipeline. Does not work for multileg builds")
	asyncReproxyShutdown = flag.Bool("async_reproxy_termination", false, "Allows reproxy to finish shutdown asyncronously. Only applicable with fast_log_collection=true")
	metricsProject       = flag.String("metrics_project", "", "If set, action and build metrics are exported to Cloud Monitoring in the specified GCP project")
	metricsPrefix        = flag.String("metrics_prefix", "", "Prefix of metrics exported to Cloud Monitoring")
	metricsNamespace     = flag.String("metrics_namespace", "", "Namespace of metrics exported to Cloud Monitoring (e.g. RBE project)")
	metricsTable         = flag.String("metrics_table", "", "Resource specifier of the BigQuery table to upload the contents of rbe_metrics.pb to. If the project is not provided in the specifier metrics_project will be used.")
	outputDir            = flag.String("output_dir", os.TempDir(), "The location to which stats should be written.")
	useADC               = flag.Bool(auth.UseAppDefaultCredsFlag, false, "Indicates whether to use application default credentials for authentication")
	useGCE               = flag.Bool(auth.UseGCECredsFlag, false, "Indicates whether to use GCE VM credentials for authentication")
	useExternalToken     = flag.Bool(auth.UseExternalTokenFlag, false, "Indicates whether to use an externally provided token for authentication")
	serviceNoAuth        = flag.Bool(auth.ServiceNoAuthFlag, false, "If true, do not authenticate with RBE.")
	credFile             = flag.String(auth.CredentialFileFlag, "", "The name of a file that contains service account credentials to use when calling remote execution. Used only if --use_application_default_credentials and --use_gce_credentials are false.")
	remoteDisabled       = flag.Bool("remote_disabled", false, "Whether to disable all remote operations and run all actions locally.")
	cacheDir             = flag.String("cache_dir", "", "Directory from which to load the cache files at startup and update at shutdown.")
)

func main() {
	defer log.Flush()
	flag.Var((*moreflag.StringListValue)(&proxyLogDir), "proxy_log_dir", "If provided, the directory path to a proxy log file of executed records.")
	flag.Var((*moreflag.StringMapValue)(&labels), "metrics_labels", "Comma-separated key value pairs in the form key=value. This is used to add arbitrary labels to exported metrics.")
	rbeflag.Parse()
	version.PrintAndExitOnVersionFlag(true)

	if !*fastLogCollection && *asyncReproxyShutdown {
		*asyncReproxyShutdown = false
		log.Info("--async_reproxy_termination=true is not compatible with --fast_log_collection=false, falling back to synchronous shutdown.")
	}

	if f := flag.Lookup("log_dir"); f != nil && f.Value.String() != "" {
		logDir = f.Value.String()
	}

	cf, err := credsFilePath()
	if err != nil {
		log.Fatalf("Failed to determine the token cache file name: %v", err)
	}
	var creds *auth.Credentials
	if !*remoteDisabled {
		creds = newCreds(cf)
		creds.SaveToDisk()
	}

	if *shutdown {
		spi := &lpb.ProxyInfo{
			EventTimes: map[string]*cpb.TimeInterval{},
			Metrics:    map[string]*lpb.Metric{},
		}
		s, err := shutdownReproxy()
		if err != nil {
			log.Warningf("Error shutting down reproxy: %v", err)
		}
		if *outputDir == "" {
			log.Fatal("Must provide an output directory.")
		}
		// Fallback on reading the rpl file if no stats are returned from reproxy
		if !*fastLogCollection || s == nil {
			if *logPath == "" && len(proxyLogDir) == 0 {
				return
			}
			log.V(3).Infof("Loading rpl file to generate stats proto")
			recs, pInfos := parseLogs()
			start := time.Now()
			s = stats.NewFromRecords(recs, pInfos).ToProto()
			spi.EventTimes[logger.EventPostBuildAggregateRpl] = command.TimeIntervalToProto(&command.TimeInterval{From: start, To: time.Now()})
		}
		down, up := stats.BandwidthStats(s)
		fmt.Printf("RBE Stats: ↓ %v, ↑ %v, %v\n", down, up, stats.CompletionStats(s))
		spi.EventTimes[logger.EventBootstrapShutdown] = command.TimeIntervalToProto(&command.TimeInterval{
			From: bootstrapStart,
			To:   time.Now(),
		})
		if *metricsProject != "" {
			start := time.Now()
			var e *monitoring.Exporter
			e, err = newExporter(creds)
			if err != nil {
				log.Warningf("Failed to initialize cloud monitoring: %v", err)
			} else {
				e.ExportBuildMetrics(context.Background(), s, spi.EventTimes[logger.EventBootstrapShutdown])
				defer e.Close()
			}
			spi.EventTimes[logger.EventPostBuildMetricsUpload] = command.TimeIntervalToProto(&command.TimeInterval{From: start, To: time.Now()})
			spi.Metrics[logger.EventPostBuildMetricsUpload] = &lpb.Metric{Value: &lpb.Metric_BoolValue{err == nil}}
		}
		s.ProxyInfo = append(s.ProxyInfo, spi)
		log.Infof("Writing stats to %v", *outputDir)
		if err := stats.WriteStats(s, *outputDir); err != nil {
			log.Errorf("WriteStats(%s) failed: %v", *outputDir, err)
		} else {
			log.Infof("Stats dumped successfully.")
		}
		if *metricsTable != "" {
			inserter, cleanup, err := bigquery.NewInserter(context.Background(), *metricsTable, *metricsProject, creds)
			if err != nil {
				log.Warningf("Error creating a bigquery client: %v", err)
				return
			}
			defer cleanup()
			err = inserter.Put(context.Background(), &stats.ProtoSaver{s})
			if err != nil {
				log.Warningf("Error uploading stats to bigquery: %v", err)
				return
			}
		}
		log.Infof("Stats uploaded successfully.")

		return
	}

	monitoring.CleanLogDir(logDir)

	args := []string{}
	if cfg := flag.Lookup("cfg"); cfg != nil {
		if cfg.Value.String() != "" {
			args = append(args, "--cfg="+cfg.Value.String())
		}
	}
	args = append(args, "--creds_file="+cf)

	if *fastLogCollection {
		args = append(args, "--wait_for_shutdown_rpc=true")
	}

	log.V(3).Infof("Trying to authenticate with %s", creds.Mechanism().String())
	currArgs := args[:]
	msg, exitCode := bootstrapReproxy(currArgs, bootstrapStart)
	if exitCode == 0 {
		fmt.Println(msg)
	} else {
		fmt.Fprintf(os.Stderr, "\nReproxy failed to start:%s\nCredentials cache file was deleted. Please try again. If this continues to fail, please file a bug.\n", msg)
		creds.RemoveFromDisk()
	}
	log.Flush()
	os.Exit(exitCode)
}

func shutdownReproxy() (*spb.Stats, error) {
	if *asyncReproxyShutdown {
		// On shutdown we may not want to wait for deps cache to finish writing
		// if we get valid stats back in the RPC
		return bootstrap.ShutdownProxyAsync(*serverAddr, *shutdownSeconds)
	}
	s, err := bootstrap.ShutDownProxy(*serverAddr, *shutdownSeconds)
	if err == nil {
		// Log this only here as ShutdownProxyAsync does not guarentee that reproxy is shutdown
		log.Infof("Reproxy shut down successfully")
	}
	return s, err
}

func bootstrapReproxy(args []string, startTime time.Time) (string, int) {
	if err := bootstrap.StartProxyWithOutput(context.Background(), *serverAddr, *reProxy, *outputDir, *waitSeconds, *shutdownSeconds, startTime, args...); err != nil {
		defaultErr := fmt.Sprintf("Error bootstrapping remote execution proxy: %v", err)
		exiterr, ok := err.(*exec.ExitError)
		if !ok {
			reproxyExecutionError := fmt.Sprintf(
				"%s. \n\033[31mHint: Unable to execute the reproxy binary at %v. "+
					"Please ensure that the specified path is valid and you have "+
					"the necessary permissions to execute the reproxy binary in that location.\033[0m\n",
				defaultErr, *reProxy,
			)
			log.Exitf(reproxyExecutionError)
		}

		status, ok := exiterr.Sys().(syscall.WaitStatus)
		if !ok {
			log.Exitf(defaultErr)
		}
		return defaultErr, status.ExitStatus()
	}
	return "Proxy started successfully.", 0
}

func newExporter(creds *auth.Credentials) (*monitoring.Exporter, error) {
	if err := monitoring.SetupViews(labels); err != nil {
		return nil, err
	}
	return monitoring.NewExporter(context.Background(), *metricsProject, *metricsPrefix, *metricsNamespace, *remoteDisabled, logDir, creds)
}

func credsFilePath() (string, error) {
	dir := os.TempDir()
	if *cacheDir != "" {
		dir = *cacheDir
	}
	cf := filepath.Join(dir, "reproxy.creds")
	err := os.MkdirAll(filepath.Dir(cf), 0755)
	if err != nil {
		return "", fmt.Errorf("failed to create dir for credentials file %q: %v", cf, err)
	}
	return cf, nil
}

func newCreds(cf string) *auth.Credentials {
	m, err := auth.MechanismFromFlags()
	if err != nil || m == auth.Unknown {
		log.Errorf("Failed to determine auth mechanism: %v", err)
		os.Exit(auth.ExitCodeNoAuth)
	}
	c, err := auth.NewCredentials(m, cf, 0)
	if err != nil {
		log.Fatalf("Failed to initialize credentials: %v", err)
	}
	return c
}

func parseLogs() ([]*lpb.LogRecord, []*lpb.ProxyInfo) {
	var recs []*lpb.LogRecord
	var pInfos []*lpb.ProxyInfo
	var err error
	if len(proxyLogDir) > 0 {
		log.Infof("Parsing logs from %v...", proxyLogDir)
		format, err := logger.ParseFormat(*logFormat)
		if err != nil {
			log.Errorf("ParseFormat(%v) failed: %v", *logFormat, err)
		} else if recs, pInfos, err = logger.ParseFromLogDirs(format, proxyLogDir); err != nil {
			log.Errorf("ParseFromLogDirs failed: %v", err)
		}
	} else {
		log.Infof("Parsing logs from %v...", *logPath)
		if recs, err = logger.ParseFromFormatFile(*logPath); err != nil {
			log.Errorf("ParseFromFormatFile(%s) failed: %v", *logPath, err)
		}
	}
	return recs, pInfos
}
