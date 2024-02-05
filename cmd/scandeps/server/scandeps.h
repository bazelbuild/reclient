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

// Everything in this header must be implemented by the target dependency
// scanner. Exactly one dependency scanner must be included in the deps of the
// build file.
#ifndef CMD_SCANDEPS_SERVER_SCANDEPS_H_
#define CMD_SCANDEPS_SERVER_SCANDEPS_H_

#include "api/scandeps/cppscandeps.grpc.pb.h"

// newDepsScanner is responsible for creating the implemented dependency
// scanner.
// TODO (b/268656738): remove experimental_deadlock and experimental_segfault
scandeps::CPPDepsScanner::Service* newDepsScanner(
    std::function<void()> shutdown_server, const char* process_name,
    const char* cache_dir, const char* log_dir, int deps_cache_max_mb,
    bool enable_deps_cache, uint32_t experimental_deadlock,
    uint32_t experimental_segfault);

// deleteDepsScanner is responsible for destroying the implemented dependency
// scanner.
bool deleteDepsScanner(scandeps::CPPDepsScanner::Service* grpc_service_impl);

#endif  // CMD_SCANDEPS_SERVER_SCANDEPS_H_
