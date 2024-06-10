#!/usr/bin/env bash
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

# This script regenerates all `.pb.go` files under //api/... by running
# all *_copy_pbgo_files rules under the //api/... directory generated
# by the go_proto_checkedin_test macro in //tools/build_defs.bzl
# Use as follows:
#
# ./scripts/regenpbgo.sh

set -eux

for r in $(bazelisk query "filter('_copy_pbgo_files', kind(sh_binary, @@//...))")
do
  bazelisk run "$r"
done
