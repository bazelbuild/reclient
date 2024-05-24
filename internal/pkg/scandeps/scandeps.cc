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

#ifdef _WIN32
#define GLOG_NO_ABBREVIATED_SEVERITIES
#endif

#include "scandeps.h"

#include <glog/logging.h>

#include <condition_variable>
#include <cstdlib>

#include "include_processor.h"

#if defined(_WIN32)
#include <winsock2.h>
#define WSA_VERSION MAKEWORD(2, 2)  // using winsock 2.2
#endif

#include "internal/pkg/version/version.h"

using grpc::ServerContext;
using grpc::Status;
using grpc::StatusCode;

using scandeps::CapabilitiesResponse;
using scandeps::CPPProcessInputsRequest;
using scandeps::CPPProcessInputsResponse;
using scandeps::StatusResponse;

namespace include_processor {
ScandepsService::ScandepsService(std::function<void()> shutdown_server,
                                 const char* process_name,
                                 std::string cache_dir, std::string log_dir,
                                 int cache_file_max_mb, bool use_deps_cache,
                                 uint32_t experimental_deadlock,
                                 uint32_t experimental_segfault)
    : current_actions_(0),
      completed_actions_(0),
      shutdown_server_(shutdown_server),
      deps_scanner_cache_(nullptr),
      process_name_(process_name),
      cache_dir_(cache_dir),
      log_dir_(log_dir),
      cache_file_max_mb_(cache_file_max_mb),
      use_deps_cache_(use_deps_cache),
      experimental_deadlock_(experimental_deadlock),
      experimental_segfault_(experimental_segfault) {
  started_ = std::time(0);
#ifdef _WIN32
  WSADATA WSAData = {};
  if (WSAStartup(WSA_VERSION, &WSAData) != 0) {
    // Tell the user that we could not find a usable WinSock DLL.
    LOG(ERROR) << "Failed to initialize Winsock API";
    if (LOBYTE(WSAData.wVersion) != LOBYTE(WSA_VERSION) ||
        HIBYTE(WSAData.wVersion) != HIBYTE(WSA_VERSION)) {
      LOG(ERROR) << "Incorrect winsock version, required 2.2 and up";
    }
    WSACleanup();
  } else {
    wsa_initialized_ = true;
  }
#endif
  std::unique_lock<std::mutex> exp_lock(init_mutex_);
  if (deps_scanner_cache_ != nullptr) {
    LOG(WARNING) << INPUT_PROCESSOR
        " dependency scanner is already initialized and will "
        "not be reinitialized";
  } else {
    std::time_t start = std::time(0);
    deps_scanner_cache_ = std::make_unique<include_processor::IncludeProcessor>(
        process_name_, cache_dir_.c_str(), log_dir_.c_str(), cache_file_max_mb_,
        use_deps_cache_);
    std::time_t end = std::time(0);
    if (!deps_scanner_cache_) {
      LOG(FATAL) << "Unable to create new " INPUT_PROCESSOR
                    " dependency scanner";
    }
    LOG(INFO) << "Initializing " INPUT_PROCESSOR " dependency scanner took "
              << end - start << " seconds";
  }
  init_cv_.notify_all();
}

ScandepsService::~ScandepsService() {
#ifdef _WIN32
  if (wsa_initialized_) {
    WSACleanup();
  }
#endif
}

Status ScandepsService::ProcessInputs(ServerContext* context,
                                      const CPPProcessInputsRequest* request,
                                      CPPProcessInputsResponse* response) {
  (void)context;

  // Count an action
  ++current_actions_;
  // This is a hack to simulate a segfault.
  if (experimental_segfault_ > 0) {
    std::unique_lock<std::mutex> exp_lock(exp_mutex_);
    if (experimental_segfault_ > 0) {
      if (--experimental_segfault_ == 0) {
        LOG(WARNING) << "Service will abort now.";
        google::FlushLogFiles(0);
        std::abort();
      }
    }
  }
  // This is a hack to simulate a deadlock.
  if (experimental_deadlock_ > 0) {
    std::unique_lock<std::mutex> exp_lock(exp_mutex_);
    if (experimental_deadlock_ > 0) {
      if (--experimental_deadlock_ == 0) {
        // Infinite loop to simulate a deadlock
        LOG(WARNING) << "Service will deadlock now.";
        google::FlushLogFiles(0);
        exp_lock.unlock();  // but we don't want to cause an actual deadlock
        while (true);
      }
    }
  }

  if (deps_scanner_cache_ == nullptr) {
    // Not fully initialized.
    // Block on the lock if it's in the process of initialization.
    std::unique_lock<std::mutex> init_lock(init_mutex_);
    if (deps_scanner_cache_ == nullptr) {
      // If we're here, ProcessInputs somehow beat the init thread.
      // Release the lock and wait for the init thread to signal that it's done.
      init_cv_.wait(init_lock, [&] { return deps_scanner_cache_ != nullptr; });
    }
  }

  auto result = std::make_shared<include_processor::Result>();
  result->directory = request->directory();
  result->filename = request->filename();
  std::unique_lock<std::mutex> result_lock(result->result_mutex);
  deps_scanner_cache_->ComputeIncludes(
      request->exec_id(), request->directory(),
      std::vector<std::string>(request->command().begin(),
                               request->command().end()),
      std::vector<std::string>(request->cmd_env().begin(),
                               request->cmd_env().end()),
      result);
  result->result_condition.wait(
      result_lock, [&result]() { return result->result_complete; });

  if (result->error.size() > 0) {
    std::ostringstream command;
    std::copy(request->command().begin(), request->command().end(),
              std::ostream_iterator<std::string>(command, " "));
    LOG(ERROR) << INPUT_PROCESSOR
        " encountered the following error processing a command: \""
               << result->error << "\"; Command: [" << command.str() << "]";
  }
  response->set_error(result->error);
  response->set_used_cache(result->used_cache);
  for (auto dependency : result->dependencies) {
    response->add_dependencies(dependency);
  }

  // TODO b/268656738: refactor this to common service code
  // Count the action as complete
  --current_actions_;
  ++completed_actions_;
  return grpc::Status::OK;
}

Status ScandepsService::Status(ServerContext* context,
                               const google::protobuf::Empty* request,
                               StatusResponse* response) {
  (void)context;
  (void)request;

  VLOG(1) << "Status request received.";
  PopulateStatusResponse(response);

  return grpc::Status::OK;
}

Status ScandepsService::Shutdown(ServerContext* context,
                                 const google::protobuf::Empty* request,
                                 StatusResponse* response) {
  (void)context;
  (void)request;

  VLOG(1) << "Shutdown request received.";
  if (shutdown_server_ != nullptr) {
    VLOG(2) << "Calling server shutdown.";
    shutdown_server_();
  }
  PopulateStatusResponse(response);

  return grpc::Status::OK;
}

Status ScandepsService::Capabilities(ServerContext* context,
                                     const google::protobuf::Empty* request,
                                     CapabilitiesResponse* response) {
  (void)context;
  (void)request;

  VLOG(1) << "Capabilities request received.";
  response->set_caching(include_processor::IncludeProcessor::caching);
  response->set_expects_resource_dir(
      include_processor::IncludeProcessor::expects_resource_dir);
  return grpc::Status::OK;
}

void ScandepsService::PopulateStatusResponse(
    scandeps::StatusResponse* response) {
  response->set_name(INPUT_PROCESSOR);
  response->set_version(RECLIENT_VERSION);
  google::protobuf::Duration* uptime = new google::protobuf::Duration();
  uptime->set_seconds(std::time(0) - started_);
  response->set_allocated_uptime(uptime);  // gRPC library takes care of cleanup
  response->set_completed_actions(completed_actions_);
  response->set_running_actions(current_actions_);
}
}  // namespace include_processor
