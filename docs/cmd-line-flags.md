# Command-line Flags for re-client binaries

### Reproxy

**`-service (string)`**

Sets the host (and port) of the remote execution service to connect to.

**`-cas_service (string)`**

Sets the host (and port) of the Content Addressable Service to connect to, if it
is different from the remote execution service. If using the Remote Build
Execution service, this will be the same as the remote execution service and
does not need to be set.

**`-instance (string)`**

The instance ID to target when calling remote execution via gRPC
(e.g., projects/$PROJECT/instances/default_instance for Google RBE).

**`-use_application_default_credentials (bool) (from remote-apis-sdks)`**

If true, this indicates that reproxy should use application default credentials
to authenticate with the Remote Execution service. See Notes on credentials and
authentication below.

**`-use_gce_credentials (bool) (from remote-apis-sdks)`**

If true, this indicates that the GCE credentials should be used to authenticate
with the Remote Build Execution service. Use of this flag only works when
reproxy is running on a GCE VM. See Notes on credentials and authentication
below.

**`-credential_file (string) (from remote-apis-sdks)`**

If set, points to a file containing the service account credentials to use to
authenticate with the Remote Build Execution service. See notes on credentials
and authentication below.

**`-server_address (string)`**

The address that reproxy will use to accept actions from rewrapper. Can be
either in host:port format (ex: 127.0.0.1:8000) or unix:///file for unix domain
sockets (ex: unix:///tmp/reproxy.sock).

**`-log_format (string)`**

File format of the proxy logs. Currently only supports 'text'.

**`-log_path (string) (DEPRECATED)`**

If this is set, this is the path to a log file of all executed records. The
format for this flag is 'text://full/file/path'. The `proxy_log_dir` flag should
be used in place of this one.

**`-local_resource_fraction (float)`**

A value between 0 and 1 that indicates how much of the local machine resources
can be used for local execution. A value of 1 means all CPUs are available, and
0 means no CPUs are available.

**`-enable_deps_cache (bool)`**

Enables the deps cache if `-cache_dir` is provided. Default is false.

**`-cache_dir`**

Directory from which to load the cache files at startup and update at shutdown.

**`-cache_silo (string)`**

A cache silo key to use for all actions. Can be used to segregate cache hits.

**`-version_cache_silo (bool)`**

Indicates whether to add a Reclient version as cache-silo key to all
remotely-executed actions. Not applicable for actions run in
local-execution-remote-cache (LERC) mode. Default is false.

**`-remote_disabled (bool)`**

If this is set, then all remote actions are not permitted and everything
executes locally. This includes both remote cache lookups and remote execution
of actions.

**`-dump_input_tree (bool)`**

If true, this will dump the input tree of actions received from rewrapper to
'/tmp'. Not useful unless you are attempting to debug an issue suspected to be
caused by rbe.

**`-use_unified_uploads (bool)`**

Whether to use the unified uploader for deduplicating uploads. Default is false.

**`-upload_buffer_size (int)`**

Buffer size to flush unified uploader daemon. Default is 10000.

**`-upload_tick_duration (duration)`**

How often to flush unified uploader daemon. Default is 50ms.

**`-use_unified_downloads (bool)`**

Whether to use the unified downloader for deduplicating downloads. Default is
false.

**`-download_buffer_size (int)`**

Buffer size to flush unified downloader daemon. Default is 10000.

**`-download_tick_duration (duration)`**

How often to flush unified downloader daemon. Default is 50ms.

**`-compression_threshold (int)`**

Threshold size in bytes for compressing Bytestream reads or writes. Use a
negative value for turning off compression. Default is -1.

**`-use_batches (bool)`**

Use batch operations for relatively small blobs. Default is true.

**`-log_keep_duration (duration)`**

Delete all RE logs older than the specified duration on startup. Default is 24h.

**`-proxy_idle_timeout (duration)`**

Inactivity period after which the running reproxy process will be killed.
Default is 6 hours. When set to 0, idle timeout is disabled.

**`-ip_timeout (duration)`**

The maximum time to wait for an input processor action. Zero and negative values
disable timeout. Default is 10m.

**`-metrics_project (string)`**

If set, action and build metrics are exported to Cloud Monitoring in the
specified GCP project. Default is empty.

**`-metrics_prefix (string)`**

Prefix of metrics exported to Cloud Monitoring. Default is empty.

**`-metrics_namespace (string)`**

Namespace of metrics exported to Cloud Monitoring. Default is empty.

**`-fail_early_min_action_count (int)`**

Minimum number of actions received by reproxy before the fail early mechanism
can take effect. 0 indicates fail early is disabled.

**`-fail_early_min_fallback_ratio (float)`**

Minimum ratio of fallbacks to total actions above which the build terminates
early. Ratio is a number in the range between 0 and 1. 0 indicates fail early is
disabled. Default is 0.

**`-fail_early_window (duration)`**

Window of time to consider for fail_early_min_action_count and
fail_early_min_fallback_ratio. 0 indicates all datapoints should be used.
Default is 0.

**`-racing_bias (float)`**

Value between 0 and 1 to indicate how racing manages the tradeoff of saving
bandwidth (0) versus speed (1). The default is to prefer speed over bandwidth.
Default is 0.75.

**`-racing_tmp_dir (string)`**

Directory where reproxy should store temporary outputs during racing mode. This
should be on the same device as the exec root for the build. Default is
[`os.TempDir()`](https://pkg.go.dev/os#TempDir).

**`-pprof_port (int)`**

"Enable pprof http server if not zero. Default is 0.

**`-pprof_file (path)`**

Enable cpu pprof if not empty. Will not work on windows as reproxy shutdowns
through an uncatchable sigkill. Default is empty.

**`-pprof_mem_file (path)`**

Enable memory pprof if not empty. Will not work on windows as reproxy shutdowns
through an uncatchable sigkill. Default is empty.

**`-profiler_service (string)`**

Service name to associate with profiles uploaded to Cloud Profiling. If unset,
Cloud Profiling is disabled. Default is empty.

**`-profiler_project_id (string)`**

Project id used for cloud profiler. Default is empty.

**`-clang_depscan_archive (bool)`**

Deep scan .a files for dependencies during clang linking. Default is false.

**`-depsscanner_address (string)`**

If set, connects to the given address for C++ dependency scanning instead of the
internal dependency scanner; a path with the prefix 'exec://' will start the
target executable and connect to it. If set to execrel:// the `scandeps_server`
binary in the same folder as reproxy will be used. Default is empty.

**`-creds_file (path)`**

Path to file where short-lived credentials are stored. If the file includes a
token, reproxy will update the token if it refreshes it. Token refresh is only
applicable if `use_external_auth_token` is used. Default is empty.

**`-wait_for_shutdown_rpc (bool)`**

If set, will only shutdown after 3 SIGINT signals. Default is false.

**`-proxy_log_dir (path)`**

If set, this is where reproxy will write a log of executed records.

**`-xattr_digest (string)`**

Extended file attribute to obtain the digest from, if available, formatted as
hash/size. If the value contains the hash only, the file size as reported by
stat is used. Default is empty.

**`clang_depscan_ignored_plugins (comma-separated list of strings)`**

Comma-separated list of plugins that should be ignored by clang dependency
scanner. Use this flag if you're using custom llvm build as your toolchain and
your llvm plugins cause dependency scanning failures. Default is empty.

**`-v (int)`**

Logging verbosity. A higher number means more logging.

### Rewrapper

**`-server_address (string)`**

The address that reproxy will use to accept actions from rewrapper. Can be
either in host:port format (ex: 127.0.0.1:8000) or unix:///file for unix domain
sockets (ex: unix:///tmp/reproxy.sock).

**`-command_id (string)`**

An identifier to place in the command to aid in debugging.

**`-invocation_id (string)`**

An identifier for a group of commands to aid in debugging.

**`-tool_name (string)`**

The name of the tool to associate with executed commands.

**`-labels (comma-separated key-value pairs)`**

A set of key=value comma-separated pairs, used by reproxy to make decisions on
how execution of the command should be handled. See
[labels.go](https://github.com/bazelbuild/reclient/tree/main/internal/pkg/labels/labels.go)
for the current set of keys and their accepted values.

**`-exec_root (path)`**

Absolute path to the root of the source tree for the project being built. All
inputs and outputs are relative to this path.

**`-exec_timeout (duration)`**

The amount of time to allow this command to execute. The default is 1 hour.

**`-platform (comma-separated key-value pairs)`**

A set of key=value comma-separated pairs, used to define the remote platform
settings (such as the docker container) used to execute the command.

**`-env_var_allowlist (comma-separated strings)`**

A list of comma-separated strings that list the environment variables that
should be passed from rewrapper to reproxy.

**`-inputs (comma-separated paths)`**

A comma-separated list of paths, relative to the `exec_root`, to files or
directories that should be included in the upload to the remote worker.

**`-toolchain_inputs (comma-separated paths)`**

A comma-separated list of command toolchain inputs relative to the `exec_root`,
which are paths to binaries needed to execute the action. Each binary can have a
`<binary>_remote_toolchain_inputs` file next to it to refer to all dependencies
of the toolchain binary. Paths in the `<binary>_remote_toolchain_inputs` file
should be normalized, and should have one path per line.

**`-input_list_paths (comma-separated paths)`**

A comma-separated list of paths to input files (rsp files). Useful for cases
where the use of the -input flag produces excessively long command-lines.

**`-output_files (comma-separated file paths)`**

A comma-separated list of paths where output files from the command will be
placed, relative to the `exec_root`.

**`-output_directories (comma-separated directory paths)`**

A comma-separated list of paths where output directories from the command will
be placed, relative to the `exec_root`. Unlike `output_files`, the names of the
files within the directory do not need to be known ahead of time. This is useful
for things like log files which may contain a timestamp as part of the file
name.

**`-exec_strategy (string)`**

One of `local`, `remote`, `remote_local_fallback` or `racing`.

*   `local` - Use Local Execution, Remote Caching (LERC). Checks the remote
    cache for action matches and uses existing results if a match is found. If
    not, execution of the action is performed locally.
*   `remote` - The remote cache is checked for an action match, and uses the
    results if one exists. If not, the action is executed remotely. If the
    action fails remotely, the action is considered a failure.
*   `remote_local_fallback` - Similar to `remote`, but will attempt to retry a
    failing remote action locally.
*   `racing` - If a remote action doesn't complete within a certain amount of
    time and there are sufficient local resources available, will start running
    the action locally as well. The results of the first to finish are used.
    This is intended to improve the build times for short incremental builds one
    might do as a developer.

**`-compare (bool)`**

Compares the chosen execution strategy to local execution. Is used to determine
if remote execution is producing different results from local. Defaults to
false.

**`-remote_accept_cache (bool)`**

Indicates if remote cache hits should be used. Defaults to true.

**`-remote_update_cache (bool)`**

Indicates if the remote cache should be updated with the results of executed
commands. Defaults to true.

**`-download_outputs (bool)`**

Determines if the results of a remote action should be downloaded. Defaults to
true.

**`-log_env (bool)`**

If true, passes the entire environment to reproxy for logging purposes. Defaults
to false.

**`-dial_timeout (duration)`**

Timeout for attempting to dial into reproxy. Default is 3 minutes.

### Bootstrap

**`-server_address (string)`**

The address that reproxy will use to accept actions from rewrapper. Can be
either in host:port format (ex: 127.0.0.1:8000) or unix:///file for unix domain
sockets (ex: unix:///tmp/reproxy.sock).

**`-re_proxy (path)`**

Location of the reproxy binary. The full path including the binary name, not
just the directory that contains it. Defaults to $HOME/rbe/reproxy.

**`-reproxy_wait_seconds (int)`**

Number of seconds to wait for reproxy to start. If reproxy fails to start in the
allotted time, an error is returned. Defaults to 20 seconds.

**`-shutdown (bool)`**

If provided, will shut down reproxy and dump the stats collected to
`rbe_metrics.txt` and `rbe_metrics.pb` files.

**`-log_format (string)`**

Format of proxy log. Currently only text and reducedtext are supported. Defaults
to reducedtext.

**`-log_path (string) (DEPRECATED)`**

If this is set, this is the path to a log file of all executed records. The
format for this flag is 'text://full/file/path'. The `proxy_log_dir` flag should
be used in place of this one.

**`-output_dir (path)`**

Directory where metrics should be written. Defaults to
[`os.TempDir()`](https://pkg.go.dev/os#TempDir).

**`-v (int)`**

Logging verbosity. A higher number means more logging.

### Dumpstats

**`-shutdown_proxy (bool)`**

If set to true, this will shut down reproxy before reading the reproxy log files
(\*.rpl) for generating metrics.

**`-server_address (string)`**

The address that reproxy will use to accept actions from rewrapper. Can be
either in host:port format (ex: 127.0.0.1:8000) or unix:///file for unix domain
sockets (ex: unix:///tmp/reproxy.sock).

**`-log_format (string)`**

Format of proxy log. Currently only text and reducedtext are supported. Defaults
to reducedtext.

**`-log_path (string) (DEPRECATED)`**

If this is set, this is the path to a log file of all executed records. The
format for this flag is 'text://full/file/path'. The `proxy_log_dir` flag should
be used in place of this one.

**`-output_dir (path)`**

Directory where metrics should be written. Defaults to /tmp.

**`-v (int)`**

Logging verbosity. A higher number means more logging.

## Setting flags via environment variables

Any of these flags can be set via an environment variable of the form
`RBE_<flagname>` where `<flagname>` is a flag defined above. For example, you
could set the `server_address` of reproxy with the following:

```
export RBE_server_address=unix:///tmp/reproxy.sock
```

## Notes on credentials and authentication ([source](https://github.com/bazelbuild/remote-apis-sdks/blob/master/go/pkg/flags/flags.go))

The flags `credential_file`, `use_application_default_credentials`, and
`use_gce_credentials` determine the client identity that is used to authenticate
with remote execution. One of the following must be true for client
authentication to work, and they are used in this order of preference:

*   the `use_application_default_credentials` flag must be set to true, or
*   the `use_gce_credentials` must be set to true, or
*   the `credential_file` flag must be set to point to a valid credential file

## Passing variables in build systems

Note that in some build systems not all variables/flags are allowed to be passed
to each binary. For example, in Android, there are certain variables that do not
get passed to rewrapper, but all variables are allowed to be passed to reproxy.
Just something to be aware of!
