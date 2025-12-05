"""Apply llvm_configure to produce a llvm-project repo."""
load("@bazel_tools//tools/build_defs/repo:http.bzl", "http_archive")
load("@llvm//utils/bazel:configure.bzl", "llvm_configure")


def _llvm_project_impl(ctx):
    http_archive(
        name = "llvm_zlib",
        build_file = "@llvm//utils/bazel/third_party_build:zlib-ng.BUILD",
        sha256 = "58c948c5b36c9718cdf81c36c94dbe92eb6cdfe49428e0f38c09d9efc907f24d",
        strip_prefix = "zlib-ng-2.3.2",
        urls = [
            "https://github.com/zlib-ng/zlib-ng/archive/refs/tags/2.3.2.zip",
        ],
    )
    http_archive(
        name = "llvm_zstd",
        build_file = "@llvm//utils/bazel/third_party_build:zstd.BUILD",
        sha256 = "eb33e51f49a15e023950cd7825ca74a4a2b43db8354825ac24fc1b7ee09e6fa3",
        strip_prefix = "zstd-1.5.7",
        urls = [
            "https://github.com/facebook/zstd/releases/download/v1.5.7/zstd-1.5.7.tar.gz",
        ],
    )
    llvm_configure(name = "llvm-project")

llvm_project = module_extension(
    implementation = _llvm_project_impl,
)