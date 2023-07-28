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

#include "clangscandepsip.h"

#ifdef _WIN32
# define GLOG_NO_ABBREVIATED_SEVERITIES
#endif
#include <glog/logging.h>

#include "internal/pkg/cppdependencyscanner/clangscandeps/bridge.h"

using grpc::ServerContext;
using grpc::Status;
using grpc::StatusCode;

using scandeps::CPPProcessInputsRequest;
using scandeps::CPPProcessInputsResponse;
using scandeps::StatusResponse;

// TODO(b/268656738): Refactor common code between this class and GomaIPServiceImpl.
struct ClangscandepsIPServiceImpl::ClangScanDepsResult {
  bool result_complete = false;
  std::condition_variable result_condition;
  std::mutex result_mutex;
};

// Implementation of newDepsScanner from scandeps.h
scandeps::CPPDepsScanner::Service* newDepsScanner(
    std::function<void()> shutdown_server,
    const char *process_name,
    const char *cache_dir, const char *log_dir,
    int deps_cache_max_mb, bool enable_deps_cache,
    uint32_t experimental_deadlock,
    uint32_t experimental_segfault) {
  return new ClangscandepsIPServiceImpl(process_name, shutdown_server);
}

// Implementation of deleteDepsScanner from scandeps.h
bool deleteDepsScanner(
    scandeps::CPPDepsScanner::Service* grpc_service_impl) {
  ClangscandepsIPServiceImpl* clangscandepsip_service =
      static_cast<ClangscandepsIPServiceImpl*>(grpc_service_impl);
  // Do necessary shutdown of the dependency scanner
  LOG(INFO) << "Destroying ClangscandepsIP service.";
  delete clangscandepsip_service;
  return true;
}

ClangscandepsIPServiceImpl::ClangscandepsIPServiceImpl(const char* process_name,
                                                       std::function<void()> shutdown_server)
    : completed_actions_(0),
      current_actions_(0),
      shutdown_server_(shutdown_server),
      deps_scanner_cache_(nullptr) {
  started_ = std::time(0);
  google::InitGoogleLogging(process_name);
  InitClangscandeps();
}

ClangscandepsIPServiceImpl::~ClangscandepsIPServiceImpl(){
  DeleteDepsScanner(deps_scanner_cache_);
}

void ClangscandepsIPServiceImpl::InitClangscandeps() {
  std::unique_lock<std::mutex> exp_lock(init_mutex_);
  if (deps_scanner_cache_ != nullptr) {
    LOG(WARNING) << "Clangscandeps dependency scanner is already initialized and will not be reinitialized";
  } else {
    std::time_t start = std::time(0);
    deps_scanner_cache_ = NewDepsScanner();
    std::time_t end = std::time(0);
    if (deps_scanner_cache_ == nullptr) {
      LOG(FATAL) << "Unable to create new clangscandeps dependency scanner";
    }
    LOG(INFO) << "Initializing clangscandeps dependency scanner took " << end - start << " seconds";
  }
  init_cv_.notify_all();
}

