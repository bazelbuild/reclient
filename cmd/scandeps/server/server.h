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

#ifndef CMD_SCANDEPS_SERVER_SERVER_H_
#define CMD_SCANDEPS_SERVER_SERVER_H_

#include <grpcpp/grpcpp.h>

#include <condition_variable>
#include <ctime>
#include <iostream>
#include <memory>
#include <string>
#include <thread>

#include "api/scandeps/cppscandeps.grpc.pb.h"
#include "scandeps.h"

class ScandepsServer {
 public:
  ScandepsServer(const std::string &server_address,
                 const std::string &cache_dir, const std::string &log_dir,
                 int cache_size_max_mb, bool use_deps_cache,
                 uint32_t shutdown_delay_seconds,
                 uint32_t experimental_deadlock,
                 uint32_t experimental_segfault);
  ~ScandepsServer();

  bool RunServer(const char *process_name);
  void StopServer();

 private:
  std::unique_ptr<grpc::Server> grpc_server_;

  bool running_;
  std::condition_variable shutdown_condition_;
  std::mutex shutdown_mutex_;

  const std::string server_address_;
  const std::string cache_dir_;
  const std::string log_dir_;
  int cache_size_max_mb_;
  bool use_deps_cache_;
  uint32_t shutdown_delay_seconds_;

  // Configurations used for experiments
  uint32_t experimental_deadlock_;
  uint32_t experimental_segfault_;
};

#endif  // CMD_SCANDEPS_SERVER_SERVER_H_