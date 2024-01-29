workspace(name = "re_client")

load("@bazel_tools//tools/build_defs/repo:http.bzl", "http_archive")
load("@bazel_tools//tools/build_defs/repo:utils.bzl", "maybe")

http_archive(
    name = "bazel_skylib",
    sha256 = "74d544d96f4a5bb630d465ca8bbcfe231e3594e5aae57e1edbf17a6eb3ca2506",
    urls = [
        "https://mirror.bazel.build/github.com/bazelbuild/bazel-skylib/releases/download/1.3.0/bazel-skylib-1.3.0.tar.gz",
        "https://github.com/bazelbuild/bazel-skylib/releases/download/1.3.0/bazel-skylib-1.3.0.tar.gz",
    ],
)

load("@bazel_skylib//:workspace.bzl", "bazel_skylib_workspace")

bazel_skylib_workspace()

load("@bazel_skylib//rules:expand_template.bzl", "expand_template")

http_archive(
    name = "io_bazel_rules_go",
    # TODO(b/180953129): Required to build re-client with RBE on windows for now.
    # Wait until https://github.com/bazelbuild/remote-apis/issues/187 is fixed on
    # RE side, and https://github.com/bazelbuild/bazel/issues/11636 on bazel side.
    patch_args = ["-p1"],
    patches = ["//third_party/patches/bazel:rules_go.patch"],
    sha256 = "16e9fca53ed6bd4ff4ad76facc9b7b651a89db1689a2877d6fd7b82aa824e366",
    urls = [
        "https://mirror.bazel.build/github.com/bazelbuild/rules_go/releases/download/v0.34.0/rules_go-v0.34.0.zip",
        "https://github.com/bazelbuild/rules_go/releases/download/v0.34.0/rules_go-v0.34.0.zip",
    ],
)

http_archive(
    name = "bazel_gazelle",
    sha256 = "efbbba6ac1a4fd342d5122cbdfdb82aeb2cf2862e35022c752eaddffada7c3f3",
    urls = [
        "https://mirror.bazel.build/github.com/bazelbuild/bazel-gazelle/releases/download/v0.27.0/bazel-gazelle-v0.27.0.tar.gz",
        "https://github.com/bazelbuild/bazel-gazelle/releases/download/v0.27.0/bazel-gazelle-v0.27.0.tar.gz",
    ],
)

load("@bazel_gazelle//:deps.bzl", "gazelle_dependencies", "go_repository")
load("@io_bazel_rules_go//go:deps.bzl", "go_register_toolchains", "go_rules_dependencies")

go_repository(
    name = "com_github_google_uuid",
    importpath = "github.com/google/uuid",
    sum = "h1:b4Gk+7WdP/d3HZH8EJsZpvV7EtDOgaZLtnaNGIu1adA=",
    version = "v1.0.0",
)

go_repository(
    name = "com_github_google_go_cmp",
    importpath = "github.com/google/go-cmp",
    sum = "h1:JFrFEBb2xKufg6XkJsJr+WbKb4FQlURi5RUcBveYu9k=",
    version = "v0.5.1",
)

go_repository(
    name = "com_google_cloud_go_bigquery",
    importpath = "cloud.google.com/go/bigquery",
    sum = "h1:PQcPefKFdaIzjQFbiyOgAqyx8q5djaE7x9Sqe712DPA=",
    version = "v1.8.0",
)

go_repository(
    name = "org_golang_x_net",
    importpath = "golang.org/x/net",
    sum = "h1:O7DYs+zxREGLKzKoMQrtrEacpb0ZVXA5rIwylE2Xchk=",
    version = "v0.0.0-20220127200216-cd36cc0744dd",
)

go_repository(
    name = "org_golang_x_oauth2",
    importpath = "golang.org/x/oauth2",
    sum = "h1:qe6s0zUXlPX80/dITx3440hWZ7GwMwgDDyrSGTPJG/g=",
    version = "v0.7.0",
)

go_repository(
    name = "org_golang_x_sync",
    importpath = "golang.org/x/sync",
    sum = "h1:qwRHBd0NqMbJxfbotnDhm2ByMI1Shq4Y6oRJo21SGJA=",
    version = "v0.0.0-20200625203802-6e8e738ad208",
)

