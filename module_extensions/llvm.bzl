"""Apply llvm_configure to produce a llvm-project repo."""

load("@bazel_tools//tools/build_defs/repo:http.bzl", "http_archive")

# Refer to go/rbe/dev/x/playbook/upgrading_clang_scan_deps
# to update clang-scan-deps version.
LLVM_COMMIT = "82e851a407c52d65ce65e7aa58453127e67d42a0"

LLVM_SHA256 = "c45d3e776d8f54362e05d4d5da8b559878077241d84c81f69ed40f11c0cdca8f"

def _llvm_version_repo_impl(ctx):
    ctx.file("BUILD.bazel")
    ctx.file("defs.bzl", content = "LLVM_COMMIT = \"" + LLVM_COMMIT + "\"")

_llvm_version_repo = repository_rule(
    implementation = _llvm_version_repo_impl,
)

def _llvm_extension_impl(ctx):
    http_archive(
        name = "llvm",
        build_file_content = "#empty",
        patch_args = ["-p1"],
        patches = [
            # For simplicity, we expose the tblgen rule for a binary we use.
            "//third_party/patches/llvm:llvm-bzl-tblgen.patch",
            # This patch picks the right version of assembly files to build libSupport
            # on Windows. Refer to https://github.com/llvm/llvm-project/issues/54685
            # for the corresponding fix to CMake files.
            "//third_party/patches/llvm:llvm-bazel-libsupport.patch",
            # Replace @llvm-raw with @llvm so we can build llvm inside of re-client.
            # In the llvm-project checkout, @llvm-raw is defined the WORKSPACE file
            # and point to the root of llvm-project; However, when we invoke the
            # line `llvm_configure(name = "llvm-project")` below, in the bzl file,
            # @llvm//utils/bazel:configure.bzl, @llvm-raw is not pre-defined.
            "//third_party/patches/llvm:llvm-bzl-config.patch",
            "//third_party/patches/llvm:llvm-project-overlay-exectools.patch",
            "//third_party/patches/llvm:llvm-delay-sanitizer-args-parsing.patch",
        ],
        sha256 = LLVM_SHA256,
        strip_prefix = "llvm-project-%s" % LLVM_COMMIT,
        urls = [
            "https://mirror.bazel.build/github.com/llvm/llvm-project/archive/%s.zip" % LLVM_COMMIT,
            "https://github.com/llvm/llvm-project/archive/%s.zip" % LLVM_COMMIT,
        ],
    )
    _llvm_version_repo(name = "llvm_version")

llvm_extension = module_extension(
    implementation = _llvm_extension_impl,
)
