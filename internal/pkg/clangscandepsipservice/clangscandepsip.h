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

#ifndef INTERNAL_PKG_CLANGSCANDEPSIPSERVICE_CLANGSCANDEPSIP_H_
#define INTERNAL_PKG_CLANGSCANDEPSIPSERVICE_CLANGSCANDEPSIP_H_

#include <grpcpp/grpcpp.h>

#include <atomic>
#include <condition_variable>
#include <ctime>

#include "api/scandeps/cppscandeps.grpc.pb.h"
#include "include_processor.h"

class ClangscandepsIPServiceImpl final
    : public scandeps::CPPDepsScanner::Service {
 public:
  ClangscandepsIPServiceImpl(const char* process_name,
                             std::function<void()> shutdown_server);
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
  struct ClangScanDepsResult;

  std::atomic<std::size_t> completed_actions_;
  std::atomic<std::size_t> current_actions_;
  std::condition_variable init_cv_;
  std::function<void()> shutdown_server_;
  std::mutex init_mutex_;
  std::time_t started_;
  std::unique_ptr<include_processor::IncludeProcessor> deps_scanner_cache_;

  void PopulateStatusResponse(scandeps::StatusResponse*);
};

#endif