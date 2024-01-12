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
 * bridge between cgo (C) and llvm (C++).
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

extern void* NewDepsScanner();

extern void DeleteDepsScanner(void* impl);

extern int ScanDependencies(void* impl, int argc, const char** argv,
                            const char* filename, const char* dir, char** deps,
                            char** errs);

#ifdef __cplusplus
}
#endif