go_repository(
    name = "org_golang_x_sys",
    importpath = "golang.org/x/sys",
    sum = "h1:Zr2JFtRQNX3BCZ8YtxRE9hNJYC8J6I1MVbMg6owUp18=",
    version = "v0.4.0",
)

go_repository(
    name = "org_golang_x_text",
    importpath = "golang.org/x/text",
    sum = "h1:olpwvP2KacW1ZWvsR7uQhoyTYvKAupfQrRGBFM352Gk=",
    version = "v0.3.7",
)

go_repository(
    name = "com_github_golang_snappy",
    importpath = "github.com/golang/snappy",
    sum = "h1:fHPg5GQYlCeLIPB9BZqMVR5nR9A+IM5zcgeTdjMYmLA=",
    version = "v0.0.3",
)

go_repository(
    name = "com_github_klauspost_compress",
    importpath = "github.com/klauspost/compress",
    sum = "h1:G5AfA94pHPysR56qqrkO2pxEexdDzrpFJ6yt/VqWxVU=",
    version = "v1.12.3",
)

go_repository(
    name = "com_github_fatih_color",
    importpath = "github.com/fatih/color",
    sum = "h1:8LOYc1KYPPmyKMuN8QV2DNRWNbLo6LZ0iLs8+mlH53w=",
    version = "v1.13.0",
)

# Needed for github.com/fatih/color.
go_repository(
    name = "com_github_mattn_go_colorable",
    importpath = "github.com/mattn/go-colorable",
    sum = "h1:jF+Du6AlPIjs2BiUiQlKOX0rt3SujHxPnksPKZbaA40=",
    version = "v0.1.12",
)

# Needed for github.com/fatih/color.
go_repository(
    name = "com_github_mattn_go_isatty",
    importpath = "github.com/mattn/go-isatty",
    sum = "h1:yVuAays6BHfxijgZPzw+3Zlu5yQgKGP2/hcQbHb7S9Y=",
    version = "v0.0.14",
)

go_repository(
    name = "org_golang_x_lint",
    importpath = "golang.org/x/lint",
    sum = "h1:VLliZ0d+/avPrXXH+OakdXhpJuEoBZuwh1m2j7U6Iug=",
    version = "v0.0.0-20210508222113-6edffad5e616",
)

# Needed for golang.org/x/lint
go_repository(
    name = "org_golang_x_tools",
    importpath = "golang.org/x/tools",
    sum = "h1:W07d4xkoAUSNOkOzdzXCdFGxT7o2rW4q8M34tB2i//k=",
    version = "v0.0.0-20200825202427-b303f430e36d",
)

go_repository(
    name = "com_github_hectane_go_acl",
    importpath = "github.com/hectane/go-acl",
    sum = "h1:PGufWXXDq9yaev6xX1YQauaO1MV90e6Mpoq1I7Lz/VM=",
    version = "v0.0.0-20230122075934-ca0b05cb1adb",
)

load("//:go_deps.bzl", "shirou_gopsutil_deps")

# gazelle:repository_macro go_deps.bzl%shirou_gopsutil_deps
shirou_gopsutil_deps()

go_rules_dependencies()

go_register_toolchains(version = "1.19.5")

# Needed for protobuf.
http_archive(
    name = "com_google_protobuf",
    sha256 = "985bb1ca491f0815daad825ef1857b684e0844dc68123626a08351686e8d30c9",
    strip_prefix = "protobuf-3.15.6",
    urls = ["https://github.com/protocolbuffers/protobuf/archive/v3.15.6.zip"],
)

load("@com_google_protobuf//:protobuf_deps.bzl", "protobuf_deps")

protobuf_deps()

load("//:settings.bzl", "GO_GRPC_VERSION", "GO_PROTO_VERSION", "LLVM_COMMIT", "LLVM_SHA256", "SDK_COMMIT")

