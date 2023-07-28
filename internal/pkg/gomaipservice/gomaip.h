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

#ifndef CMD_SCANDEPS_SKELETON_SKELETON_H_
#define CMD_SCANDEPS_SKELETON_SKELETON_H_

#include <grpcpp/grpcpp.h>

#include <atomic>
#include <condition_variable>
#include <ctime>
#include <thread>

#include "api/scandeps/cppscandeps.grpc.pb.h"

class GomaIPServiceImpl final : public scandeps::CPPDepsScanner::Service {
 public:
  GomaIPServiceImpl(std::function<void()> shutdown_server,
                    const char* process_name, std::string cache_dir,
                    std::string log_dir, int cache_file_max_mb, bool use_deps_cache,
                    uint32_t experimental_deadlock, uint32_t experimental_segfault);
  ~GomaIPServiceImpl();
  grpc::Status ProcessInputs(grpc::ServerContext*,
                             const scandeps::CPPProcessInputsRequest*,
                             scandeps::CPPProcessInputsResponse*) override;
  grpc::Status Status(grpc::ServerContext*, const google::protobuf::Empty*,
                      scandeps::StatusResponse*) override;
  grpc::Status Shutdown(grpc::ServerContext*, const google::protobuf::Empty*,
                        scandeps::StatusResponse*) override;

  void InitGoma();
 private:
  std::thread init_thread_;
  std::mutex init_mutex_;
  std::condition_variable init_cv_;
  std::time_t started_;
  std::atomic<std::size_t> current_actions_;
  std::atomic<std::size_t> completed_actions_;
  std::function<void()> shutdown_server_;
  void *deps_scanner_cache_;
  const char* process_name_;
  std::string cache_dir_;
  std::string log_dir_;
  int cache_file_max_mb_;
  bool use_deps_cache_;
#ifdef _WIN32
  bool wsa_initialized_;
#endif

  // Configurations used for experiments
  std::mutex exp_mutex_;
  uint32_t experimental_deadlock_;
  uint32_t experimental_segfault_;

  void PopulateStatusResponse(scandeps::StatusResponse*);
};

#endif  // CMD_SCANDEPS_SKELETON_SKELETON_H_
