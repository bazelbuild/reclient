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

// Package main uploads reproxy build level metrics to cloudmonitoring and bigquery.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/bazelbuild/reclient/internal/pkg/bigquery"
	"github.com/bazelbuild/reclient/internal/pkg/collectlogfiles"
	"github.com/bazelbuild/reclient/internal/pkg/monitoring"
	"github.com/bazelbuild/reclient/internal/pkg/rbeflag"
	"github.com/bazelbuild/reclient/internal/pkg/stats"
	"github.com/bazelbuild/reclient/pkg/version"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/client"
	rflags "github.com/bazelbuild/remote-apis-sdks/go/pkg/flags"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/moreflag"
	log "github.com/golang/glog"
	"golang.org/x/oauth2"
	"google.golang.org/grpc/credentials/oauth"
	"google.golang.org/protobuf/proto"

	spb "github.com/bazelbuild/reclient/api/stats"
)

var (
	labels               = make(map[string]string)
	proxyLogDir          []string
	logPath              = flag.String("log_path", "", "DEPRECATED. Use proxy_log_dir instead. If provided, the path to a log file of all executed records. The format is e.g. text://full/file/path.")
	metricsProject       = flag.String("metrics_project", "", "If set, action and build metrics are exported to Cloud Monitoring in the specified GCP project.")
	metricsPrefix        = flag.String("metrics_prefix", "", "Prefix of metrics exported to Cloud Monitoring.")
	metricsNamespace     = flag.String("metrics_namespace", "", "Namespace of metrics exported to Cloud Monitoring (e.g. RBE project).")
	metricsTable         = flag.String("metrics_table", "", "Resource specifier of the BigQuery table to upload the contents of rbe_metrics.pb to. If the project is not provided in the specifier metrics_project will be used.")
	outputDir            = flag.String("output_dir", os.TempDir(), "The location to which stats should be written.")
	useCasNg             = flag.Bool("use_casng", false, "Use casng pkg.")
	compressionThreshold = flag.Int("compression_threshold", -1, "Threshold size in bytes for compressing Bytestream reads or writes. Use a negative value for turning off compression.")
	useBatches           = flag.Bool("use_batches", true, "Use batch operations for relatively small blobs.")
	uploadBufferSize     = flag.Int("upload_buffer_size", 10000, "Buffer size to flush unified uploader daemon.")
	uploadTickDuration   = flag.Duration("upload_tick_duration", 50*time.Millisecond, "How often to flush unified uploader daemon.")
	oauthToken           = flag.String("oauth_token", "", "Token to use when authenticating with GCP.")
)

func main() {
	defer log.Flush()
	flag.Var((*moreflag.StringMapValue)(&labels), "metrics_labels", "Comma-separated key value pairs in the form key=value. This is used to add arbitrary labels to exported metrics.")
	rbeflag.Parse()
	version.PrintAndExitOnVersionFlag(true)

	if *metricsProject == "" {
		log.Fatalf("--metrics_project is required.")
	}
	ctx := context.Background()
	s := &spb.Stats{}
	if rbeMetricsBytes, err := os.ReadFile(filepath.Join(*outputDir, stats.AggregatedMetricsFileBaseName+".pb")); err != nil {
		log.Fatalf("Error reading %s.pb file: %v", stats.AggregatedMetricsFileBaseName, err)
	} else if err := proto.Unmarshal(rbeMetricsBytes, s); err != nil {
		log.Fatalf("Failed to parse %s.pb: %v", stats.AggregatedMetricsFileBaseName, err)
	}

	var ts *oauth.TokenSource
	if *oauthToken != "" {
		ts = &oauth.TokenSource{
			TokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: *oauthToken}),
		}
	}

	if err := uploadToCloudMonitoring(ctx, s, ts); err != nil {
		log.Errorf("Error uploading to cloud monitoring: %v", err)
	}
	if len(proxyLogDir) > 0 {
		*logPath = ""
	}

	logDirs, err := collectlogfiles.DeduplicateDirs(append(proxyLogDir, getLogDir(), *outputDir))
	if err != nil {
		log.Errorf("Unable to determine log dirs for uploading: %v", err)
	}

	if len(logDirs) > 0 {
		grpcClient, err := connectToRBE(ctx, ts)
		if err != nil {
			log.Errorf("Error connecting to rbe: %v", err)
		}
		defer grpcClient.Close()
		ldgs, err := collectlogfiles.UploadDirsToCas(grpcClient, logDirs, *logPath)
		if err != nil {
			log.Errorf("Error uploading logs to rbe: %v", err)
		}
		s.LogDirectories = ldgs
	}

	err = stats.WriteStats(s, *outputDir)
	if err != nil {
		log.Errorf("Error writing log digests to rbe_metrics files: %v", err)
	}

	if *metricsTable != "" {
		if err := uploadToBigQuery(ctx, s, ts); err != nil {
			log.Errorf("Error uploading to bigquery: %v", err)
		}
	}
}

// getLogDir retrieves the glog log directory
func getLogDir() string {
	if f := flag.Lookup("log_dir"); f != nil {
		return f.Value.String()
	}
	return ""
}

func connectToRBE(ctx context.Context, ts *oauth.TokenSource) (*client.Client, error) {
	clientOpts := []client.Opt{
		client.UnifiedUploadBufferSize(*uploadBufferSize),
		client.UnifiedUploadTickDuration(*uploadTickDuration),
		client.UseBatchOps(*useBatches),
		client.CompressedBytestreamThreshold(*compressionThreshold),
		client.UseCASNG(*useCasNg),
	}
	if ts != nil {
		clientOpts = append(clientOpts, &client.PerRPCCreds{Creds: ts})
	}
	log.Infof("Creating a new SDK client")
	return rflags.NewClientFromFlags(ctx, clientOpts...)
}

func uploadToCloudMonitoring(ctx context.Context, s *spb.Stats, ts *oauth.TokenSource) error {
	if err := monitoring.SetupViews(labels); err != nil {
		return fmt.Errorf("failed to initialize cloud monitoring views: %w", err)
	}
	e, err := monitoring.NewExporter(ctx, *metricsProject, *metricsPrefix, *metricsNamespace, ts)
	if err != nil {
		return fmt.Errorf("failed to initialize cloud monitoring exporter: %w", err)
	}
	defer e.Close()
	e.ExportBuildMetrics(ctx, s)
	return nil
}

func uploadToBigQuery(ctx context.Context, s *spb.Stats, ts *oauth.TokenSource) error {
	inserter, cleanup, err := bigquery.NewInserter(ctx, *metricsTable, *metricsProject, ts)
	if err != nil {
		return fmt.Errorf("error creating a bigquery client: %w", err)
	}
	defer func() {
		err := cleanup()
		if err != nil {
			log.Errorf("Error cleaning up bigquery client: %v", err)
		}
	}()
	err = inserter.Put(ctx, &stats.ProtoSaver{Stats: s})
	if err != nil {
		return fmt.Errorf("error uploading stats to bigquery: %w", err)
	}
	return nil
}
