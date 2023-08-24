#!/bin/bash
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

# This script checks for diffs between bazel generated files and files in the
# source directory. These files must have the same base name and be in the
# same package.
# This script is meant to be run as a bazel sh_test.
# The env var TEST_WORKSPACE should be set by bazel.
# https://bazel.build/reference/test-encyclopedia#initial-conditions
# In a bazel build file, use as follows:
#
#    native.sh_test(
#        name = name,
#        srcs = ["//tools:diff_gen_vs_workspace.sh"],
#        args = [
#            package_name(),
#            "$(locations :my_gen_files)",
#        ],
#        deps = ["@bazel_tools//tools/bash/runfiles"],
#        data = [
#            ":my_gen_files",
#            ":my_src_files",
#        ],
#        tags = [
#            "local",
#        ],
#    )


# Fail on any error.
set -o errexit
set -o nounset
set -o pipefail

pkg="$1"
for file in "${@:2}"
do
  gen="$(rlocation "$TEST_WORKSPACE/$file")"
  checked_in="$(rlocation "$TEST_WORKSPACE/$pkg/$(basename "$file")")"
  if [ ! -f "$gen" ]; then
      echo "Generated file '$file' not found"
      exit 1
  fi
  if [ ! -f "$checked_in" ]; then
      echo "Source file '$pkg/$(basename "$file")' not found"
      exit 1
  fi
  echo "Checking $pkg/$(basename "$file")"
  diff "$(rlocation "$TEST_WORKSPACE/$file")" "$(rlocation "$TEST_WORKSPACE/$pkg/$(basename "$file")")"
done