Status ClangscandepsIPServiceImpl::ProcessInputs(ServerContext* context,
                                                 const CPPProcessInputsRequest* request,
                                                 CPPProcessInputsResponse* response){
  (void)context;

  ++current_actions_;
  if (deps_scanner_cache_ == nullptr) {
    // Not fully initialized.
    // Block on the lock if it's in the process of initialization.
    std::unique_lock<std::mutex> init_lock(init_mutex_);
    if (deps_scanner_cache_ == nullptr) {
      // If we're here, ProcessInputs somehow beat the init thread.
      // Release the lock and wait for the init thread to signal that it's done.
      init_cv_.wait(init_lock, [&]{return deps_scanner_cache_ != nullptr;});
    }
  }

  std::vector<const char*> argv(request->command_size());
  for (int i = 0; i < request->command_size(); ++i) {
    argv[i] = request->command(i).c_str();
  }

  ClangScanDepsResult clangscandeps_result;
  char* depsStr = nullptr;
  char* errStr = nullptr;
  std::unique_lock<std::mutex> result_lock(clangscandeps_result.result_mutex);
  ScanDependenciesResult(clangscandeps_result, deps_scanner_cache_, request->command_size(), argv.data(), request->filename().c_str(), request->directory().c_str(), &depsStr, &errStr);
  clangscandeps_result.result_condition.wait(result_lock, [&clangscandeps_result]() { return clangscandeps_result.result_complete; });

  if (errStr != nullptr) {
    std::string err(errStr);
    response->set_error(err);

    std::ostringstream command;
    std::copy(request->command().begin(), request->command().end(),
            std::ostream_iterator<std::string>(command, " "));
    LOG(ERROR) << "Clangscandeps encountered the following error processing a command: \"" << errStr << "\"; Command: [" << command.str() << "]";
    free(errStr);
  }

  if (depsStr != nullptr) {
    std::string deps(depsStr);
    std::vector<std::string> dependencies = Parse(deps);

    // Set the dependencies in the response message
    for (auto dependency : dependencies) {
      response->add_dependencies(dependency);
    }
    free(depsStr);
  }
  response->add_dependencies(request->filename());

  // TODO b/268656738: refactor this to common service code
  // Count the action as complete
  --current_actions_;
  ++completed_actions_;
  return grpc::Status::OK;
}

void ClangscandepsIPServiceImpl::ScanDependenciesResult(
    ClangScanDepsResult& clangscandeps_result,
    void *impl,
    int argc,
    const char** argv,
    const char* filename,
    const char* dir,
    char** deps,
    char** errs){
  ScanDependencies(impl, argc, argv, filename, dir, deps, errs);
  clangscandeps_result.result_complete = true;
  clangscandeps_result.result_condition.notify_all();
}

// deps format
// <output>: <input> ...
// <input> is space sparated
// '\'+newline is space
// '\'+space is an escaped space (not separater)
std::vector<std::string> ClangscandepsIPServiceImpl::Parse(std::string depsStr){
  std::vector<std::string> dependencies;

  // Skip until ':'
  size_t start = depsStr.find_first_of(":");
  if (start < 0) {
    return dependencies;
  }
  std::string deps = depsStr.substr(start + 1);

  std::string dependency;
  for (int i = 0; i < deps.length(); i++) {
    char c = deps[i];

    // Skip spaces and append dependency.
    if (c == ' ' || c == '\t' || c == '\n') {
      if (dependency.length() > 0) {
        dependencies.push_back(dependency);
      }
      dependency.clear();
      continue;
    }

    // \\ followed by \n is a space. Skip this character.
    if (c == '\\' && i + 1 < deps.length() && deps[i+1] == '\n') {
      continue;
    }

    // \\ followed by a ' ' is not an escape character. Only append ' '.
    if (c == '\\' && i + 1 < deps.length() && deps[i+1] == ' ') {
      dependency += ' ';
      i++;
    } else {
      dependency += c;
    }
  }

  if (dependency.length() > 0) {
    dependencies.push_back(dependency);
  }

  return dependencies;
}

Status ClangscandepsIPServiceImpl::Status(ServerContext* context,
                                          const google::protobuf::Empty* request,
                                          StatusResponse* response){
  (void)context;
  (void)request;

  VLOG(1) << "Status request received.";
  PopulateStatusResponse(response);

  return grpc::Status::OK;
}

Status ClangscandepsIPServiceImpl::Shutdown(ServerContext* context,
                                     const google::protobuf::Empty* request,
                                     StatusResponse* response){
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

void ClangscandepsIPServiceImpl::PopulateStatusResponse(scandeps::StatusResponse* response) {
  response->set_name("ClangscandepsIP");
  response->set_version("1.0.0-beta");
  google::protobuf::Duration* uptime = new google::protobuf::Duration();
  uptime->set_seconds(std::time(0) - started_);
  response->set_allocated_uptime(uptime);  // gRPC library takes care of cleanup
  response->set_completed_actions(completed_actions_);
  response->set_running_actions(current_actions_);
}