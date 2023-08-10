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

	"team/foundry-x/re-client/internal/pkg/bigquery"
	"team/foundry-x/re-client/internal/pkg/monitoring"
	"team/foundry-x/re-client/internal/pkg/rbeflag"
	"team/foundry-x/re-client/internal/pkg/stats"
	"team/foundry-x/re-client/pkg/version"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/moreflag"
	log "github.com/golang/glog"
	"golang.org/x/oauth2"
	"google.golang.org/grpc/credentials/oauth"
	"google.golang.org/protobuf/proto"

	spb "team/foundry-x/re-client/api/stats"
)

var (
	labels           = make(map[string]string)
	metricsProject   = flag.String("metrics_project", "", "If set, action and build metrics are exported to Cloud Monitoring in the specified GCP project.")
	metricsPrefix    = flag.String("metrics_prefix", "", "Prefix of metrics exported to Cloud Monitoring.")
	metricsNamespace = flag.String("metrics_namespace", "", "Namespace of metrics exported to Cloud Monitoring (e.g. RBE project).")
	metricsTable     = flag.String("metrics_table", "", "Resource specifier of the BigQuery table to upload the contents of rbe_metrics.pb to. If the project is not provided in the specifier metrics_project will be used.")
	rbeMetricsPb     = flag.String("rbe_metrics_path", "", "Path to rbe_metrics.pb that will be uploaded to bigquery and parsed for cloud monitoring build level metrics.")
	oauthToken       = flag.String("oauth_token", "", "Token to use when authenticating with GCP.")
)

func main() {
	defer log.Flush()
	flag.Var((*moreflag.StringMapValue)(&labels), "metrics_labels", "Comma-separated key value pairs in the form key=value. This is used to add arbitrary labels to exported metrics.")
	rbeflag.Parse()
	version.PrintAndExitOnVersionFlag(true)

	if *metricsProject == "" {
		log.Fatalf("--metrics_project is required.")
	}
	s := &spb.Stats{}
	if rbeMetricsBytes, err := os.ReadFile(*rbeMetricsPb); err != nil {
		log.Fatalf("Error reading rbe_metrics.pb file: %v", err)
	} else if err := proto.Unmarshal(rbeMetricsBytes, s); err != nil {
		log.Fatalf("Failed to parse rbe_metrics.pb: %v", err)
	}
	err := os.Remove(*rbeMetricsPb)
	if err != nil {
		log.Errorf("Failed to delete rbe_metrics.pb: %v", err)
	}

	var ts *oauth.TokenSource
	if *oauthToken != "" {
		ts = &oauth.TokenSource{
			TokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: *oauthToken}),
		}
	}

	if err := uploadToCloudMonitoring(s, ts); err != nil {
		log.Errorf("Error uploading to cloud monitoring: %v", err)
	}

	if *metricsTable != "" {
		if err := uploadToBigQuery(s, ts); err != nil {
			log.Errorf("Error uploading to bigquery: %v", err)
		}
	}
}

func uploadToCloudMonitoring(s *spb.Stats, ts *oauth.TokenSource) error {
	if err := monitoring.SetupViews(labels); err != nil {
		return fmt.Errorf("failed to initialize cloud monitoring views: %w", err)
	}
	e, err := monitoring.NewExporter(context.Background(), *metricsProject, *metricsPrefix, *metricsNamespace, ts)
	if err != nil {
		return fmt.Errorf("failed to initialize cloud monitoring exporter: %w", err)
	}
	defer e.Close()
	e.ExportBuildMetrics(context.Background(), s)
	return nil
}

func uploadToBigQuery(s *spb.Stats, ts *oauth.TokenSource) error {
	inserter, cleanup, err := bigquery.NewInserter(context.Background(), *metricsTable, *metricsProject, ts)
	if err != nil {
		return fmt.Errorf("error creating a bigquery client: %w", err)
	}
	defer func() {
		err := cleanup()
		if err != nil {
			log.Errorf("Error cleaning up bigquery client: %v", err)
		}
	}()
	err = inserter.Put(context.Background(), &stats.ProtoSaver{Stats: s})
	if err != nil {
		return fmt.Errorf("error uploading stats to bigquery: %w", err)
	}
	return nil
}