http_archive(
    name = "llvm",
    build_file_content = "#empty",
    patch_args = ["-p1"],
    patches = [
        # Windows GCC tries to canonize path imports paths if their canonization is
        # form is shorter than the current shown form [1]. For imports of the type:
        # "extralong\path\..\..", gcc isn't smart enough to just remove the redundant
        # part of the path and instead makes them absolute. This patch removes all
        # the ".." includes from the files we care about.
        # We can also try patching GCC, but even if that's successful, we'd still
        # need to wait for a release.
        # 1: https://github.com/gcc-mirror/gcc/blob/16e2427f50c208dfe07d07f18009969502c25dc8/libcpp/files.c#L405
        "//third_party/patches/llvm:llvm-long-path.patch",
        # For simplicity, we expose the tblgen rule for a binary we use.
        "//third_party/patches/llvm:llvm-bzl-tblgen.patch",
        # This patch picks the right version of assembly files to build libSupport
        # on Windows. Refer to https://github.com/llvm/llvm-project/issues/54685
        # for the corresponding fix to CMake files.
        "//third_party/patches/llvm:llvm-bazel-libsupport.patch",
        # This patch includes a few linking options required to make gcc on windows
        # work. We tried upstreaming them but since msvc doesn't need them, the maintainers
        # decided to make them .bazelrc config options instead. Trying to use bazel
        # conditions to select on the compiler has been _very_ challenging.
        # Instead of making them global options, we just stuck them on the necessary
        # targets to optimze compilation.
        "//third_party/patches/llvm:llvm-bzl-mingw.patch",
        # https://github.com/bazelbuild/rules_go/issues/2848
        # Not ideal, but can't compile binaries without this.
        "//third_party/patches/llvm:llvm-bzl-static.patch",
        # Don't link version.lib.
        # The library is not part of gcc toolchain and require VS SDK to be installed.
        # It's not needed to build @llvm-project//clang:tooling_dependency_scanning
        # that's used by clang dependency scanner
        "//third_party/patches/llvm:llvm-project-overlay-driver.patch",
        # Replace @llvm-raw with @llvm so we can build llvm inside of re-client.
        # In the llvm-project checkout, @llvm-raw is defined the WORKSPACE file
        # and point to the root of llvm-project; However, when we invoke the
        # line `llvm_configure(name = "llvm-project")` below, in the bzl file,
        # @llvm//utils/bazel:configure.bzl, @llvm-raw is not pre-defined.
        "//third_party/patches/llvm:llvm-bzl-config.patch",
        # Disable @llvm_zstd//:zstd on Windows build
        "//third_party/patches/llvm:llvm-bzl-zstd.patch",
    ],
    sha256 = LLVM_SHA256,
    strip_prefix = "llvm-project-%s" % LLVM_COMMIT,
    urls = [
        "https://mirror.bazel.build/github.com/llvm/llvm-project/archive/%s.zip" % LLVM_COMMIT,
        "https://github.com/llvm/llvm-project/archive/%s.zip" % LLVM_COMMIT,
    ],
)

load("@llvm//utils/bazel:configure.bzl", "llvm_configure")

llvm_configure(name = "llvm-project")

# Need to keep zlib and zstd below to build LLVM on Windows.
# We also need to have a patch to disable the use of zstd on Windows.
# See: third_party/patches/llvm/llvm-bzl-zstd.patch
# Without these two libs, windows build through err msg like this:
# https://paste.googleplex.com/5724967793065984
# Without the patch to disable zstd on Windows, we get err msg like this:
# https://paste.googleplex.com/4613455502376960
maybe(
    http_archive,
    name = "llvm_zlib",
    build_file = "@llvm//utils/bazel/third_party_build:zlib-ng.BUILD",
    sha256 = "e36bb346c00472a1f9ff2a0a4643e590a254be6379da7cddd9daeb9a7f296731",
    strip_prefix = "zlib-ng-2.0.7",
    urls = [
        "https://github.com/zlib-ng/zlib-ng/archive/refs/tags/2.0.7.zip",
    ],
)

maybe(
    http_archive,
    name = "llvm_zstd",
    build_file = "@llvm//utils/bazel/third_party_build:zstd.BUILD",
    sha256 = "7c42d56fac126929a6a85dbc73ff1db2411d04f104fae9bdea51305663a83fd0",
    strip_prefix = "zstd-1.5.2",
    urls = [
        "https://github.com/facebook/zstd/releases/download/v1.5.2/zstd-1.5.2.tar.gz",
    ],
)

# This grpc section must come after llvm
http_archive(
    name = "com_github_grpc_grpc",
    sha256 = "76900ab068da86378395a8e125b5cc43dfae671e09ff6462ddfef18676e2165a",
    strip_prefix = "grpc-1.50.0",
    urls = [
        "https://github.com/grpc/grpc/archive/refs/tags/v1.50.0.tar.gz",
    ],
)

