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

/*
 * bridge between cgo (C) and goma (C++).
 *
 * when included in cgo, it is compiled as C,
 * which doesn't understand `extern "C"`.
 *
 * when included in bridge.cc, it is compiled as C++,
 * and use `extern "C"` to use C linkage.
 */

#ifdef __cplusplus
extern "C" {
#endif
// TODO: b/247818598
// These must be removed when building the dependency scanner service on Windows
#include <stdbool.h>
#include <stdint.h>

extern void* NewDepsScanner(const char* process_name,
                            const char* cache_dir,
                            const char* log_dir,
                            int cache_file_max_mb,
                            bool use_deps_cache);

extern void Close(void *impl);

extern int ScanDependencies(void *impl,
                     const char* exec_id,
                     int argc,
                     const char** argv,
                     const char** envp,
                     const char* filename,
                     const char* dir,
                     uintptr_t req);

#ifdef __cplusplus
}
#endif
