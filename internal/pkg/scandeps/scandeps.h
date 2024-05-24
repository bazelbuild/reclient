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

#ifndef INTERNAL_PKG_SCANDEPS_SCANDEPS_H_
#define INTERNAL_PKG_SCANDEPS_SCANDEPS_H_

#include <grpcpp/grpcpp.h>

#include <atomic>
#include <condition_variable>
#include <ctime>
#include <string>
#include <thread>

#include "api/scandeps/cppscandeps.grpc.pb.h"
#include "include_processor.h"

namespace include_processor {

class ScandepsService final : public scandeps::CPPDepsScanner::Service {
 public:
  ScandepsService(std::function<void()> shutdown_server,
                  const char* process_name, std::string cache_dir,
                  std::string log_dir, int cache_file_max_mb,
                  bool use_deps_cache, uint32_t experimental_deadlock,
                  uint32_t experimental_segfault);
  ScandepsService(const ScandepsService& other) = delete;
  ScandepsService& operator=(const ScandepsService& other) = delete;
  ~ScandepsService();
  grpc::Status ProcessInputs(grpc::ServerContext*,
                             const scandeps::CPPProcessInputsRequest*,
                             scandeps::CPPProcessInputsResponse*) override;
  grpc::Status Status(grpc::ServerContext*, const google::protobuf::Empty*,
                      scandeps::StatusResponse*) override;
  grpc::Status Shutdown(grpc::ServerContext*, const google::protobuf::Empty*,
                        scandeps::StatusResponse*) override;
  grpc::Status Capabilities(grpc::ServerContext*,
                            const google::protobuf::Empty*,
                            scandeps::CapabilitiesResponse*) override;

 private:
  std::thread init_thread_;
  std::mutex init_mutex_;
  std::condition_variable init_cv_;
  std::time_t started_;
  std::atomic<std::size_t> current_actions_;
  std::atomic<std::size_t> completed_actions_;
  std::function<void()> shutdown_server_;
  std::unique_ptr<IncludeProcessor> deps_scanner_cache_;
  const char* process_name_;
  const std::string cache_dir_;
  const std::string log_dir_;
  const int cache_file_max_mb_;
  const bool use_deps_cache_;
#ifdef _WIN32
  bool wsa_initialized_;
#endif

  // Configurations used for experiments
  std::mutex exp_mutex_;
  uint32_t experimental_deadlock_;
  uint32_t experimental_segfault_;

  void PopulateStatusResponse(scandeps::StatusResponse*);
};

}  // namespace include_processor

#endif  // INTERNAL_PKG_SCANDEPSIPSERVICE_DEPENDENCY_SCANNER_H_