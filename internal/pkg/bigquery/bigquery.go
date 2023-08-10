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

package bigquery

import (
	"context"
	"fmt"
	"strings"

	"cloud.google.com/go/bigquery"
	"google.golang.org/api/option"
	"google.golang.org/grpc/credentials/oauth"
)

// parseResourceSpec parses spec as per the bq command-line tool format described
// here: https://cloud.google.com/bigquery/docs/reference/bq-cli-reference#resource_specification.
// spec should be in the form <project>:<dataset>.<table> or <dataset>.<table>
func parseResourceSpec(spec, defaultProject string) (project, dataset, table string, err error) {
	project = defaultProject
	if strings.ContainsRune(spec, ':') {
		chunks := strings.Split(spec, ":")
		if len(chunks) != 2 {
			return "", "", "", fmt.Errorf("bigquery resource spec should have form <project>:<dataset>.<table> or <dataset>.<table>, got %s", spec)
		}
		project = chunks[0]
		spec = chunks[1]
	}
	if project == "" {
		return "", "", "", fmt.Errorf("project not specified, either BQ resource spec should have form <project>:<dataset>.<table> or a default project should be specified")
	}
	chunks := strings.Split(spec, ".")
	if len(chunks) != 2 {
		return "", "", "", fmt.Errorf("bigquery resource spec should have form <project>:<dataset>.<table> or <dataset>.<table>, got %s", spec)
	}
	return project, chunks[0], chunks[1], nil
}

// NewInserter creates a an inserter for the table specified by the given resourceSpec in
// the form <project>:<dataset>.<table> or <dataset>.<table>
func NewInserter(ctx context.Context, resourceSpec, defaultProject string, ts *oauth.TokenSource) (inserter *bigquery.Inserter, cleanup func() error, err error) {
	project, dataset, table, err := parseResourceSpec(resourceSpec, defaultProject)
	cleanup = func() error { return nil }
	if err != nil {
		return nil, cleanup, err
	}
	var opts []option.ClientOption
	if ts != nil {
		opts = append(opts, option.WithTokenSource(ts))
	}
	client, err := bigquery.NewClient(ctx, project, opts...)
	if err != nil {
		return nil, cleanup, err
	}
	return client.Dataset(dataset).Table(table).Inserter(), client.Close, nil
}
