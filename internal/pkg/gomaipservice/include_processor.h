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

// IncludeProcessor receives include processor requests and manages the worker
// pools necessary to execute such requests. It provides common functionality
// across requests like CompilerInfo querying. The code mostly comes from
// https://chromium.googlesource.com/infra/goma/client/+/refs/heads/main/client/compile_service.cc

#ifndef INTERNAL_PKG_GOMAIPSERVICE_INCLUDE_PROCESSOR_H_
#define INTERNAL_PKG_GOMAIPSERVICE_INCLUDE_PROCESSOR_H_

#include <condition_variable>
#include <set>
#include <string>
#include <vector>

namespace include_processor {

struct GomaResult {
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
                               std::shared_ptr<GomaResult> req) = 0;
  virtual void Quit() = 0;
  virtual ~IncludeProcessor() = default;
};

std::unique_ptr<include_processor::IncludeProcessor> NewDepsScanner(
    const char* process_name, const char* cache_dir, const char* log_dir,
    int cache_file_max_mb, bool use_deps_cache);

}  // namespace include_processor

#endif  // INTERNAL_PKG_GOMAIPSERVICE_INCLUDE_PROCESSOR_H_