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

#include "server.h"

#ifdef _WIN32
# define GLOG_NO_ABBREVIATED_SEVERITIES
#endif
#include <glog/logging.h>
#include <grpcpp/grpcpp.h>

#include "api/scandeps/cppscandeps.grpc.pb.h"
#include "scandeps.h"

using includescanner::CPPDepsScanner;
using includescanner::CPPProcessInputsRequest;
using includescanner::CPPProcessInputsResponse;
using includescanner::StatusResponse;

// Argument to start_server
typedef struct ScandepsServerWrapper {
  grpc::Server *grpc_server;
  bool *running;
  std::condition_variable *shutdown_condition;
  std::mutex *shutdown_mutex;
  uint32_t *shutdown_delay_seconds;
} ScandepsServerWrapper;

void *StartServer(void *server) {
  ScandepsServerWrapper *tserver = (ScandepsServerWrapper*)(server);
  std::unique_lock<std::mutex> shutdown_lock(*(tserver->shutdown_mutex));
  *(tserver->running) = true;
  tserver->shutdown_condition->wait(shutdown_lock, [tserver]() { return !(*(tserver->running)); });
  LOG(INFO) << "Stopping server.";
  auto deadline = std::chrono::system_clock::now() +
                  std::chrono::seconds(*(tserver->shutdown_delay_seconds));
  tserver->grpc_server->Shutdown(deadline);
  return NULL;
}

ScandepsServer::ScandepsServer(const std::string &server_address,
                               const std::string &cache_dir,
                               const std::string &log_dir,
                               int cache_size_max_mb,
                               bool use_deps_cache,
                               uint32_t shutdown_delay_seconds,
                               uint32_t experimental_deadlock,
                               uint32_t experimental_segfault)
    : running_(false),
      server_address_(server_address),
      cache_dir_(cache_dir),
      log_dir_(log_dir),
      cache_size_max_mb_(cache_size_max_mb),
      use_deps_cache_(use_deps_cache),
      shutdown_delay_seconds_(shutdown_delay_seconds),
      experimental_deadlock_(experimental_deadlock),
      experimental_segfault_(experimental_segfault) {}

ScandepsServer::~ScandepsServer() {}

bool ScandepsServer::RunServer(const char* process_name) {
  includescanner::CPPDepsScanner::Service *service =
      newDepsScanner([&]() { this->StopServer(); }, process_name, cache_dir_.c_str(), log_dir_.c_str(),
        cache_size_max_mb_, use_deps_cache_, experimental_deadlock_, experimental_segfault_);

  grpc::ServerBuilder builder;
  // Listen on the given address without any authentication mechanism.
  builder.AddListeningPort(server_address_, grpc::InsecureServerCredentials());
  builder.RegisterService(service);
  grpc_server_ = builder.BuildAndStart();
  if (grpc_server_ == nullptr) {
    LOG(ERROR) << "Failed to bind to address " << server_address_;
    return false;
  }
  LOG(INFO) << "Server listening on " << server_address_;
  google::FlushLogFiles(0);

#if !defined(__linux__) || __GLIBC_NEW__
  // Use C++ threads
  std::thread run_thread = std::thread([&]() {
    std::unique_lock<std::mutex> shutdown_lock(shutdown_mutex_);
    running_ = true;
    shutdown_condition_.wait(shutdown_lock, [this]() { return !running_; });
    LOG(INFO) << "Stopping server.";
    auto deadline = std::chrono::system_clock::now() +
                    std::chrono::seconds(shutdown_delay_seconds_);
    grpc_server_->Shutdown(deadline);
  });
  grpc_server_->Wait();
  run_thread.join();
#else
  // Must use pthread for 1604 compatibility
  ScandepsServerWrapper tserver;
  tserver.grpc_server = grpc_server_.get();
  tserver.running = &running_;
  tserver.shutdown_condition = &shutdown_condition_;
  tserver.shutdown_mutex = &shutdown_mutex_;
  tserver.shutdown_delay_seconds = &shutdown_delay_seconds_;

  pthread_t run_thread;
  int tret = pthread_create(&run_thread, NULL, StartServer, (void*)&tserver);
  if (tret != 0) {
    LOG(ERROR) << "Failed to start server thread; aborting.";
    grpc_server_->Shutdown();
  } else {
    grpc_server_->Wait();
    pthread_join(run_thread, NULL);
  }
#endif
  LOG(INFO) << "Server stopped, destroying deps scanner.";
  deleteDepsScanner(service);
  return true;
}

void ScandepsServer::StopServer() {
  if (running_) {
    std::lock_guard<std::mutex> shutdown_lock(shutdown_mutex_);
    running_ = false;
    shutdown_condition_.notify_all();
  }
}
