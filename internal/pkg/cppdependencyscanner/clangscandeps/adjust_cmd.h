// Copyright 2024 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

#include <set>
#include <string>
#include <vector>

#ifndef INTERNAL_PKG_CPPDEPENDENCYSCANNER_CLANGSCANNER_ADJUST_CMD_H_
#define INTERNAL_PKG_CPPDEPENDENCYSCANNER_CLANGSCANNER_ADJUST_CMD_H_
namespace clangscandeps {
// Adjusts the given command to be compatible with clangscandeps.
void AdjustCmd(std::vector<std::string>& cmd, std::string filename,
               const std::set<std::string>& ignoredPlugins);
}  // namespace clangscandeps

#endif  // INTERNAL_PKG_CPPDEPENDENCYSCANNER_CLANGSCANNER_ADJUST_CMD_H_