load("@com_github_grpc_grpc//bazel:grpc_deps.bzl", "grpc_deps")

grpc_deps()

# Extra dependencies extracted from grpc_extra_deps.bzl to remove duplication and conflicts
load("@build_bazel_apple_support//lib:repositories.bzl", "apple_support_dependencies")
load("@build_bazel_rules_apple//apple:repositories.bzl", "apple_rules_dependencies")
load("@com_google_googleapis//:repository_rules.bzl", "switched_rules_by_language")

apple_rules_dependencies()

apple_support_dependencies()

# Initialize Google APIs with only C++ and Python targets
switched_rules_by_language(
    name = "com_google_googleapis_imports",
    cc = True,
    grpc = True,
    python = True,
)
# End grpc section

http_archive(
    name = "com_github_gflags_gflags",
    sha256 = "34af2f15cf7367513b352bdcd2493ab14ce43692d2dcd9dfc499492966c64dcf",
    strip_prefix = "gflags-2.2.2",
    urls = ["https://github.com/gflags/gflags/archive/v2.2.2.tar.gz"],
)

# glog here is specifically used on Windows for the dependency scanner service.
# The version must be identical to that used by goma.
http_archive(
    name = "com_github_google_glog",
    sha256 = "21bc744fb7f2fa701ee8db339ded7dce4f975d0d55837a97be7d46e8382dea5a",
    strip_prefix = "glog-0.5.0",
    urls = ["https://github.com/google/glog/archive/v0.5.0.zip"],
)

go_repository(
    name = "com_github_golang_glog",
    importpath = "github.com/golang/glog",
    sum = "h1:uCdmnmatrKCgMBlM4rMuJZWOkPDqdbZPnrMXDY4gI68=",
    version = "v1.2.0",
)

go_repository(
    name = "org_golang_google_protobuf",
    importpath = "google.golang.org/protobuf",
    sum = "h1:Ejskq+SyPohKW+1uil0JJMtmHCgJPJ/qWTxr8qp+R4c=",
    version = GO_PROTO_VERSION,
)

# Needed for io_opencensus_go_contrib_exporter_stackdriver.
go_repository(
    name = "com_google_cloud_go",
    importpath = "cloud.google.com/go",
    sum = "h1:Dg9iHVQfrhq82rUNu9ZxUDrJLaxFUe/HlCVaLyRruq8=",
    version = "v0.65.0",
)

go_repository(
    name = "com_github_microsoft_go_winio",
    importpath = "github.com/Microsoft/go-winio",
    sum = "h1:aPJp2QD7OOrhO5tQXqQoGSJc+DjDtWTGLOmNyAm6FgY=",
    version = "v0.5.1",
)

# needed for cloud.google.com/go/profiler
go_repository(
    name = "com_github_google_pprof",
    importpath = "github.com/google/pprof",
    sum = "h1:Ak8CrdlwwXwAZxzS66vgPt4U8yUZX7JwLvVR58FN5jM=",
    version = "v0.0.0-20200708004538-1a94d8640e99",
)

# Needed for opencensus.
go_repository(
    name = "com_github_golang_groupcache",
    importpath = "github.com/golang/groupcache",
    sum = "h1:1r7pUrabqp18hOBcwBwiTsbnFeTZHV9eER/QT5JVZxY=",
    version = "v0.0.0-20200121045136-8c9f03a8e57e",
)

# remote and vcs attributes are required for downloading major version suffixed.
go_repository(
    name = "com_github_shirou_gopsutil",
    commit = "faad8060806256ffc4da37f9535160af4e8b0fee",
    importpath = "github.com/shirou/gopsutil/v3",
    remote = "https://github.com/shirou/gopsutil",
    vcs = "git",
)

go_repository(
    name = "io_opencensus_go",
    importpath = "go.opencensus.io",
    sum = "h1:y73uSU6J157QMP2kn2r30vwW1A2W2WFwSCGnAVxeaD0=",
    version = "v0.24.0",
)

go_repository(
    name = "io_opencensus_go_contrib_exporter_stackdriver",
    importpath = "contrib.go.opencensus.io/exporter/stackdriver",
    patch_args = ["-p1"],
    patches = ["//third_party/patches/opencensus-go-exporter-stackdriver:opencensus-stackdriver-interval.patch"],
    sum = "h1:lIFYmQsqejvlq+GobFUbC5F0prD5gvhP6r0gWLZRDq4=",
    version = "v0.13.8",
)

