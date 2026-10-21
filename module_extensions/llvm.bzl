"""Apply llvm_configure to produce a llvm-project repo."""

load("@bazel_tools//tools/build_defs/repo:http.bzl", "http_archive")

# Refer to go/rbe/dev/x/playbook/upgrading_clang_scan_deps
# to update clang-scan-deps version.
# llvmorg-23.1.0. Must stay at or past 4f50a725fa19 ("Add
# LangOptions::AllowLiteralDigitSeparator to fix #88896"), which is what lets
# the dependency-directives scanner lex C++14 digit separators in preprocessor
# conditionals -- without it, scanning any LLVM build with libc on the include
# path fails on hardening.h.
LLVM_COMMIT = "ea7d852a70e8bdfaf601d6626a760f9771b2c4b4"

LLVM_SHA256 = "298aa483c883027a38c6a38008464e50546aaa738e8ff9b9bd1045f5268646c0"

def _llvm_version_repo_impl(ctx):
    ctx.file("BUILD.bazel")
    ctx.file("defs.bzl", content = "LLVM_COMMIT = \"" + LLVM_COMMIT + "\"")

_llvm_version_repo = repository_rule(
    implementation = _llvm_version_repo_impl,
)

def _llvm_extension_impl(ctx):
    http_archive(
        name = "llvm-raw",
        build_file_content = "#empty",
        patch_args = ["-p1"],
        patches = [
            # Expose the tblgen rule to generate the clang-options.json file,
            # provide dep_scanning alias, and static link clang for Windows.
            "//third_party/patches/llvm:llvm-bzl-tblgen.patch",
            # Avoid the @llvm//platforms/config settings, which come from a
            # registry module that requires a newer Bazel than .bazelversion
            # pins. See the patch header.
            "//third_party/patches/llvm:llvm-bzl-platform-config.patch",
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
