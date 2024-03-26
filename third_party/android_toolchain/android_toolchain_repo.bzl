# Copyright 2023 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

''' Provides android_toolchain_repostitory '''

GLIBC_DIR = "android_prebuilts/glibc"
CLANG_DIR = "android_prebuilts/clang-latest"

def _android_prebuilts_repostitory_rule(ctx):
    ctx.download_and_extract(url = ctx.attr.glibc_url, sha256 = ctx.attr.glibc_sha256, output = GLIBC_DIR)
    ctx.download_and_extract(url = ctx.attr.clang_url, sha256 = ctx.attr.clang_sha256, output = CLANG_DIR)
    ctx.execute(["find", ".", "-type", "f", "-name", "BUILD.bazel", "-delete"], working_directory = ".")
    ctx.file("BUILD.bazel", "")

# This is split out so that the downloaded and extracted archives can be cached separately
_android_prebuilts_repostitory = repository_rule(
    implementation = _android_prebuilts_repostitory_rule,
    attrs = {
        "clang_url": attr.string(),
        "clang_sha256": attr.string(),
        "glibc_url": attr.string(),
        "glibc_sha256": attr.string(),
    },
)

def _android_toolchain_repostitory_rule(ctx):
    # We cannot just pass in @...//:android_prebuilts as a label as that doesnt get correctly detected as a directory
    # https://github.com/bazelbuild/bazel/issues/3901
    ctx.symlink(ctx.path(ctx.attr.prebuilts_repo).dirname.get_child("android_prebuilts"), "android_prebuilts")
    ctx.symlink(Label("@//configs/linux/cc:cc_toolchain_config.bzl"), "cc_toolchain_config.bzl")
    ctx.template("BUILD.bazel", Label("@//third_party/android_toolchain:BUILD.androidtoolchain"), {
        "{repo_name}": ctx.name,
        "{glibc_dir}": GLIBC_DIR,
        "{clang_dir}": CLANG_DIR,
        "{parent_platform}": ctx.attr.parent_platform,
    })


_android_toolchain_repostitory = repository_rule(
    implementation = _android_toolchain_repostitory_rule,
    attrs = {
        "prebuilts_repo": attr.label(),
        "parent_platform": attr.string(),
    },
)

def android_toolchain_repostitory(name, clang_url, clang_sha256, glibc_url, glibc_sha256, parent_platform = "@local_config_platform//:host"):
    _android_prebuilts_repostitory(
        name = name + "_android_prebuilts",
        clang_url = clang_url,
        clang_sha256 = clang_sha256,
        glibc_url = glibc_url,
        glibc_sha256 = glibc_sha256,
    )
    _android_toolchain_repostitory(
        name = name,
        prebuilts_repo = "@@" + name + "_android_prebuilts//:BUILD.bazel",
        parent_platform = parent_platform,
    )

