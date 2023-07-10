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

// sample sends an echo command to execute remotely.
// Several important reflags are:
// * --echostr: the string to echo
// * --service: the RE service to connect to, for RBE set to: "remotebuildexecution.googleapis.com:443"
// * --instance: the RE instance to use, for testing set to: "projects/foundry-x-experiments/instances/default_instance"
// * --credential_file: the credential file to use, set to something like: "$HOME/.config/foundry/keys/dev-foundry.json"
package main

import (
	"context"
	"flag"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/filemetadata"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/moreflag"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/outerr"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/rexec"

	reflags "github.com/bazelbuild/remote-apis-sdks/go/pkg/flags"
	log "github.com/golang/glog"
)

const (
	// The standard RBE Ubuntu container.
	dockerImage = "docker://gcr.io/cloud-marketplace/google/rbe-ubuntu16-04@sha256:94d7d8552902d228c32c8c148cc13f0effc2b4837757a6e95b73fdc5c5e4b07b"
)

var (
	echoStr = flag.String("echostr", "Hello world", "The string to echo remotely")
)

func main() {
	moreflag.Parse()
	ctx := context.Background()
	grpcClient, err := reflags.NewClientFromFlags(ctx)
	if err != nil {
		log.Exitf("error connecting to remote execution client: %v", err)
	}
	defer grpcClient.Close()
	c := &rexec.Client{
		FileMetadataCache: filemetadata.NewNoopCache(),
		GrpcClient:        grpcClient,
	}

	cmd := &command.Command{
		ExecRoot: ".",
		Args:     []string{"echo", *echoStr},
		Platform: map[string]string{"container-image": dockerImage},
	}
	res, _ := c.Run(ctx, cmd, command.DefaultExecutionOptions(), outerr.SystemOutErr)
	if !res.IsOk() {
		log.Exitf("Error executing action: %v", res)
	}
	log.V(2).Info("Successfully executed action.")
}
