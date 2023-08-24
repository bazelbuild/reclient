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

# This script copies a list of bazel generated files to the source directory.
# This script is meant to be run as a bazel sh_binary.
# The env var BUILD_WORKSPACE_DIRECTORY should be set by bazel.
# In a bazel build file, use as follows:
#
#     native.sh_binary(
#        name = "my_copy_target",
#        srcs = ["//tools:copy_to_workspace.sh"],
#        args = [
#            package_name(),
#            "$(execpaths :my_gen_files)",
#        ],
#        data = [
#            ":my_gen_files",
#        ],
#    )

# Fail on any error.
set -o errexit
set -o nounset
set -o pipefail

pkg="$1"
cd "$BUILD_WORKSPACE_DIRECTORY" || exit 1
for file in "${@:2}"
do
  cp -f "$file" "$pkg/$(basename "$file")"
done
