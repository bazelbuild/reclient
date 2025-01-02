"""Apply llvm_configure to produce a llvm-project repo."""
load("@bazel_tools//tools/build_defs/repo:http.bzl", "http_archive")
load("@llvm//utils/bazel:configure.bzl", "llvm_configure")


def _llvm_project_impl(ctx):
    http_archive(
        name = "llvm_zlib",
        build_file = "@llvm//utils/bazel/third_party_build:zlib-ng.BUILD",
        sha256 = "22eb64722368a0ef1a834f14eea644dc13309e16e1cd0f0eeec6ccc2c5c2c127",
        strip_prefix = "zlib-ng-2.2.3",
        urls = [
            "https://github.com/zlib-ng/zlib-ng/archive/refs/tags/2.2.3.zip",
        ],
    )
    http_archive(
        name = "llvm_zstd",
        build_file = "@llvm//utils/bazel/third_party_build:zstd.BUILD",
        sha256 = "8c29e06cf42aacc1eafc4077ae2ec6c6fcb96a626157e0593d5e82a34fd403c1",
        strip_prefix = "zstd-1.5.6",
        urls = [
            "https://github.com/facebook/zstd/releases/download/v1.5.6/zstd-1.5.6.tar.gz",
        ],
    )
    llvm_configure(name = "llvm-project")

llvm_project = module_extension(
    implementation = _llvm_project_impl,
)