"""Apply llvm_configure to produce a llvm-project repo."""
load("@bazel_tools//tools/build_defs/repo:http.bzl", "http_archive")
load("@llvm//utils/bazel:configure.bzl", "llvm_configure")


def _llvm_project_impl(ctx):
    http_archive(
        name = "llvm_zlib",
        build_file = "@llvm//utils/bazel/third_party_build:zlib-ng.BUILD",
        sha256 = "5b01369b98c951d79b8aec0dcd74ad539db13d9d333366cd3ba0d20743ee90b0",
        strip_prefix = "zlib-ng-2.3.3",
        urls = [
            "https://github.com/zlib-ng/zlib-ng/archive/refs/tags/2.3.3.zip",
        ],
    )
    http_archive(
        name = "llvm_zstd",
        build_file = "@llvm//utils/bazel/third_party_build:zstd.BUILD",
        sha256 = "2ba599ae2a257976278e7e71e998433a387bfd18aa7b78678229fd5d84f5e9b8",
        strip_prefix = "zstd-1.6.0",
        urls = [
            "https://github.com/facebook/zstd/releases/download/v1.6.0/zstd-1.6.0.tar.gz",
        ],
    )
    llvm_configure(name = "llvm-project")

llvm_project = module_extension(
    implementation = _llvm_project_impl,
)