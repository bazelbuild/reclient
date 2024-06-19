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

# This script is used to print current git SHA so that Bazel's workspace command
# can consume it.

# Use googler sha script if it exists, this script does not exist in the
# open source repo.
test -f scripts/sha_internal.sh && exec scripts/sha_internal.sh

if gitSha=$(git rev-parse HEAD); then
  echo STABLE_VERSION_SHA "$gitSha"
else
  echo STABLE_VERSION_SHA unknown
fi