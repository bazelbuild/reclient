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

// Package tabulator is used to store experiment results into a BigQuery table
package tabulator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"team/foundry-x/re-client/experiments/internal/pkg/gcs"

	"cloud.google.com/go/bigquery"
	"google.golang.org/protobuf/encoding/prototext"

	spb "team/foundry-x/re-client/api/stats"
	epb "team/foundry-x/re-client/experiments/api/experiment"

	log "github.com/golang/glog"
)

const (
	dataset = "results"
	table   = "results"
)

// RunExperimentResultCollector stores an experiment's results into a BigQuery table for
// easy querying.
func RunExperimentResultCollector(resBucket string, expName string,
	gcpProject string) error {
	ctx := context.Background()
	configs, err := gcs.List(ctx, fmt.Sprintf("gs://%v/%v", resBucket, expName))
	if err != nil {
		return fmt.Errorf("Failed to find directory for experiment %v: %v", expName, err)
	}
	exp := &epb.Results{Name: expName}
	for _, c := range configs {
		if strings.HasSuffix(c, "textproto") {
			exp.ConfigUrl = c
			continue
		}
		exp.ConfigResults = append(exp.ConfigResults, getConfig(ctx, c))
	}
	client, err := bigquery.NewClient(ctx, gcpProject)
	if err != nil {
		return fmt.Errorf("Failed to create bigquery client: %v", err)
	}
	schema, err := bigquery.InferSchema(epb.Results{})
	if err != nil {
		return fmt.Errorf("Failed to generate schema: %v", err)
	}
	schema = schema.Relax()
	t := client.Dataset(dataset).Table(table)
	if _, err := t.Metadata(ctx); err != nil {
		if err := t.Create(ctx, &bigquery.TableMetadata{Schema: schema}); err != nil {
			return fmt.Errorf("Failed to create table: %v", err)
		}
	}
	if err := t.Uploader().Put(ctx, exp); err != nil {
		return fmt.Errorf("Failed to insert experiment: %v", err)
		if multiError, ok := err.(bigquery.PutMultiError); ok {
			for _, err1 := range multiError {
				for _, err2 := range err1.Errors {
					return fmt.Errorf("Failed to insert: %v", err2)
				}
			}
		} else {
			return fmt.Errorf("Failed to insert: %v", err)
		}
	}
	fmt.Printf("Results uploaded to bigquery. You can query bigquery as follows: \n"+
		"SELECT * FROM `%v.%v.%v` WHERE name='%v'\n"+
		"Or in plx as follows:\n"+
		"SET bigquery_billing_project = '%v';\n"+
		"SELECT c.name, AVG(d) as avg_duration\n"+
		"FROM `%v.%v.%v` r, UNNEST(ConfigResults) c, UNNEST(c.durations) d\n"+
		"WHERE r.Name = '%v'\n"+
		"GROUP BY 1;\n", gcpProject, dataset, table, expName, gcpProject, gcpProject, dataset, table, expName)
	return nil
}

func getConfig(ctx context.Context, path string) *epb.ConfigurationResult {
	trials, err := gcs.List(ctx, path)
	if err != nil {
		log.Warningf("Failed to find trials for experiment %v: %v", path, err)
	}
	cr := &epb.ConfigurationResult{Name: filepath.Base(path)}
	log.Infof("Trials: %v", trials)
	for _, t := range trials {
		if err := gcs.Copy(ctx, fmt.Sprintf("%vtime.txt", t), "/tmp/time.txt"); err != nil {
			log.Warningf("Couldn't find elapsed time for %v: %v", t, err)
			continue
		}
		dur, err := os.ReadFile("/tmp/time.txt")
		if err != nil {
			log.Warningf("Couldn't read elapsed time from /tmp/time.txt: %v", err)
			continue
		}
		d, err := strconv.ParseInt(strings.Trim(string(dur), "s\n"), 10, 64)
		if err != nil {
			log.Warningf("Couldn't parse duration in %v: %v", string(dur), err)
			continue
		}
		cr.Durations = append(cr.Durations, d)
		if err := gcs.Copy(ctx, fmt.Sprintf("%vrbe_metrics.txt", t), "/tmp/rbe_metrics.txt"); err != nil {
			log.Warningf("Couldn't find RBE metrics for %v: %v", t, err)
			continue
		}
		data, err := os.ReadFile("/tmp/rbe_metrics.txt")
		if err != nil {
			log.Warningf("Couldn't read RBE metrics for %v: %v", t, err)
			continue
		}
		stats := &spb.Stats{}
		if err := prototext.Unmarshal(data, stats); err != nil {
			log.Warningf("Couldn't unmarshal RBE metrics for %v: %v", t, err)
			continue
		}
		cr.Stats = append(cr.Stats, &epb.Stats{
			NumRecords:   stats.NumRecords,
			Stats:        stats.Stats,
			ToolVersion:  stats.ToolVersion,
			Verification: stats.Verification,
		})
	}
	return cr
}