# Needed for io_opencensus_go_contrib_exporter_stackdriver.
go_repository(
    name = "com_github_census_instrumentation_opencensus_proto",
    build_extra_args = ["-exclude=src"],
    importpath = "github.com/census-instrumentation/opencensus-proto",
    sum = "h1:glEXhBS5PSLLv4IXzLA5yPRVX4bilULVyxxbrfOtDAk=",
    version = "v0.2.1",
)

# Needed for io_opencensus_go_contrib_exporter_stackdriver.
go_repository(
    name = "com_github_aws_aws_sdk_go",
    importpath = "github.com/aws/aws-sdk-go",
    tag = "v1.23.20",
)

go_repository(
    name = "org_golang_google_api",
    importpath = "google.golang.org/api",
    sum = "h1:yfrXXP61wVuLb0vBcG6qaOoIoqYEzOQS8jum51jkv2w=",
    version = "v0.30.0",
)

go_repository(
    name = "org_golang_google_grpc",
    importpath = "google.golang.org/grpc",
    sum = "h1:fVRFRnXvU+x6C4IlHZewvJOVHoOv1TUuQyoRsYnB4bI=",
    version = GO_GRPC_VERSION,
)

go_repository(
    name = "com_github_googleapis_gax_go_v2",
    importpath = "github.com/googleapis/gax-go/v2",
    sum = "h1:sjZBwGj9Jlw33ImPtvFviGYvseOtDM7hkSKB7+Tv3SM=",
    version = "v2.0.5",
)

go_repository(
    name = "com_github_karrick_godirwalk",
    importpath = "github.com/karrick/godirwalk",
    tag = "v1.16.1",
)

go_repository(
    name = "com_github_pkg_xattr",
    importpath = "github.com/pkg/xattr",
    tag = "v0.4.4",
)

go_repository(
    name = "com_github_vardius_progress_go",
    commit = "c85a970b9413ed1fe58311b98ac4048826ffcc93",
    importpath = "github.com/vardius/progress-go",
)

go_repository(
    name = "org_golang_x_xerrors",
    importpath = "golang.org/x/xerrors",
    sum = "h1:go1bK/D/BFZV2I8cIQd1NKEZ+0owSTG1fDTci4IqFcE=",
    version = "v0.0.0-20200804184101-5ec99f83aff1",
)

http_archive(
    name = "protoc_gen_bq_schema",
    build_file = "protoc_gen_bq_schema/BUILD.bazel",
    sha256 = "e7d18d4d0f91a647aebb808c07a72d498515635beb2a0e8b0e2cac44ee944e5a",
    strip_prefix = "protoc-gen-bq-schema-026f9fcdf7054ab6c21c8c72484fe6774ac5f149",
    urls = ["https://github.com/GoogleCloudPlatform/protoc-gen-bq-schema/archive/026f9fcdf7054ab6c21c8c72484fe6774ac5f149.zip"],
    workspace_file = "protoc_gen_bq_schema/WORKSPACE",
)

load("gclient.bzl", "gclient_repository")

GOMA_REV = "603dfba63fe365a01879029588b48b6f122fe0b8"

gclient_repository(
    name = "goma",
    base_dir = "client/client",
    build_file = "BUILD.goma",
    gclient_vars_windows = "checkout_mingw=True",
    gn_args_linux = "is_debug=false agnostic_build=true use_custom_libcxx=false",
    gn_args_macos_arm64 = "is_debug=false agnostic_build=true target_cpu=\"arm64\"",
    gn_args_macos_x86 = "is_debug=false agnostic_build=true target_cpu=\"x64\"",
    gn_args_windows = "is_debug=false is_clang=true is_win_gcc=false agnostic_build=true is_component_build=true",
    gn_args_windows_dbg = "is_debug=true is_clang=true is_win_gcc=false agnostic_build=true is_component_build=true",
    patches = [
        "//third_party/patches/goma:goma.patch",
        # It seems like on Mac, popen and pclose calls aren't thread safe, which is how we
        # invoke it with scandeps_server. According to
        # https://github.com/microsoft/vcpkg-tool/pull/695/, this maybe due to a bug in
        # popen implementation in Mac that makes it not thread safe. This patch adds a mutex
        # that prevents multi-threaded popen and pclose calls.
        "//third_party/patches/goma:goma_subprocess.patch",
    ],
    remote = "https://chromium.googlesource.com/infra/goma/client",
    revision = GOMA_REV,
)

