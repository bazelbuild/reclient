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

#ifdef _WIN32
#define GLOG_NO_ABBREVIATED_SEVERITIES
#endif

#include "gomaip.h"

#include <glog/logging.h>

#include <condition_variable>
#include <cstdlib>

#if defined(_WIN32)
#include <winsock2.h>
#define WSA_VERSION MAKEWORD(2, 2)  // using winsock 2.2
#endif

#include "include_processor.h"
#include "pkg/version/version.h"

using grpc::ServerContext;
using grpc::Status;
using grpc::StatusCode;

using scandeps::CapabilitiesResponse;
using scandeps::CPPProcessInputsRequest;
using scandeps::CPPProcessInputsResponse;
using scandeps::StatusResponse;

// Implementation of newDepsScanner from scandeps.h
// TODO (b/268656738): remove experimental_deadlock and experimental_segfault
scandeps::CPPDepsScanner::Service* newDepsScanner(
    std::function<void()> shutdown_server, const char* process_name,
    const char* cache_dir, const char* log_dir, int deps_cache_max_mb,
    bool enable_deps_cache, uint32_t experimental_deadlock,
    uint32_t experimental_segfault) {
  return new GomaIPServiceImpl(shutdown_server, process_name, cache_dir,
                               log_dir, deps_cache_max_mb, enable_deps_cache,
                               experimental_deadlock, experimental_segfault);
}
// Implementation of deleteDepsScanner from scandeps.h
bool deleteDepsScanner(scandeps::CPPDepsScanner::Service* grpc_service_impl) {
  GomaIPServiceImpl* gomaip_service =
      static_cast<GomaIPServiceImpl*>(grpc_service_impl);
  // Do necessary shutdown of the dependency scanner
  LOG(INFO) << "Destroying GomaIP service.";
  delete gomaip_service;
  return true;
}

GomaIPServiceImpl::GomaIPServiceImpl(std::function<void()> shutdown_server,
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
    LOG(WARNING) << "Goma dependency scanner is already initialized and will "
                    "not be reinitialized";
  } else {
    std::time_t start = std::time(0);
    deps_scanner_cache_ = include_processor::NewDepsScanner(
        process_name_, cache_dir_.c_str(), log_dir_.c_str(), cache_file_max_mb_,
        use_deps_cache_);
    std::time_t end = std::time(0);
    if (!deps_scanner_cache_) {
      LOG(FATAL) << "Unable to create new goma dependency scanner";
    }
    LOG(INFO) << "Initializing goma dependency scanner took " << end - start
              << " seconds";
  }
  init_cv_.notify_all();
}

GomaIPServiceImpl::~GomaIPServiceImpl() {
#ifdef _WIN32
  if (wsa_initialized_) {
    WSACleanup();
  }
#endif
  if (deps_scanner_cache_ != nullptr) {
    LOG(INFO) << "Shutting down input processor";
    deps_scanner_cache_->Quit();
  }
}

Status GomaIPServiceImpl::ProcessInputs(ServerContext* context,
                                        const CPPProcessInputsRequest* request,
                                        CPPProcessInputsResponse* response) {
  (void)context;

  // TODO b/268656738: refactor this to common service code
  // Count an action
  ++current_actions_;
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

  auto goma_result = std::make_shared<include_processor::GomaResult>();
  goma_result->directory = request->directory();
  goma_result->filename = request->filename();
  std::unique_lock<std::mutex> result_lock(goma_result->result_mutex);
  deps_scanner_cache_->ComputeIncludes(
      request->exec_id(), request->directory(),
      std::vector<std::string>(request->command().begin(),
                               request->command().end()),
      std::vector<std::string>(request->cmd_env().begin(),
                               request->cmd_env().end()),
      goma_result);
  goma_result->result_condition.wait(
      result_lock, [&goma_result]() { return goma_result->result_complete; });

  if (goma_result->error.size() > 0) {
    std::ostringstream command;
    std::copy(request->command().begin(), request->command().end(),
              std::ostream_iterator<std::string>(command, " "));
    LOG(ERROR)
        << "Goma encountered the following error processing a command: \""
        << goma_result->error << "\"; Command: [" << command.str() << "]";
  }
  response->set_error(goma_result->error);
  response->set_used_cache(goma_result->used_cache);
  for (auto dependency : goma_result->dependencies) {
    response->add_dependencies(dependency);
  }
  response->add_dependencies(request->filename());

  // TODO b/268656738: refactor this to common service code
  // Count the action as complete
  --current_actions_;
  ++completed_actions_;
  return grpc::Status::OK;
}

Status GomaIPServiceImpl::Status(ServerContext* context,
                                 const google::protobuf::Empty* request,
                                 StatusResponse* response) {
  (void)context;
  (void)request;

  VLOG(1) << "Status request received.";
  PopulateStatusResponse(response);

  return grpc::Status::OK;
}

Status GomaIPServiceImpl::Shutdown(ServerContext* context,
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

Status GomaIPServiceImpl::Capabilities(ServerContext* context,
                                       const google::protobuf::Empty* request,
                                       CapabilitiesResponse* response) {
  (void)context;
  (void)request;

  VLOG(1) << "Capabilities request received.";
  response->set_caching(true);
  response->set_expects_resource_dir(false);

  return grpc::Status::OK;
}

void GomaIPServiceImpl::PopulateStatusResponse(
    scandeps::StatusResponse* response) {
  response->set_name(INPUT_PROCESSOR);
  response->set_version(RECLIENT_VERSION);
  google::protobuf::Duration* uptime = new google::protobuf::Duration();
  uptime->set_seconds(std::time(0) - started_);
  response->set_allocated_uptime(uptime);  // gRPC library takes care of cleanup
  response->set_completed_actions(completed_actions_);
  response->set_running_actions(current_actions_);
}
