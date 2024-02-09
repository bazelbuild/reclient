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

#include <ctime>
#include <iostream>
#include <memory>
#include <string>
#include <thread>

#ifdef _WIN32
#define GLOG_NO_ABBREVIATED_SEVERITIES
#endif
#include <glog/logging.h>

#include "gflags/gflags.h"
#include "pkg/version/version.h"
#include "server/server.h"

#ifdef __linux__
#ifndef __GLIBC_NEW__
extern "C" {
/*
 * getentropy is technically compiled into goma but is never actually called by
 * anything the scandeps service does.
 */
int __wrap_getentropy(uint8_t* buffer, size_t length) {
  LOG(FATAL) << "getentropy called; this is unimplemented.  If this error is "
                "observed then it will need to be fixed.";
  // FATAL log will terminate the service.
}
}

#else
int __wrap_getentropy(uint8_t* buffer, size_t length) {
  return __real_getentropy(buffer, length);
}
#endif  // __GLIBC_NEW__
#endif  // __linux__

// Threads have 5 seconds to finish before being hard terminated.
constexpr std::size_t kShutdown_delay = 5;

#ifdef _WIN32  // autodefined when targeting Windows (32 and 64bit)
// gRPC on Windows does not currently support named pipes for IPC communication
DEFINE_string(server_address, "127.0.0.1:8001",
              "The address/port to listen on as TCP/IP address:"
              "port.");
#else
DEFINE_string(server_address, "127.0.0.1:8001",
              "The address to listen on; prepend with 'unix://' "
              "for a named pipe, otherwise IP address:port is assumed.");
#endif

// log_dir is automatic from glog.
DEFINE_string(cache_dir, "",
              "The directory to which the deps cache file should be written to "
              "and from which it should be loaded from.");
DEFINE_int32(
    deps_cache_max_mb, 256,
    "Maximum size of the deps cache file (for goma input processor only).");
DEFINE_bool(enable_deps_cache, false,
            "Enables the deps cache if --cache_dir is provided");
DEFINE_uint32(shutdown_delay_seconds, kShutdown_delay,
              "Delay, in seconds, server will wait for requests to finish "
              "during shutdown before terminating them.");

DEFINE_uint32(experimental_deadlock, 0,
              "Indicates the number of inputs to process before simulating a "
              "deadlock; 0 disables "
              "(default); for debugging purposes only");
DEFINE_uint32(experimental_segfault, 0,
              "Indicates the number of inputs to process before segfaulting; 0 "
              "disables (default); "
              "for debugging purposes only");

int main(int argc, char** argv) {
  gflags::SetVersionString(RECLIENT_VERSION " " INPUT_PROCESSOR);
  gflags::ParseCommandLineFlags(&argc, &argv, true);

  std::unique_ptr<ScandepsServer> server = std::make_unique<ScandepsServer>(
      FLAGS_server_address, FLAGS_cache_dir, FLAGS_log_dir,
      FLAGS_deps_cache_max_mb, FLAGS_enable_deps_cache,
      FLAGS_shutdown_delay_seconds, FLAGS_experimental_deadlock,
      FLAGS_experimental_segfault);

  auto success =
      server.get()->RunServer(argv[0]);  // Block until server is stopped
  LOG(INFO) << "Server has been stopped.";
  if (success) {
    return EXIT_SUCCESS;
  }
  return EXIT_FAILURE;
}