http_archive(
    name = "rules_foreign_cc",
    patch_args = ["-p1"],
    # TODO(b/203451199): Remove when https://github.com/bazelbuild/rules_foreign_cc/pull/805 is merged.
    patches = ["//third_party/patches/bazel:rfcc.patch"],
    sha256 = "2a4d07cd64b0719b39a7c12218a3e507672b82a97b98c6a89d38565894cf7c51",
    strip_prefix = "rules_foreign_cc-0.9.0",
    url = "https://github.com/bazelbuild/rules_foreign_cc/archive/refs/tags/0.9.0.tar.gz",
)

load("@rules_foreign_cc//foreign_cc:repositories.bzl", "rules_foreign_cc_dependencies")

# See documentation in:
# https://github.com/bazelbuild/rules_foreign_cc/blob/23907e59728326c8a8baf774cdb4e16332c0d002/foreign_cc/repositories.bzl
# For simplicity, leave defaults which also install cmake and make.
rules_foreign_cc_dependencies(
    register_built_tools = True,
    register_default_tools = True,
    register_preinstalled_tools = False,
)

# As explained in github.com/bazelbuild/bazel-gazelle/issues/1115,
# gazelle dependencies should be ran after all their dependencies.
# In part this is due to github.com/bazelbuild/bazel/issues/6864,
# which would silently override our own dependencies with the ones
# from gazelle. Unfortunately, something about the setup of the RE SDK
# and RE API prevents them from being imported before gazelle.
gazelle_dependencies()

# need build_file_generation="off"
# https://github.com/bazelbuild/bazel-gazelle/issues/890
go_repository(
    name = "com_github_bazelbuild_remote_apis_sdks",
    build_file_generation = "off",
    commit = SDK_COMMIT,
    importpath = "github.com/bazelbuild/remote-apis-sdks",
)
# Use the local_reprository configuration below to replace the github version of the SDK with a local version.
#local_repository(
#    name = "com_github_bazelbuild_remote_apis_sdks",
#    path = "/usr/local/google/home/{user}/remote-apis-sdks"
#)

load("@com_github_bazelbuild_remote_apis_sdks//:go_deps.bzl", "remote_apis_sdks_go_deps")

remote_apis_sdks_go_deps()

# Additional dependencies of remote_apis_sdks that cannot be loaded in remote_apis_sdks_go_deps().
http_archive(
    name = "googleapis",
    build_file = "BUILD.googleapis",
    sha256 = "7b6ea252f0b8fb5cd722f45feb83e115b689909bbb6a393a873b6cbad4ceae1d",
    strip_prefix = "googleapis-143084a2624b6591ee1f9d23e7f5241856642f4d",
    urls = ["https://github.com/googleapis/googleapis/archive/143084a2624b6591ee1f9d23e7f5241856642f4d.zip"],
)

go_repository(
    name = "com_github_bazelbuild_remote_apis",
    importpath = "github.com/bazelbuild/remote-apis",
    sum = "h1:Lj8uXWW95oXyYguUSdQDvzywQb4f0jbJWsoLPQWAKTY=",
    version = "v0.0.0-20230411132548-35aee1c4a425",
)

load("@com_github_bazelbuild_remote_apis//:remote_apis_deps.bzl", "remote_apis_go_deps")

remote_apis_go_deps()

# For integration tests that use ninja.
load("@bazel_tools//tools/build_defs/repo:git.bzl", "new_git_repository")

new_git_repository(
    name = "depot_tools",
    build_file_content = """
exports_files(["gn", "gn.bat", "gn.py", "ninja", "ninja-linux64", "ninja-mac", "ninja.exe",])
""",
    commit = "940cd8e20f5451a03737f1fbcc505f7b84dff2b3",
    remote = "https://chromium.googlesource.com/chromium/tools/depot_tools.git",
    shallow_since = "1660680867 +0000",
)

http_archive(
    name = "com_google_googletest",
    strip_prefix = "googletest-7c07a863693b0c831f80473f7c6905d7e458682c",
    urls = ["https://github.com/google/googletest/archive/7c07a863693b0c831f80473f7c6905d7e458682c.zip"],
)
