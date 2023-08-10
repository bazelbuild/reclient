# Copyright 2016 The Bazel Authors. All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This becomes the BUILD file for @local_config_cc// under non-BSD unixes.

load(":cc_toolchain_config.bzl", "cc_toolchain_config")
load(":armeabi_cc_toolchain_config.bzl", "armeabi_cc_toolchain_config")
load("@rules_cc//cc:defs.bzl", "cc_toolchain", "cc_toolchain_suite")

clang_version = "r498229"

clang_short_version = "17"

filegroup(
    name = "empty",
    srcs = [],
)

filegroup(
    name = "compiler_deps",
    srcs = glob(
        ["extra_tools/**"],
        allow_empty = True,
    ),
)

cc_toolchain_suite(
    name = "toolchain-1404",
    tags = ["manual"],
    toolchains = {
        "k8|clang": ":cc-compiler-k8-1404",
        "k8": ":cc-compiler-k8-1404",
        "armeabi-v7a|compiler": ":cc-compiler-armeabi-v7a",
        "armeabi-v7a": ":cc-compiler-armeabi-v7a",
    },
)

cc_toolchain(
    name = "cc-compiler-k8-1404",
    all_files = ":compiler_deps",
    ar_files = ":compiler_deps",
    as_files = ":compiler_deps",
    compiler_files = ":compiler_deps",
    dwp_files = ":empty",
    linker_files = ":compiler_deps",
    module_map = None,
    objcopy_files = ":empty",
    strip_files = ":empty",
    supports_param_files = 1,
    toolchain_config = ":linux_gnu_x86-1404",
    toolchain_identifier = "linux_gnu_x86-1404",
)

cc_toolchain_config(
    name = "linux_gnu_x86-1404",
    abi_libc_version = "glibc_2.19",
    abi_version = "clang",
    compile_flags = [
        "-fstack-protector",
        "-Wall",
        "-Wthread-safety",
        "-Wself-assign",
        "-fcolor-diagnostics",
        "-fno-omit-frame-pointer",
    ],
    compiler = "clang",
    coverage_compile_flags = ["--coverage"],
    coverage_link_flags = ["--coverage"],
    cpu = "k8",
    cxx_builtin_include_directories = [
        "android_prebuilts/x86_64-linux-glibc2.17-4.8/sysroot/usr/include/x86_64-linux-gnu",
        "android_prebuilts/x86_64-linux-glibc2.17-4.8/sysroot/usr/include",
        "android_prebuilts/clang-" + clang_version + "/lib/clang/" + clang_short_version + "/include",
        "android_prebuilts/clang-" + clang_version + "/include/c++/v1",
        "android_prebuilts/clang-" + clang_version + "/include/x86_64-unknown-linux-gnu/c++/v1",
    ],
    cxx_flags = ["-std=c++0x"],
    dbg_compile_flags = ["-g"],
    host_system_name = "i686-unknown-linux-gnu",
    link_flags = [
        "-Wl,-no-as-needed",
        "-Wl,-z,relro,-z,now",
        "-B/usr/local/bin",
        "-B/usr/bin",
        "-Bconfigs/linux_android1404/cc/android_prebuilts/x86_64-linux-glibc2.17-4.8/lib/gcc/x86_64-linux/4.8.3",
        "-Lconfigs/linux_android1404/cc/android_prebuilts/x86_64-linux-glibc2.17-4.8/lib/gcc/x86_64-linux/4.8.3",
        "--sysroot=configs/linux_android1404/cc/android_prebuilts/x86_64-linux-glibc2.17-4.8/sysroot",
        "-Bconfigs/linux_android1404/cc/android_prebuilts/x86_64-linux-glibc2.17-4.8/x86_64-linux/lib64",
        "-Lconfigs/linux_android1404/cc/android_prebuilts/x86_64-linux-glibc2.17-4.8/x86_64-linux/lib64",
        # This rpath points to the relative directory that the binary is expected to run in in Android src.
        "-Wl,-rpath=$ORIGIN/../../clang/host/linux-x86/clang-" + clang_version + "/lib/x86_64-unknown-linux-gnu",
        # This rpath is required to actually build the binary, since the directory needs to exist.
        "-Wl,-rpath=configs/linux_android1404/cc/android_prebuilts/clang-" + clang_version + "/lib/x86_64-unknown-linux-gnu",
        "-stdlib=libc++",
        "-lc++",
    ],
    link_libs = [
        "-lstdc++",
        "-lm",
    ],
    opt_compile_flags = [
        "-g0",
        "-O2",
        "-D_FORTIFY_SOURCE=1",
        "-DNDEBUG",
        "-ffunction-sections",
        "-fdata-sections",
    ],
    opt_link_flags = ["-Wl,--gc-sections"],
    supports_start_end_lib = False,
    target_libc = "glibc_2.19",
    target_system_name = "x86_64-unknown-linux-gnu",
    tool_paths = {
        "ar": "/usr/bin/ar",
        "ld": "/usr/bin/ld",
        "llvm-cov": "/usr/local/bin/llvm-cov",
        "cpp": "/usr/bin/cpp",
        "gcc": "android_prebuilts/clang-" + clang_version + "/bin/clang",
        "dwp": "/usr/bin/dwp",
        "gcov": "/dev/null",
        "nm": "/usr/bin/nm",
        "objcopy": "/usr/bin/objcopy",
        "objdump": "/usr/bin/objdump",
        "strip": "/usr/bin/strip",
    },
    toolchain_identifier = "linux_gnu_x86-1404",
    unfiltered_compile_flags = [
        "-no-canonical-prefixes",
        "-Wno-builtin-macro-redefined",
        "-D__DATE__=\"redacted\"",
        "-D__TIMESTAMP__=\"redacted\"",
        "-D__TIME__=\"redacted\"",
        "-Wno-deprecated-builtins",
        "-Wno-unused-but-set-variable",
        "-Wno-unused-command-line-argument",
        "-Wno-gnu-offsetof-extensions",
        "-stdlib=libc++",
        "--sysroot=configs/linux_android1404/cc/android_prebuilts/x86_64-linux-glibc2.17-4.8/sysroot",
    ],
)

# Android tooling requires a default toolchain for the armeabi-v7a cpu.
cc_toolchain(
    name = "cc-compiler-armeabi-v7a",
    all_files = ":empty",
    ar_files = ":empty",
    as_files = ":empty",
    compiler_files = ":empty",
    dwp_files = ":empty",
    linker_files = ":empty",
    objcopy_files = ":empty",
    strip_files = ":empty",
    supports_param_files = 1,
    toolchain_config = ":stub_armeabi-v7a",
    toolchain_identifier = "stub_armeabi-v7a",
)

armeabi_cc_toolchain_config(name = "stub_armeabi-v7a")
