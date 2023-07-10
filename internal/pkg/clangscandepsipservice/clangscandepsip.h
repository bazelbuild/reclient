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

#include <grpcpp/grpcpp.h>

#include <atomic>
#include <condition_variable>
#include <ctime>

#include "api/scandeps/cppscandeps.grpc.pb.h"

class ClangscandepsIPServiceImpl final : public includescanner::CPPDepsScanner::Service {
 public:
  ClangscandepsIPServiceImpl(const char* process_name, std::function<void()> shutdown_server);
  ~ClangscandepsIPServiceImpl();
  grpc::Status ProcessInputs(grpc::ServerContext*,
                             const includescanner::CPPProcessInputsRequest*,
                             includescanner::CPPProcessInputsResponse*) override;
  grpc::Status Status(grpc::ServerContext*, const google::protobuf::Empty*,
                      includescanner::StatusResponse*) override;
  grpc::Status Shutdown(grpc::ServerContext*, const google::protobuf::Empty*,
                        includescanner::StatusResponse*) override;
  void InitClangscandeps();

 private:
  struct ClangScanDepsResult;

  std::atomic<std::size_t> completed_actions_;
  std::atomic<std::size_t> current_actions_;
  std::condition_variable init_cv_;
  std::function<void()> shutdown_server_;
  std::mutex init_mutex_;
  std::time_t started_;
  void *deps_scanner_cache_;

  std::vector<std::string> Parse(std::string deps);
  void PopulateStatusResponse(includescanner::StatusResponse*);
  void ScanDependenciesResult(ClangScanDepsResult& clangscandeps_result,
                              void *impl, int argc, const char** argv,
                              const char* filename, const char* dir,
                              char** deps, char** errs);
};