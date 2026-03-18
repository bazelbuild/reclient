"""Apply llvm_configure to produce a llvm-project repo."""

load("@bazel_tools//tools/build_defs/repo:http.bzl", "http_archive")

# Refer to go/rbe/dev/x/playbook/upgrading_clang_scan_deps
# to update clang-scan-deps version.
LLVM_COMMIT = "6d4a0935c850ec3ddfc70c4ba97b98adc35c676e"

LLVM_SHA256 = "46d963c8cbc1c3f0f06424e61fc739c626317e5b7c91cca8b0ef3cf0c69dd14a"

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
            # Expose the tblgen rule to generate the clang-options.json file,
            # provide dep_scanning alias, and static link clang for Windows.
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
