#!/bin/sh
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

# Script to run gofmt, golint, and gazelle before commiting.
# To install, use the following command:
# ./scripts/install_precommit.sh

STAGED_GO_FILES=$(git diff --cached --name-only --diff-filter=d | grep ".go$")

# Ensures that non-interactive shell sessions can run Go tools
if command -v go &>/dev/null; then
    export PATH="$PATH:$(go env GOPATH)/bin"
fi

GAZELLEPASS=true
echo Running Gazelle...
if ! bazelisk run --ui_event_filters=-info,-stderr --noshow_progress //:gazelle; then
  GAZELLEPASS=false
fi

LINTPASS=true
echo Running gofmt...
bazelisk run --ui_event_filters=-info,-stderr --noshow_progress //linters:gofmt -- -w .
echo Running golint...
if ! bazelisk run --ui_event_filters=-info,-stderr --noshow_progress //linters:golint -- "-set_exit_status" ./...;  then
  printf "\t\033[31mgolint\033[0m \033[0;30m\033[41mFAILURE!\033[0m\n"
  LINTPASS=false
else
  printf "\t\033[32mgolint\033[0m \033[0;30m\033[42mpass\033[0m\n"
fi

PASS=true
if ! $LINTPASS; then
  printf "\033[0;30m\033[41mThere are lint errors. Please fix!\033[0m\n"
  PASS=false
fi

if ! $GAZELLEPASS; then
  printf "\033[0;30m\033[41mbazelisk run //:gazelle failed. Please fix the errors and try again.\033[0m\n"
  PASS=false
fi


if ! git diff --exit-code &> /dev/null; then
  printf "\033[0;30m\033[41mPrecommit made changes to source. Please check the changes and re-stage files.\033[0m\n"
  PASS=false
fi

if ! $PASS; then
  printf "\033[0;30m\033[41mCOMMIT FAILED\033[0m\n"
  exit 1
else
  printf "\033[0;30m\033[42mCOMMIT SUCCEEDED\033[0m\n"
fi
