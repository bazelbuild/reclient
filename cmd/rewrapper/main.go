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

// Main package for the rewrapper binary.
//
// The rewrapper is used as a prefix to a build/test command that should be remoted to RBE. The
// rewrapper works as follows:
//
// rewrapper --labels=type=compile,lang=cpp --command_id=12345 --exec_strategy=remote -- \
//
//	clang -c test.c -o test.o
//
// In the above example, the clang command is packaged as a request to a long running proxy server
// which receives the command and returns its result. The result could be obtained via remote
// execution, local execution, or cache.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"team/foundry-x/re-client/internal/pkg/ipc"
	"team/foundry-x/re-client/internal/pkg/protoencoding"
	"team/foundry-x/re-client/internal/pkg/rbeflag"
	"team/foundry-x/re-client/internal/pkg/rewrapper"
	"team/foundry-x/re-client/pkg/version"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/moreflag"

	pb "team/foundry-x/re-client/api/proxy"

	log "github.com/golang/glog"
)

var (
	cOpts       = &rewrapper.CommandOptions{StartTime: time.Now()}
	serverAddr  = "127.0.0.1:8000"
	dialTimeout *time.Duration

	execStrategies = []string{"local", "remote", "remote_local_fallback", "racing"}
)

func initFlags() {
	flag.StringVar(&serverAddr, "server_address", "127.0.0.1:8000", "The server address in the format of host:port for network, or unix:///file for unix domain sockets.")
	flag.StringVar(&cOpts.CommandID, "command_id", "", "An identifier for the command for use in future debugging")
	flag.StringVar(&cOpts.InvocationID, "invocation_id", "", "An identifier for a group of commands for use in future debugging")
	flag.StringVar(&cOpts.ToolName, "tool_name", "", "The name of the tool to associate with executed commands")
	flag.Var((*moreflag.StringMapValue)(&cOpts.Labels), "labels", "Comma-separated key value pairs in the form key=value. This is used to identify the type of command to help the proxy make decisions regarding remote execution. Defaults to type=tool.")
	flag.StringVar(&cOpts.ExecRoot, "exec_root", "", "The exec root of the command. The path from which all inputs and outputs are defined relatively. Defaults to current working directory.")
	flag.DurationVar(&cOpts.ExecTimeout, "exec_timeout", time.Hour, "Timeout for the command on RBE. Default is 1 hour.")
	flag.DurationVar(&cOpts.ReclientTimeout, "reclient_timeout", time.Hour, "Timeout for remotely executed actions to wait for a response from RBE. Default is 1 hour.")
	flag.Var((*moreflag.StringMapValue)(&cOpts.Platform), "platform", "Comma-separated key value pairs in the form key=value. This is used to identify remote platform settings like the docker image to use to run the command.")
	flag.Var((*moreflag.StringListValue)(&cOpts.EnvVarAllowlist), "env_var_allowlist", "List of environment variables allowed to pass to the proxy.")
	flag.Var((*moreflag.StringListValue)(&cOpts.Inputs), "inputs", "Comma-separated command input paths, relative to exec root. Each path may be either a file or a directory.")
	flag.Var((*moreflag.StringListValue)(&cOpts.ToolchainInputs), "toolchain_inputs", "Comma-separated command toolchain inputs relative to the exec root, which are paths to binaries needed to execute the action. Each binary can have a <binary>_remote_toolchain_inputs file next to it to refer to all dependencies of the toolchain binary. Paths in the <binary>_remote_toolchain_inputs file should be normalized.")
	flag.Var((*moreflag.StringListValue)(&cOpts.InputListPaths), "input_list_paths", "Comma-separated paths to files containing lists of inputs (rsp files). Used when inputs are too long to add to the command line. Paths contained in this file should be relative to the exec_root.")
	flag.Var((*moreflag.StringListValue)(&cOpts.OutputListPaths), "output_list_paths", "Comma-separated paths to files containing lists of outputs (rsp files). Used when outputs are too long to add to the command line. Paths contained in this file should be relative to the exec_root.")
	flag.Var((*moreflag.StringListValue)(&cOpts.OutputFiles), "output_files", "Comma-separated command output file paths, relative to exec root.")
	flag.Var((*moreflag.StringListValue)(&cOpts.OutputDirectories), "output_directories", "Comma-separated command output directory paths, relative to exec root.")
	flag.StringVar(&cOpts.ExecStrategy, "exec_strategy", "remote", fmt.Sprintf("one of %s. Defaults to remote.", execStrategies))
	flag.BoolVar(&cOpts.Compare, "compare", false, "Boolean indicating whether to compare chosen exec strategy with local execution. Default is false.")
	flag.IntVar(&cOpts.NumRetriesIfMismatched, "num_retries_if_mismatched", 0, "Deprecated: Number of times the action should be remotely executed to identify determinism. Used only when compare is set to true.")
	flag.IntVar(&cOpts.NumLocalReruns, "num_local_reruns", 0, "Number of times the action should be rerun locally.")
	flag.IntVar(&cOpts.NumRemoteReruns, "num_remote_reruns", 0, "Number of times the action should be rerun remotely.")
	flag.BoolVar(&cOpts.RemoteAcceptCache, "remote_accept_cache", true, "Boolean indicating whether to accept remote cache hits. Default is true.")
	flag.BoolVar(&cOpts.RemoteUpdateCache, "remote_update_cache", true, "Boolean indicating whether to cache the command result remotely. Default is true.")
	flag.BoolVar(&cOpts.DownloadOutputs, "download_outputs", true, "Boolean indicating whether to download outputs after the command is executed. Default is true.")
	flag.BoolVar(&cOpts.LogEnvironment, "log_env", false, "Boolean indicating whether to pass the entire environment of the rewrapper to the reproxy for logging. Default is false.")
	flag.BoolVar(&cOpts.PreserveUnchangedOutputMtime, "preserve_unchanged_output_mtime", false, "Boolean indicating whether or not to preserve mtimes of unchanged outputs when they are downloaded. Default is false.")
	flag.StringVar(&cOpts.LocalWrapper, "local_wrapper", "", "Wrapper path to execute locally only.")
	flag.StringVar(&cOpts.RemoteWrapper, "remote_wrapper", "", "Wrapper path to execute on remote worker.")
	dialTimeout = flag.Duration("dial_timeout", 3*time.Minute, "Timeout for dialing reproxy. Default is 3 minutes.")
	flag.BoolVar(&cOpts.PreserveSymlink, "preserve_symlink", false, "Boolean indicating whether to preserve symlinks in input tree. Default is false.")
	flag.BoolVar(&cOpts.CanonicalizeWorkingDir, "canonicalize_working_dir", false, "Replaces local working directory with a canonical value when running on RE server. The feature makes actions working-dir agnostic and enables to cache them across various same depth (e.g. out/default and out/rbe-build) local working directories (default: false)")
	flag.StringVar(&cOpts.ActionLog, "action_log", "", "If set, write a reproxy log entry for this remote action to the named file.")
}

