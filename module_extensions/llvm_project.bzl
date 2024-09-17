"""Apply llvm_configure to produce a llvm-project repo."""
load("@bazel_tools//tools/build_defs/repo:http.bzl", "http_archive")
load("@llvm//utils/bazel:configure.bzl", "llvm_configure")


def _llvm_project_impl(ctx):
    http_archive(
        name = "llvm_zlib",
        build_file = "@llvm//utils/bazel/third_party_build:zlib-ng.BUILD",
        sha256 = "c4ed7d7e77b8793f612c37822cbc627d6152f2f1cad49050e42f1159b0e69e1a",
        strip_prefix = "zlib-ng-2.2.2",
        urls = [
            "https://github.com/zlib-ng/zlib-ng/archive/refs/tags/2.2.2.zip",
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