"""Apply llvm_configure to produce a llvm-project repo."""
load("@bazel_tools//tools/build_defs/repo:http.bzl", "http_archive")
load("@llvm//utils/bazel:configure.bzl", "llvm_configure")


def _llvm_project_impl(ctx):
    http_archive(
        name = "llvm_zlib",
        build_file = "@llvm//utils/bazel/third_party_build:zlib-ng.BUILD",
        sha256 = "e36bb346c00472a1f9ff2a0a4643e590a254be6379da7cddd9daeb9a7f296731",
        strip_prefix = "zlib-ng-2.0.7",
        urls = [
            "https://github.com/zlib-ng/zlib-ng/archive/refs/tags/2.0.7.zip",
        ],
    )
    http_archive(
        name = "llvm_zstd",
        build_file = "@llvm//utils/bazel/third_party_build:zstd.BUILD",
        sha256 = "7c42d56fac126929a6a85dbc73ff1db2411d04f104fae9bdea51305663a83fd0",
        strip_prefix = "zstd-1.5.2",
        urls = [
            "https://github.com/facebook/zstd/releases/download/v1.5.2/zstd-1.5.2.tar.gz",
        ],
    )
    llvm_configure(name = "llvm-project")

llvm_project = module_extension(
    implementation = _llvm_project_impl,
)