func execStrategyValid() bool {
	for _, s := range execStrategies {
		if cOpts.ExecStrategy == s {
			return true
		}
	}
	return false
}

func main() {
	initFlags()
	version.PrintAndExitOnVersionFlag(false)
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %v [-flags] -- command ...\n", path.Base(os.Args[0]))
		flag.PrintDefaults()
	}
	rbeflag.Parse()
	rbeflag.LogAllFlags(1)
	cmd := flag.Args()
	if len(cmd) == 0 {
		flag.Usage()
		log.Fatal("No command provided")
	}
	if !execStrategyValid() {
		flag.Usage()
		log.Fatalf("No exec_strategy provided, must be one of %v", execStrategies)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *dialTimeout)
	defer cancel()
	conn, err := ipc.DialContext(ctx, serverAddr)
	if err != nil {
		log.Fatalf("Fail to dial %s: %v", serverAddr, err)
	}
	defer conn.Close()

	proxy := pb.NewCommandsClient(conn)
	ctx = context.Background()
	wd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Failed to get current working directory: %v", err)
	}
	if wd == "/proc/self/cwd" {
		wd, err = os.Readlink(wd)
		if err != nil {
			log.Fatalf("Failed to get current true directory: %v", err)
		}
	}
	if cOpts.ExecRoot == "" {
		cOpts.ExecRoot = wd
	}
	if !filepath.IsAbs(cOpts.ExecRoot) {
		cOpts.ExecRoot, err = filepath.Abs(cOpts.ExecRoot)
		if err != nil {
			log.Exitf("Failed to get abs path for exec_root: %v", err)
		}
	}
	if len(cOpts.Labels) == 0 {
		cOpts.Labels = map[string]string{"type": "tool"}
	}
	cOpts.WorkDir, err = filepath.Rel(cOpts.ExecRoot, wd)
	if err != nil {
		log.Fatalf("Failed to compute working directory path relative to the exec root: %v", err)
	}
	if strings.HasPrefix(cOpts.WorkDir, "..") {
		log.Fatalf("Current working directory (%q) is not under the exec root (%q), relative working dir = %q", wd, cOpts.ExecRoot, cOpts.WorkDir)
	}

	if cOpts.PreserveUnchangedOutputMtime && !cOpts.DownloadOutputs {
		log.Fatalf("--preserve_unchanged_output_mtime=true is not compatible with --download_outputs=false.")
	}

	resp, err := rewrapper.RunCommand(ctx, proxy, cmd, cOpts)
	if err != nil {
		// Don't use log.Fatalf to avoid printing a stack trace.
		log.Exitf("Command failed: %v", err)
	}
	if cOpts.ActionLog != "" && resp.ActionLog != nil {
		if err := os.WriteFile(cOpts.ActionLog, []byte(protoencoding.TextWithIndent.Format(resp.ActionLog)), 0644); err != nil {
			log.Errorf("Failed to write reproxy action log %v", cOpts.ActionLog)
		}
	}
	os.Stdout.Write(resp.GetStdout())
	os.Stderr.Write(resp.GetStderr())
	log.Flush()
	os.Exit(int(resp.GetResult().GetExitCode()))
}
