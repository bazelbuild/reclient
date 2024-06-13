# Remote Execution Client

This repository contains a client implementation of
[Remote Execution API](https://github.com/bazelbuild/remote-apis) that works
with
[Remote Execution API SDKs](https://github.com/bazelbuild/remote-apis-sdks).

Reclient integrates with an existing build system to enable remote execution and
caching of build actions.

When used with Server implementation of Remote Execution API, it helps to reduce
the build times by applying 2 main techniques:

1.  Distribution of the load by executing individual build actions in parallel
    on separate remote workers instead of on one build machine so that the build
    actions that are executed in parallel don’t compete for the same local
    resources.
1.  RE Server instance-wide cache for build actions, inputs, and artifacts As a
    consequence, results of a build action that was already executed for exactly
    the same inputs on the same instance of RE Server will be fetched from the
    cache even if the action was never executed on the machine.

Most clients are expected to see the performance improvement of their builds
after migrating from local to remote builds. However, builds with a high number
of deterministic build actions that can be executed in parallel are expected to
see the greatest improvement.

Reclient consists of the following main binaries:

1.  **rewrapper** - a wrapper that forwards build commands to RBE
1.  **reproxy** - a process that should be started at the beginning of the build
    and shut down at the end. It communicates with RBE to execute build actions
    remotely and/or fetch build artifacts from RE Server's CAS (Content
    Addressable Storage).
1.  **bootstrap** - starts and stops reproxy, and aggregates the metrics during
    the shutdown.
1.  **scandeps_server** - a standalone process for scanning includes of C(++)
    compile actions. Started and stopped automatically by reproxy.

## Note

This is not an officially supported Google product.

# Prerequisites

* `re-client` builds with [Bazel](https://bazel.build/). We recommend using
  [Bazelisk](https://github.com/bazelbuild/bazelisk) to use the version of Bazel
  currently supported by this code base.
* `re-client` also requires `gclient`, which can be installed by checking out
  [depot_tools](https://chromium.googlesource.com/chromium/tools/depot_tools/)
  and adding the `depot_tools` directory to your `PATH`.

# Building

`re-client` currently builds and is supported on Linux / Mac / Windows.

Once you've installed Bazel, and are in the re-client repo:

## Build the code

To build a complete set of binaries for reclient with a clangscandeps deps scanner:

```shell
$ bazelisk build --config=clangscandeps //:artifacts_tar
[...]
Target //:artifacts_tar up-to-date:
  bazel-bin/artifacts.tar
```

To build a complete set of binaries for reclient with a goma deps scanner:

```shell
$ bazelisk build --config=goma //:artifacts_tar
[...]
Target //:artifacts_tar up-to-date:
  bazel-bin/artifacts.tar
```

## Install binaries (linux and mac only)

To install all binaries to a `$BINDIR`

```shell
$ bazelisk run --config=goma //:artifacts_install -- --destdir $BINDIR
[...]
INFO: Running command line: bazel-bin/artifacts_install --destdir $BINDIR
```

# Run unit tests
```
$ bazelisk test //pkg/... //internal/...
[...]
INFO: Elapsed time: 77.166s, Critical Path: 30.24s
INFO: 472 processes: 472 linux-sandbox.
INFO: Build completed successfully, 504 total actions
//internal/pkg/cli:go_default_test                                       PASSED in 0.2s
//internal/pkg/deps:go_default_test                                      PASSED in 1.2s
//internal/pkg/inputprocessor/action/cppcompile:go_default_test          PASSED in 0.1s
//internal/pkg/inputprocessor/flagsparser:go_default_test                PASSED in 0.1s
//internal/pkg/inputprocessor/pathtranslator:go_default_test             PASSED in 0.1s
//internal/pkg/inputprocessor/toolchain:go_default_test                  PASSED in 0.2s
//internal/pkg/labels:go_default_test                                    PASSED in 0.1s
//internal/pkg/logger:go_default_test                                    PASSED in 0.2s
//internal/pkg/rbeflag:go_default_test                                   PASSED in 0.1s
//internal/pkg/reproxy:go_default_test                                   PASSED in 15.5s
//internal/pkg/rewrapper:go_default_test                                 PASSED in 0.2s
//internal/pkg/stats:go_default_test                                     PASSED in 0.1s
//pkg/cache:go_default_test                                              PASSED in 0.2s
//pkg/cache/singleflightcache:go_default_test                            PASSED in 0.1s
//pkg/filemetadata:go_default_test                                       PASSED in 2.1s
//pkg/inputprocessor:go_default_test                                     PASSED in 0.2s

Executed 16 out of 16 tests: 16 tests pass.
```

Reclient can be built to use Goma's input processor. Goma's input processor is
3x faster than clang-scan-deps for a typical compile action in Chrome. Build as
follows:

```shell
bazelisk build //:artifacts_tar --config=goma
```


# Versioning

There are four binaries that are built from this repository and used with
Android Platform for build acceleration:

-   rewrapper
-   reproxy
-   dumpstats
-   bootstrap

These binaries must be stamped with an appropriate version number before they
are dropped into Android source for consumption.

## Versioning Guidelines

1.  We will *maintain a consistent version across all of the binaries*. That
    means, when there are changes to only one of the binaries, we will increment
    the version number for all of them.

2.  In order to be consistent with
    [Semantic versioning scheme](https://semver.org/), the version format is of
    the form “X.Y.Z.SHA” denoting “MAJOR.MINOR.PATCH.GIT_SHA”.

3.  Updating version numbers:

    MAJOR

    -   Declare major version “1” when re-client is feature complete for caching
        and remote-execution capabilities.
    -   Update major version post “1”, when there are breaking changes to
        interface / behavior of rewrapper tooling. Some examples of this are:
        changing any of the flag names passed to rewrapper, changing the name of
        rewrapper binary.

    MINOR - Update minor version when

    -   New features are introduced in a backward compatible way. For example,
        when remote-execution capability is introduced.
    -   Major implementation changes without changes to behavior / interface.
        For example, if the “.deps” file is changed to JSON format.

    PATCH - Update patch version

    -   For all other bug fixes only. Feature additions (irrespective of how
        insignificant they are) should result in a MINOR version change.
    -   Any new release to Android Platform of re-client tools should update the
        PATCH version at minimum.

4.  Release Frequency:

    -   Kokoro release workflows can be triggered as often as necessary to
        generate new release artifacts.

## How to update version numbers?

You can update the MAJOR/MINOR/PATCH version numbers by simply changing the
`version.bzl` file present in the root of this repository.


# Reclient releases

Reclient binaries are released into the
[CIPD](https://chrome-infra-packages.appspot.com/p/infra/rbe/client) (Chrome
Infrastructure Package Deployment) with separate packages for Linux, Mac (amd64
and arm64), and Windows. Whenever a new version of Reclient is released there
are 2 sets of binaries released for each of the platforms. Those binaries use 2
different include scanners for C++ build actions: clang-scan-deps and goma. The
binaries using the goma include scanner have a version number ending with
“-gomaip” suffix, the ones using clang-scan-deps don’t have the suffix. Clients
migrating from Goma should use the releases using goma include scanner (with
-gomaip suffix).

## Downloading Reclient binaries

Reclient binaries can be downloaded using CIPD's
[Web UI](\(https://chrome-infra-packages.appspot.com/p/infra/rbe/client\)), with
a
[CLI client](https://chromium.googlesource.com/infra/luci/luci-go/+/refs/heads/main/cipd/client),
or using
[gclient](https://chromium.googlesource.com/chromium/tools/depot_tools/+/HEAD/README.gclient.md)'s
configuration.

### Downloading binaries with CIPD CLI client

To download Reclient with GomaIP dependency scanner (used for building
Chromium):

```
echo 'infra/rbe/client/${platform}' $RECLIENT_VERSION > /tmp/reclient.ensure
cipd ensure --root $CHECKOUT_DIR --ensure-file /tmp/reclient.ensure
```

To use Reclient with Clangscandeps (used for Android builds) instead, add `-csd`
suffix to CIPD package:

```
echo 'infra/rbe/client/${platform}-csd' $RECLIENT_VERSION > /tmp/reclient.ensure

```

*   **`$RECLIENT_VERSION`** - the version of Reclient. It can be set to one of
    the following:

    *   A fixed version of Reclient. For example
        `re_client_version:0.114.2.81e819b-gomaip` (for Reclient with GomaIP
        dependency scanner) or `re_client_version:0.114.2.81e819b` (for Reclient
        with Clangscandeps).
    *   `latest` - the latest released Reclient version.
    *   `stable` - the latest stable Reclient version. Stable version is usually
        1-2wks behind the `latest` as Reclient needs to run a few days on test
        and staging environments without issues and degradations before it's
        considered as `stable`.

*   **`$CHECKOUT_DIR`** - the location where Reclient should be downloaded.

### Downloading binaries with gclient

You can configure
[gclient](https://chromium.googlesource.com/chromium/tools/depot_tools/+/HEAD/README.gclient.md)
to download Reclient binaries during the `gclient sync` phase. Gclient expects a
DEPS file in the repository’s root directory. The file contains components that
will be checked out during the sync phase. To check out Reclient, the file
should have a similar entry to:

```
vars = {
    ...
    'reclient_version': '<version>',
    ...
}

deps = {
      ...
'<checkout-directory>': {
    'packages': [
      {
        'package': 'infra/rbe/client/${{platform}}',
        'version': Var('reclient_version'),
      }
    ],
    'dep_type': 'cipd',
  },
}
```

This will instruct **gclient** to check out `<version>` of Reclient from
`/infra/rbe/client/<platform>` CIPD package into `<checkout-directory>`
([example](https://source.chromium.org/chromium/chromium/src/+/main:DEPS;l=583-590;drc=9362acf277a0dac6295d43e5c592ee8b065c496b)).
Extracting a version to a variable (as in an example above) is optional, but
provides a benefit of being able to override the default value through gclient’s
custom variables.

**Note:** The snippet above will instruct `gclient` to download Reclient with
GomaIP dependency processor. If you prefer Reclient with Clangscandeps, you'd
need to set package to `infra/rbe/client/${{platform}}-csd`.

# Using Reclient

## Starting and stopping reproxy

Reclient requires reproxy to be started at the beginning of the build, and
stopped at the end. This is done through `bootstrap` binary by executing
following commands:

**Start:**

```
bootstrap -re_proxy=$reproxy_location [-cfg=$reproxy_config_location]
```

**Stop:**

```
bootstrap -re_proxy=$reproxy_location -shutdown
```

## Configuration

Each of Reclient’s binaries can be configured either by command line flags,
environment variables, config files, or by combination of either of those (some
flags provided in the command line while others in the config file or set as
environment variables). If the same flag is defined in the command line and in
the config file or as an environment variable, the order of precedence is
following (from lowest to highest priority):

1.  Config file
1.  Environment variable
1.  Command line argument

To use a configuration file, specify it with the `-cfg=$config_file_location`
flag. The config file is a list of `flag_name=flag_value` pairs, each on a new
line. Example below:

```
service=$RE_SERVER_ADDRESS
instance=$RE_SERVER_INSTANCE
server_address=unix:///tmp/reproxy.sock
log_dir=/tmp
output_dir=/tmp
proxy_log_dir=/tmp
depsscanner_address=$scandeps_server_location #distributed with Reclient
use_gce_credentials=true
```

To configure Reclient with environment variables, the variables should be
prefixed with `RBE_` (e.g. the value of `RBE_service` environment variable is
used to set the `service` flag).

### Rewrapper

Full list of rewrapper config flags can be found in
[docs/cmd-line-flags.md](docs/cmd-line-flags.md). A few of the most commonly
used flags are:

*   **platform** - Comma-separated key value pairs in the form key=value. This
    is used to identify remote platform settings like the docker image to use to
    run the command. The list of supported keys depends on RE Server
    implementation. A detailed lexicon can be found
    [here](https://github.com/bazelbuild/remote-apis/blob/main/build/bazel/remote/execution/v2/platform.md)
*   **server_address** - The address reproxy is running on. It needs to be set
    to the same value as reproxy’s `server_address` flag so that rewrapper and
    reproxy can communicate with each other. This value should be UDS on
    Linux/Mac (e.g. unix:///tmp/reproxy.sock) and a named pipe on Windows (e.g.
    pipe://reproxy.ipc). Depot_tools has a helper choosing the address based on
    the platform
    ([here](https://source.chromium.org/chromium/chromium/tools/depot_tools/+/main:reclient_helper.py;l=159-168;drc=60b21dd19301b13eaf7c9069ac191f95f84ca6e9)).
*   **labels** - Identifies the type of command to help the proxy make decisions
    regarding remote execution. Labels consist of comma-separated key-value
    pairs in form key=value where key is one of the following: type, compiler,
    lang, tool, and toolname. Some examples of valid labels are:
    *   `type=compile,compiler=clang,lang=cpp` - clang compile actions
    *   `type=compile,compiler=clang-cl,lang=cpp` - clang compile actions
    *   `type=compile,compiler=nacl,lang=cpp` - nacl compile actions
    *   `type=compile,compiler=javac,lang=java` - java compile actions
    *   `type=link,tool=clang` - link actions
    *   `type=tool` - generic action that doesn’t require any action specific
        input processing
*   **exec_strategy** - One of `local`, `remote`, `remote_local_fallback`,
    `racing`. It is recommended to set it to `remote_local_fallback` or
    `racing`. With `remote_local_fallback` it will try to execute the action
    remotely and fallback to local if the remote execution failed. With `racing`
    it tries to execute both and picks the one that finished sooner.
*   `env_var_allowlist` - List of environment variables allowed to pass to the
    proxy. If the build action depends on local environment variables, they
    should be set here, so they're reproduced on the remote worker.

If you are experiencing sporadic timeouts when dialing reproxy, you might
consider adding:

*   **dial_timeout** - By default is 3m, if the flag is not set. But for some
    projects, increasing it up to 10m has proved beneficial in eliminating the
    timeouts

### Reproxy

Full list of reproxy flags can be found
[docs/cmd-line-flags.md](docs/cmd-line-flags.md). A few of the most commonly
used flags are:

*   **service** - The remote execution service to dial when calling via gRPC,
    including port, such as `localhost:8790`
*   **instance** - If a server supports multiple instances of the execution
    system (with their own workers, storage, caches, etc.), the field instructs
    the server which instance of the execution system to operate against. If the
    server does not support different instances, the field can be omitted.
*   **server_address** - An address reproxy should start its gRPC server on and
    listen for incoming communication from rewrapper (should be set to the same
    value as rewrapper's `server_address` parameter)
*   **depsscanner_address** - The address of the dependency scanner service. To
    use the `scandeps_server` distributed with Reclient set the value to
    `exec://$absolute_path_to_reclient_dir/scandeps_server` For instance, if
    Reclient is checked out to `/home/$user/chromium/src/buildtools/reclient/`,
    the value of the attribute should be
    `exec:///home/$user/chromium/src/buildtools/reclient/scandeps_server`

#### Authentication flags

If your RE Server implementation does not use RPC authentication then use one
of:

*   **service_no_auth** - If `true`, do not authenticate with the service
    (implied by `-service_no_security`).
*   **use_rpc_credentials** - If `false`, no per-RPC credentials will be used.
    Disables `--credential_file`, `-use_application_default_credentials`, and
    `-use_gce_credentials`. (default `true`).

If your RE Server uses RPC authentication then use one of the following flags:

*   **use_gce_credentials** - If `true` (and
    `--use_application_default_credentials` is `false`), use the default GCE
    credentials to authenticate with remote execution
    (https://cloud.google.com/docs/authentication/provide-credentials-adc#attached-sa).
*   **use_application_default_credentials** - If `true`, use application default
    credentials to connect to remote execution. See
    https://cloud.google.com/sdk/gcloud/reference/auth/application-default/login
*   **credential_file** - The name of a file that contains service account
    credentials to use when calling remote execution. Used only if
    `-use_application_default_credentials` and `-use_gce_credentials` are false.
*   **experimental_credentials_helper** - Path to the credentials helper binary. If given `execrel://`, looks for the `credshelper` binary in the same folder as bootstrap/reproxy
*   **experimental_credentials_helper_args** - Arguments for the experimental credentials helper, separated by space

    The reproxy is typically started via the bootstrap, so it is recommended to
    avoid configuring it through the command line flags. It's advised to use
    either a configuration file that’s passed to the bootstrap with the `-cfg`
    flag or by setting environment variables before starting the bootstrap
    ([example](https://source.chromium.org/chromium/chromium/tools/build/+/main:recipes/recipe_modules/reclient/api.py;l=378-396;drc=f3b7708f2a5728408a74ccbb6cba6eb9cb161aae)).

#### Auxiliary Metadata flag

If you want to collect backend workers' auxiliary metadata (cpu, memory usage per action), you can
generate a .pb (or .proto.bin) file contains the descriptor information that will be used by reproxy at runtime to
decode the auxiliary metadata, which is a [proto message](http://shortn/_3m8oS5NBur) in the type of `google.protobuf.Any`.

Once you have customized `auxiliary_metadata.proto` file per your backend worker's
specification, compile it as a `.pb` or `.proto.bin` file with `protoc`, and pass
the file path to reproxy via `--auxiliary_metadata_path` flag, or environment
variable `RBE_auxiliary_metadata_path`. Then, at runtime, reporxy will use this file to parse
your backend worker's auxiliary metadata and log the data into reporxy logs.

```bash
cd api/auxiliary_metadata # or where you have the cusotmized `.proto` file

protoc \
--proto_path=. \
--descriptor_set_out=auxiliary_metadata.pb \
auxiliary_metadata.proto

export RBE_auxiliary_metadata_path=~/Workspace/re-client/api/auxiliary_metadata/auxiliary_metadata.pb

# then continue with your regular build with reproxy
```

or

```bash
cd api/auxiliary_metadata # or where you have the cusotmized `.proto` file

protoc \
--proto_path=. \
--descriptor_set_out=auxiliary_metadata.proto.bin \
auxiliary_metadata.proto

export RBE_auxiliary_metadata_path=~/Workspace/re-client/api/auxiliary_metadata/auxiliary_metadata.proto.bin

# then continue with your regular build with reproxy
```

It's worth noting that the backend can give this proto message any arbitrary name;
however, the client side proto message should strictly use `AuxiliaryMetadata` to receive it.
For example, in the unit test, the backend send the proto msg out with name `WorkerAuxiliaryMetadata`, and client receives it as `AuxiliaryMetadata`.

## Integration with the build system

To execute your build actions remotely through Reclient, the build command
should be prepended with:

```
 $rewrapper [-cfg=$config-file] -exec_root=$checkout-dir --
```

, where:

*   **\$rewrapper** - path of the rewrapper binary
*   **\$config-file** - path of the rewrapper config file (assuming that
    rewrapper was configured with config file)
*   **\$checkout-dir** - root directory of the source repository

When rewrapper is executed, it passes the build command to a running instance of
reproxy that:

*   Determines dependencies either from command line flags (e.g. clang/gcc’s -I
    flag) or from the content of input files (during input processing phase)
*   Uploads toolchain, the inputs, and their dependencies to RBE
*   Executes the command remotely
*   Downloads artifacts locally

Before the build is executed, reproxy needs to be started by bootstrap and shut
down at the end of the build.

During the run, reproxy writes its application-level logs to a directory
specified by a `log_dir` flag and logs records about the executed build actions
to an RPL file in a directory specified by the `proxy_log_dir` flag. During
reproxy shutdown, bootstrap dumps Reclient related build metrics to
`rbe_metrics.txt` file saved at a location specified by the bootstrap's
`output_path` flag.

### GN integration

[GN](https://gn.googlesource.com/gn/) is a meta-build system that generates
build files for Ninja. Its configuration files are written in a simple,
dynamically typed language. Reclient can be integrated with the build by
modifying the GN config files. Because of GN's language flexibility the method
of how Reclient should be integrated will depend on the project, but usually it
should involve adding a rewrapper prefix
([example](https://source.chromium.org/chromium/chromium/src/+/main:build/toolchain/gcc_toolchain.gni;l=214;drc=df7d81e33033a594ff56d5ab8387aa1f13cd1c39))
that's controlled by a gn argument
([example](https://source.chromium.org/chromium/chromium/src/+/main:build/toolchain/rbe.gni;l=14;drc=dc850b9b1565dc29b7d8997994ed82d867852250)),
and starting and stopping reproxy before and after the build. The latter might
be done by a helper script with reproxy start and stop steps around the ninja
call
[example](https://source.chromium.org/chromium/chromium/tools/depot_tools/+/main:reclient_helper.py).

### CMake integration

You can integrate CMake with Reclient by using
[`<LANG>_COMPILER_LAUNCHER`](https://cmake.org/cmake/help/v3.14/prop_tgt/LANG_COMPILER_LAUNCHER.html)
property. This property is initialized by the value of the
`CMAKE_<LANG>_COMPILER_LAUNCHER` variable if it is set when a target is created.
For instance, to use Reclient for c/c++ compile actions, you’d need to set both
`CMAKE_C_COMPILER_LAUNCHER` and `CMAKE_CXX_COMPILER_LAUNCHER` to
`$rewrapper;-cfg=$config-file;-exec_root=$checkout-dir` (the property accepts
semicolon separated list as a launcher command).

Please note that CMake operates on absolute paths and you need to ensure that RE
server executes the action on a remote worker in the same directory as it is in
a local build machine (the method depends on your RE Server implementation).
Moreover, please be aware that rewrapper's `canonicalize_working_dir` flag
tampers the build actions' inputs paths, and thus should be disabled for the
build actions generated by CMake.

Example of CMake build integration with Reclient can be found
[here](https://chromium.googlesource.com/emscripten-releases/+/refs/heads/main/src/host_toolchains.py).

### Test Import Workflow
Test
