## Remote Execution Client

This repository contains a client that works with
[Remote Execution API](https://github.com/bazelbuild/remote-apis-sdks).


### Building

`re-client` currently builds and is supported on Linux / Mac / Windows.

`re-client` builds with [Bazel](https://bazel.build/). We recommend using
[Bazelisk](https://github.com/bazelbuild/bazelisk) to use the version of Bazel
currently supported by this code base.

Once you've installed Bazel, and are in the re-client repo:

```

# Build the code
$ bazelisk build --config=clangscandeps //cmd/...
# You should now have binaries for 'bootstrap', 'dumpstats', 'reproxy',
# 'rewrapper'.

# Run unit tests
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

```
bazelisk build //cmd/... --config=goma
```


### Versioning

There are four binaries that are built from this repository and used with
Android Platform for build acceleration:

-   rewrapper
-   reproxy
-   dumpstats
-   bootstrap

These binaries must be stamped with an appropriate version number before they
are dropped into Android source for consumption.

#### Versioning Guidelines

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

#### How to update version numbers?

You can update the MAJOR/MINOR/PATCH version numbers by simply changing the
`version.bzl` file present in the root of this repository.


### Note

This is not an officially supported Google product.
