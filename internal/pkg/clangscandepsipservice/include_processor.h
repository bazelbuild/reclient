// Copyright 2023 Google LLC
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

#ifndef INTERNAL_PKG_CLANGSCANDEPSIPSERVICE_DEPENDENCY_SCANNER_H_
#define INTERNAL_PKG_CLANGSCANDEPSIPSERVICE_DEPENDENCY_SCANNER_H_

#include <condition_variable>
#include <set>
#include <string>
#include <vector>

namespace include_processor {

struct Result {
  std::string directory;
  std::string filename;

  std::set<std::string> dependencies;
  bool used_cache = false;
  std::string error;
  std::condition_variable result_condition;
  std::mutex result_mutex;
  bool result_complete = false;
};

class IncludeProcessor {
 public:
  virtual void ComputeIncludes(const std::string& exec_id,
                               const std::string& cwd,
                               const std::vector<std::string>& args,
                               const std::vector<std::string>& envs,
                               std::shared_ptr<Result> req) = 0;
  virtual ~IncludeProcessor() = default;
};

std::unique_ptr<include_processor::IncludeProcessor> NewDepsScanner();

}  // namespace include_processor

#endif  // INTERNAL_PKG_CLANGSCANDEPSIPSERVICE_DEPENDENCY_SCANNER_H_