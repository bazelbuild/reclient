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

#include "bridge.h"

#include <errno.h>
#include <fcntl.h>
#include <signal.h>

#include <iostream>
#include <map>
#include <memory>
#include <set>
#include <string>
#include <thread>
#include <type_traits>
#include <vector>

#ifdef _WIN32
#define GLOG_NO_ABBREVIATED_SEVERITIES
#endif

#include "breakpad.h"
#include "callback.h"
#include "clang_modules/modulemap/cache.h"
#include "compiler_flags_parser.h"
#include "compiler_info_cache.h"
#include "compiler_info_state.h"
#include "compiler_type_specific_collection.h"
#include "cxx/include_processor/cpp_include_processor.h"
#include "cxx/include_processor/include_cache.h"
#include "deps_cache.h"
#include "file_stat_cache.h"
#include "get_compiler_info_param.h"
#include "goma_init.h"
#include "ioutil.h"
#include "list_dir_cache.h"
#include "mypath.h"
#include "path.h"
#include "pkg/version/version.h"
#include "platform_thread.h"
#include "scoped_fd.h"
#include "util.h"
#include "worker_thread.h"
#include "worker_thread_manager.h"

// Some of reclient's use cases require ubuntu 16.04, which is only shipped
// with GLIBC 2.23 at the latest, by default (or so is the ubuntu:16.04 docker
// image). GLIBC isn't forwards compatible, so we have to explicitly link
// against the older version.
//
// See explanation example in https://thecharlatan.ch/GLIBC-Back-Compat/
#ifdef __linux__
#include <glob.h>

#ifndef __GLIBC_NEW__

extern "C" {
int __glob_compatible(const char* pattern, int flags,
                      int (*errfunc)(const char* epath, int errno),
                      glob_t* pglob);
}

asm(".symver __glob_compatible, glob@GLIBC_2.2.5");

extern "C" {
// Have the __wrap_glob func call to the symver'd version of glob via
// __glob_compatible.
int __wrap_glob(const char* pattern, int flags,
                int (*errfunc)(const char* epath, int errno), glob_t* pglob) {
  return __glob_compatible(pattern, flags, errfunc, pglob);
}
}
#else
extern "C" {
int __real_glob(const char* pattern, int flags,
                int (*errfunc)(const char* epath, int errno), glob_t* pglob);
// Have the __wrap_glob func call to the original version of glob via
// __real_glob.
int __wrap_glob(const char* pattern, int flags,
                int (*errfunc)(const char* epath, int errno), glob_t* pglob) {
  return __real_glob(pattern, flags, errfunc, pglob);
}
}
#endif  // __GLIBC_NEW__

#endif  // __linux__

#include "subprocess.h"
#include "subprocess_controller_client.h"
#include "subprocess_task.h"
// Exported by goma.go when reproxy is built, declare this when building the
// depsscanner service.
void gComputeIncludesDone(uintptr_t req_ptr, std::set<std::string>& res,
                          bool used_cache, std::string& err);

#define REPROXY_CACHE_FILE "reproxy-gomaip.cache"

using namespace std;
using namespace devtools_goma;

namespace include_processor {

struct IncludeProcessorRequestParam {
  // input file_stat_cache will be moved to temporarily.
  std::unique_ptr<FileStatCache> file_stat_cache;
};

struct IncludeProcessorResponseParam {
  // result of IncludeProcessor.
  CompilerTypeSpecific::IncludeProcessorResult result;
  // return borrowed file_stat_cache to CompileTask.
  std::unique_ptr<FileStatCache> file_stat_cache;
  // true if include processor was canceled.
  bool canceled = false;
};

class Request;

// IncludeProcessor receives include processor requests and manages the worker
// pools necessary to execute such requests. It provides common functionality
// across requests like CompilerInfo querying. The code mostly comes from
// https://chromium.googlesource.com/infra/goma/client/+/refs/heads/main/client/compile_service.cc
class IncludeProcessor {
 public:
  unique_ptr<WorkerThreadManager> wm_;
  int include_processor_pool_;
  int ComputeIncludes(const string& exec_id, const string& cwd,
                      const vector<string>& args, const vector<string>& envs,
                      uintptr_t req);
  void ComputeIncludesDone(Request* request);

  void Quit();
  IncludeProcessor(string tmpdir)
      : compiler_type_specific_collection_(
            make_unique<CompilerTypeSpecificCollection>()),
        wm_(make_unique<WorkerThreadManager>()) {
    wm_.get()->Start(FLAGS_COMPILER_PROXY_THREADS);
    compiler_info_pool_ =
        wm_->StartPool(FLAGS_COMPILER_INFO_POOL, "compiler_info");
    request_pool_ =
        wm_->StartPool(FLAGS_COMPILER_PROXY_THREADS, "request_pool");
    include_processor_pool_ =
        wm_->StartPool(FLAGS_INCLUDE_PROCESSOR_THREADS, "include_processor");

    SubProcessControllerClient::Initialize(wm_.get(), tmpdir);

    load_deps_cache_ = make_unique<WorkerThreadRunner>(
        wm_.get(), FROM_HERE, NewCallback(DepsCache::LoadIfEnabled));
    load_compiler_info_cache_ = make_unique<WorkerThreadRunner>(
        wm_.get(), FROM_HERE, NewCallback(CompilerInfoCache::LoadIfEnabled));
  }
  void GetCompilerInfo(const string exec_id, GetCompilerInfoParam* param,
                       OneshotClosure* callback);

  CompilerTypeSpecificCollection* compiler_type_specific_collection() {
    return compiler_type_specific_collection_.get();
  }

 private:
  void GetCompilerInfoInternal(GetCompilerInfoParam* param,
                               OneshotClosure* callback);
  typedef std::pair<GetCompilerInfoParam*, OneshotClosure*> CompilerInfoWaiter;
  typedef std::vector<CompilerInfoWaiter> CompilerInfoWaiterList;
  unique_ptr<CompilerTypeSpecificCollection> compiler_type_specific_collection_;
  unique_ptr<WorkerThreadRunner> load_compiler_info_cache_;
  unique_ptr<WorkerThreadRunner> load_deps_cache_;
  mutable Lock gmu_;
  int compiler_info_pool_;
  int request_pool_;
  mutable Lock compiler_info_mu_;
  absl::flat_hash_map<std::string, CompilerInfoWaiterList*>
      compiler_info_waiters_ ABSL_GUARDED_BY(compiler_info_mu_);
  unordered_map<string, shared_ptr<Lock>> compiler_info_locks_
      ABSL_GUARDED_BY(gmu_);
};

// Request holds information about a single command we receive for input
// processing. It is responsible for determining the CompilerInfoState and
// running the include scanner. Most of the code comes from
// https://chromium.googlesource.com/infra/goma/client/+/refs/heads/main/client/compile_task.cc
class Request {
 public:
  unique_ptr<CompilerFlags> flags_;
  string exec_id_;
  const string cwd_;
  vector<string> args_;
  vector<string> env_;
  PlatformThreadId thread_id_;
  DepsCache::Identifier deps_identifier_;
  bool depscache_used_ = false;
  set<string> required_files_;
  string err_;
  // go_req_ is a pointer to a Go object representing the input processing
  // request. The pointer is passed to the Go handler gComputeIncludesDone, and
  // then gComputeIncludesDone sends the results on a channel in the request
  // object.
  uintptr_t go_req_;

  IncludeProcessor* processor_;
  CompilerTypeSpecific* compiler_type_specific_;
  ScopedCompilerInfoState compiler_info_state_;
  std::unique_ptr<FileStatCache> input_file_stat_cache_;
  Request(const string exec_id, const string cwd, const vector<string>& args,
          const vector<string>& env)
      : exec_id_(exec_id), cwd_(cwd), args_(args), env_(env) {}
  void StartInputProcessing();
  void SaveToDepsCache();

 private:
  void InputProcessingDone();
  void FillCompilerInfo();
  void FillCompilerInfoDone(std::unique_ptr<GetCompilerInfoParam> param);
  void StartIncludeProcessor();
  void RunIncludeProcessor(
      std::unique_ptr<IncludeProcessorRequestParam> request_param);
  void RunIncludeProcessorDone(
      std::unique_ptr<IncludeProcessorResponseParam> response_param);
};

void GetAdditionalEnv(const vector<string>& in_envs, const char* name,
                      vector<string>* envs) {
  int namelen = strlen(name);
  for (const auto& e : in_envs) {
#ifdef _WIN32
    // environment variables are case insensitive on Windows
    // that means that we should pass on either PATH or Path
    if (strnicmp(e.c_str(), name, namelen) == 0 && e[namelen] == '=') {
#else
    if (strncmp(e.c_str(), name, namelen) == 0 && e[namelen] == '=') {
#endif
      envs->push_back(e);
      return;
    }
  }
}

void IncludeProcessor::Quit() {
  FlushLogFiles();
  SubProcessControllerClient::Get()->Quit();
  load_deps_cache_.reset();
  load_compiler_info_cache_.reset();
  devtools_goma::CompilerInfoCache::instance()->Save();
  CompilerInfoCache::Quit();
  DepsCache::Quit();
  IncludeCache::Quit();
  modulemap::Cache::Quit();
  ListDirCache::Quit();
  SubProcessControllerClient::Get()->Shutdown();
  wm_->Finish();
  if (FLAGS_ENABLE_GLOBAL_FILE_STAT_CACHE) {
    GlobalFileStatCache::Quit();
  }
}

void IncludeProcessor::GetCompilerInfo(const string exec_id,
                                       GetCompilerInfoParam* param,
                                       OneshotClosure* callback) {
  VLOG(1) << exec_id << ": Started GetCompilerInfo";
  param->state.reset(CompilerInfoCache::instance()->Lookup(param->key));
  if (param->state.get() != nullptr) {
    param->cache_hit = true;
    callback->Run();
    VLOG(1) << exec_id << ": Finished GetCompilerInfo with cache hit";
    return;
  }
  {
    AUTOLOCK(lock, &compiler_info_mu_);
    auto p = compiler_info_waiters_.insert(std::make_pair(
        param->key.ToString(CompilerInfoCache::Key::kCwdRelative),
        static_cast<CompilerInfoWaiterList*>(nullptr)));
    if (p.second) {
      // first call for the key.
      p.first->second = new CompilerInfoWaiterList;
    } else {
      // another task already requested the same key.
      // callback will be called once the other task gets compiler info.
      p.first->second->emplace_back(param, callback);
      return;
    }
  }
  VLOG(1) << exec_id << ": Finished GetCompilerInfo without cache hit";
  wm_->RunClosureInPool(
      FROM_HERE, compiler_info_pool_,
      NewCallback(this, &IncludeProcessor::GetCompilerInfoInternal, param,
                  callback),
      WorkerThread::PRIORITY_MED);
}

void IncludeProcessor::GetCompilerInfoInternal(GetCompilerInfoParam* param,
                                               OneshotClosure* callback) {
  std::vector<std::string> env(param->run_envs);
  env.push_back("GOMA_WILL_FAIL_WITH_UKNOWN_FLAG=true");
  param->state.reset(CompilerInfoCache::instance()->Lookup(param->key));
  if (param->state.get() == nullptr) {
    SimpleTimer timer;
    CompilerTypeSpecific* compiler_type_specific_ =
        compiler_type_specific_collection()->Get(param->flags->type());
    std::unique_ptr<CompilerInfoData> cid(
        compiler_type_specific_->BuildCompilerInfoData(
            *param->flags, param->key.local_compiler_path, env));
    param->state.reset(
        CompilerInfoCache::instance()->Store(param->key, std::move(cid)));
    param->updated = true;
  }
  std::unique_ptr<CompilerInfoWaiterList> waiters;
  {
    AUTOLOCK(lock, &compiler_info_mu_);
    const std::string key_cwd =
        param->key.ToString(CompilerInfoCache::Key::kCwdRelative);
    auto p = compiler_info_waiters_.find(key_cwd);
    CHECK(p != compiler_info_waiters_.end())
        << param->trace_id << " state=" << param->state.get()
        << " key_cwd=" << key_cwd;
    waiters.reset(p->second);
    compiler_info_waiters_.erase(p);
  }
  // keep alive at least in this func.
  // param->state might be derefed so CompilerInfoState may be deleted.
  ScopedCompilerInfoState state(param->state.get());

  wm_->RunClosureInThread(FROM_HERE, param->thread_id, callback,
                          WorkerThread::PRIORITY_MED);
  // param may be invalidated here.
  CHECK(waiters.get() != nullptr) << " state=" << state.get();
  for (const auto& p : *waiters) {
    GetCompilerInfoParam* wparam = p.first;
    OneshotClosure* wcallback = p.second;
    wparam->state.reset(state.get());
    wm_->RunClosureInThread(FROM_HERE, wparam->thread_id, wcallback,
                            WorkerThread::PRIORITY_MED);
  }
}

void Request::FillCompilerInfo() {
  std::vector<std::string> run_envs;
  VLOG(1) << exec_id_ << ": Started FillCompilerInfo";
  // Append scandeps service version string as env variable in CompilerInfo, so
  // that when we update scandeps version, scandeps cache will be invalidated.
  run_envs.push_back(INPUT_PROCESSOR "=" RECLIENT_VERSION);
  // Used by nacl on Mac.
  GetAdditionalEnv(env_, "PATH", &run_envs);
#ifdef _WIN32
  // used by nacl on Win
  GetAdditionalEnv(env_, "SystemRoot", &run_envs);

  // used by vpython
  GetAdditionalEnv(env_, "HOMEDRIVE", &run_envs);
  GetAdditionalEnv(env_, "HOMEPATH", &run_envs);
  GetAdditionalEnv(env_, "USERPROFILE", &run_envs);
  // used by (clang-)cl.exe
  GetAdditionalEnv(env_, "INCLUDE", &run_envs);
  GetAdditionalEnv(env_, "LIB", &run_envs);
#endif

  auto param = std::make_unique<GetCompilerInfoParam>();
  param->thread_id = processor_->wm_->GetCurrentThreadId();
  param->key =
      CompilerInfoCache::CreateKey(*flags_, flags_->args()[0], run_envs);
  VLOG(1) << exec_id_
          << ": local_compiler_path=" << param->key.local_compiler_path;
  param->flags = flags_.get();
  param->run_envs = run_envs;

  GetCompilerInfoParam* param_pointer = param.get();
  processor_->GetCompilerInfo(
      exec_id_, param_pointer,
      NewCallback(this, &Request::FillCompilerInfoDone, std::move(param)));
}

void Request::FillCompilerInfoDone(
    std::unique_ptr<GetCompilerInfoParam> param) {
  VLOG(1) << exec_id_ << ": Started FillCompilerInfoDone";
  if (param->state.get() == nullptr) {
    err_ = "something went wrong trying to get compiler info.";
    processor_->ComputeIncludesDone(this);
    return;
  }
  compiler_info_state_ = std::move(param->state);
  if (compiler_info_state_.get()->info().HasError()) {
    // In this case, it found local compiler, but failed to get necessary
    // information, such as system include paths.
    // It would happen when multiple -arch options are used.
    err_ = compiler_info_state_.get()->info().error_message();
    processor_->ComputeIncludesDone(this);
    return;
  }
  StartIncludeProcessor();
}

void Request::StartIncludeProcessor() {
  auto b = compiler_type_specific_->SupportsDepsCache(*flags_);
  VLOG(1) << exec_id_ << ": Started StartIncludeProcessor";
  if (DepsCache::IsEnabled() &&
      compiler_type_specific_->SupportsDepsCache(*flags_) &&
      flags_->input_filenames().size() == 1U) {
    const std::string& input_filename = flags_->input_filenames()[0];
    const std::string& abs_input_filename =
        file::JoinPathRespectAbsolute(flags_->cwd(), input_filename);

    DepsCache* dc = DepsCache::instance();
    deps_identifier_ = DepsCache::MakeDepsIdentifier(
        compiler_info_state_.get()->info(), *flags_);
    if (deps_identifier_.has_value() &&
        dc->GetDependencies(deps_identifier_, flags_->cwd(), abs_input_filename,
                            &required_files_, input_file_stat_cache_.get())) {
      VLOG(1) << exec_id_ << ": Used deps cache, found "
              << required_files_.size() << " dependencies";
      depscache_used_ = true;
      processor_->ComputeIncludesDone(this);
      return;
    }
  }

  auto request_param = absl::make_unique<IncludeProcessorRequestParam>();

  input_file_stat_cache_->ReleaseOwner();
  request_param->file_stat_cache = std::move(input_file_stat_cache_);

  OneshotClosure* closure = NewCallback(this, &Request::RunIncludeProcessor,
                                        std::move(request_param));
  processor_->wm_->RunClosureInPool(FROM_HERE,
                                    processor_->include_processor_pool_,
                                    closure, WorkerThread::PRIORITY_LOW);
}

void Request::RunIncludeProcessor(
    std::unique_ptr<IncludeProcessorRequestParam> request_param) {
  VLOG(1) << exec_id_ << ": Started RunIncludeProcessor";

  // Pass ownership temporary to IncludeProcessor thread.
  request_param->file_stat_cache->AcquireOwner();
  auto command_spec = CommandSpec();
  command_spec.set_name(flags_->compiler_name());
  VLOG(1) << exec_id_ << ": Got owner of file stat cache";
  VLOG(1) << exec_id_ << ": checks " << (compiler_info_state_.get() != nullptr)
          << (request_param->file_stat_cache.get() != nullptr);

  CompilerTypeSpecific::IncludeProcessorResult result =
      compiler_type_specific_->RunIncludeProcessor(
          exec_id_, *flags_, compiler_info_state_.get()->info(), command_spec,
          request_param->file_stat_cache.get());
  VLOG(1) << exec_id_ << ": Got include processor result";

  auto response_param = absl::make_unique<IncludeProcessorResponseParam>();
  response_param->result = std::move(result);
  response_param->file_stat_cache = std::move(request_param->file_stat_cache);
  response_param->file_stat_cache->ReleaseOwner();

  VLOG(1) << exec_id_ << ": Finished RunIncludeProcessor";
  processor_->wm_->RunClosureInThread(
      FROM_HERE, thread_id_,
      NewCallback(this, &Request::RunIncludeProcessorDone,
                  std::move(response_param)),
      WorkerThread::PRIORITY_LOW);
}

void Request::RunIncludeProcessorDone(
    std::unique_ptr<IncludeProcessorResponseParam> response_param) {
  VLOG(1) << exec_id_ << ": Started RunIncludeProcessorDone";
  input_file_stat_cache_ = std::move(response_param->file_stat_cache);
  input_file_stat_cache_->AcquireOwner();
  required_files_ = std::move(response_param->result.required_files);
  if (!response_param->result.ok) {
    err_ = strdup(response_param->result.error_reason.c_str());
  }
  processor_->ComputeIncludesDone(this);
}

void Request::SaveToDepsCache() {
  VLOG(1) << exec_id_ << ": Started SaveToDepsCache";
  // When deps_identifier_.has_value() is true, the condition to use DepsCache
  // should be satisfied. However, several checks are done for the safe.
  if (DepsCache::IsEnabled() && !depscache_used_ &&
      compiler_type_specific_->SupportsDepsCache(*flags_) && err_ == "" &&
      deps_identifier_.has_value() && flags_->input_filenames().size() == 1U) {
    const std::string& input_filename = flags_->input_filenames()[0];
    const std::string& abs_input_filename =
        file::JoinPathRespectAbsolute(flags_->cwd(), input_filename);
    DepsCache* dc = DepsCache::instance();
    if (!dc->SetDependencies(deps_identifier_, flags_->cwd(),
                             abs_input_filename, required_files_,
                             input_file_stat_cache_.get())) {
      LOG(WARNING) << "Failed to save dependencies for action with input file "
                   << abs_input_filename;
    } else {
      VLOG(1) << exec_id_ << ": Saved to deps cache";
    }
  }
  VLOG(1) << exec_id_ << ": Finished SaveToDepsCache";
}

void Request::StartInputProcessing() {
  VLOG(1) << exec_id_ << ": Starting input processing";
  input_file_stat_cache_ = absl::make_unique<FileStatCache>();
  flags_ = CompilerFlagsParser::MustNew(args_, cwd_);
  compiler_type_specific_ =
      processor_->compiler_type_specific_collection()->Get(flags_->type());
  vector<string> compiler_info_envs;
  thread_id_ = processor_->wm_->GetCurrentThreadId();
  err_ = "";

  VLOG(1) << exec_id_ << ": Input processing on thread " << thread_id_;
  FillCompilerInfo();
}

void IncludeProcessor::ComputeIncludesDone(Request* request) {
  VLOG(1) << request->exec_id_ << ": Started ComputeIncludesDone";
  gComputeIncludesDone(request->go_req_, request->required_files_,
                       request->depscache_used_, request->err_);
  request->SaveToDepsCache();
  delete request;
}

int IncludeProcessor::ComputeIncludes(const string& exec_id, const string& cwd,
                                      const vector<string>& args,
                                      const vector<string>& envs,
                                      uintptr_t req) {
  // TODO(b/246553914): add logic to prevent a memory leak here.
  auto request = new Request(exec_id, cwd, args, envs);
  request->processor_ = this;
  request->go_req_ = req;
  wm_->RunClosureInPool(FROM_HERE, request_pool_,
                        NewCallback(request, &Request::StartInputProcessing),
                        WorkerThread::PRIORITY_MED);
  return 0;
}

}  // namespace include_processor

// converts a vector of c++ std strings to an array of c strings
unique_ptr<char*[]> toCStringsArray(const vector<string>& strings) {
  auto cstrings = make_unique<char*[]>(strings.size());
  for (int i = 0; i < strings.size(); i++) {
    cstrings[i] = const_cast<char*>(strings[i].c_str());
  }
  return cstrings;
}

void* NewDepsScanner(const char* process_name, const char* cache_dir,
                     const char* log_dir, int cache_file_max_mb,
                     bool use_deps_cache) {
  auto argc = 1;
  char* argv[1];
  argv[0] = strdup(process_name);
  const char* envp[1];
  envp[0] = NULL;
#ifdef _WIN32
  PlatformThread::SetName(GetCurrentThread(), "main");
  // On Windows SubProcessController::Initialize does not use a fork to create a
  // new process and does not initialize google logging, so it's safe to call
  // InitLogging before SubProcessController::Initialize
  InitLogging(argv[0]);
#endif  // _WIN32

  FLAGS_log_dir = log_dir;
  Init(argc, argv, envp);

  devtools_goma::SubProcessController::Options subproc_options;
  subproc_options.max_subprocs = FLAGS_MAX_SUBPROCS;
  subproc_options.max_subprocs_low_priority = FLAGS_MAX_SUBPROCS_LOW;
  subproc_options.max_subprocs_heavy_weight = FLAGS_MAX_SUBPROCS_HEAVY;
  subproc_options.dont_kill_subprocess = FLAGS_DONT_KILL_SUBPROCESS;
  devtools_goma::SubProcessController::Initialize(process_name,
                                                  subproc_options);

#ifndef _WIN32
  // On *nix-es, SubProcessController::Initialize uses fork to create a new
  // process and initializes google loging there. Hence, we should initialize
  // logging only after :SubProcessController::Initialize is called to avoid
  // getting FATAL due to duplicated google logging initialization.
  InitLogging(argv[0]);
#endif

  if (FLAGS_ENABLE_GLOBAL_FILE_STAT_CACHE) {
    GlobalFileStatCache::Init();
    LOG(INFO) << "GlobalFileStatCache initialized";
  }
  string tmpdir = FLAGS_TMP_DIR;
#ifndef _WIN32
  // Initialize rand.
  srand(static_cast<unsigned int>(time(nullptr)));

  // Do not die with a SIGHUP and SIGPIPE.
  signal(SIGHUP, SIG_IGN);
  signal(SIGPIPE, SIG_IGN);
#else
  // change directory to tmpdir, so that running process will keep
  // the directory and it makes it possible to remove the directory.
  // TODO(b/192980840): check whether changing directory affects
  // reproxy on Windows.
  LOG(INFO) << "chdir to " << tmpdir;
  if (!Chdir(tmpdir.c_str())) {
    LOG(ERROR) << "failed to chdir to " << tmpdir;
  }
#ifdef NDEBUG
  // Sets error mode to SEM_FAILCRITICALERRORS and SEM_NOGPFAULTERRORBOX
  // to prevent from popping up message box on error.
  // We don't use CREATE_DEFAULT_ERROR_MODE for dwCreationFlags in
  // CreateProcess function.
  // http://msdn.microsoft.com/en-us/library/windows/desktop/ms680621(v=vs.85).aspx
  UINT old_error_mode =
      SetErrorMode(SEM_FAILCRITICALERRORS | SEM_NOGPFAULTERRORBOX);
  LOG(INFO) << "Set error mode from " << old_error_mode << " to "
            << GetErrorMode();
#endif  // NDEBUG
#endif  // _WIN32
  if (FLAGS_COMPILER_PROXY_ENABLE_CRASH_DUMP) {
    InitCrashReporter(log_dir);
    LOG(INFO) << "breakpad is enabled in " << log_dir;
  }

  InstallReadCommandOutputFunc(SubProcessTask::ReadCommandOutput);

  IncludeFileFinder::Init(FLAGS_ENABLE_GCH_HACK);
  std::string cache_filename;
  cache_filename = file::JoinPathRespectAbsolute(cache_dir, REPROXY_CACHE_FILE);
  if (cache_filename == REPROXY_CACHE_FILE) {
    // Set the cache_dir to tmpdir for use in the compiler info cache.
    cache_dir = tmpdir.c_str();
  }
  IncludeCache::Init(FLAGS_MAX_INCLUDE_CACHE_ENTRIES, use_deps_cache);
  if (use_deps_cache) {
    DepsCache::Init(cache_filename,
                    FLAGS_DEPS_CACHE_IDENTIFIER_ALIVE_DURATION >= 0
                        ? absl::optional<absl::Duration>(absl::Seconds(
                              FLAGS_DEPS_CACHE_IDENTIFIER_ALIVE_DURATION))
                        : absl::nullopt,
                    FLAGS_DEPS_CACHE_TABLE_THRESHOLD, cache_file_max_mb);
  }
  modulemap::Cache::Init(FLAGS_MAX_MODULEMAP_CACHE_ENTRIES);
  ListDirCache::Init(FLAGS_MAX_LIST_DIR_CACHE_ENTRY_NUM);
  CompilerInfoCache::Init(
      cache_dir, FLAGS_COMPILER_INFO_CACHE_FILE,
      FLAGS_COMPILER_INFO_CACHE_NUM_ENTRIES,
      absl::Seconds(FLAGS_COMPILER_INFO_CACHE_HOLDING_TIME_SEC));
  return new include_processor::IncludeProcessor(tmpdir);
}

int ScanDependencies(void* impl, const char* exec_id, int argc,
                     const char** argv, const char** envp, const char* filename,
                     const char* dir, uintptr_t req) {
  include_processor::IncludeProcessor* scanner =
      reinterpret_cast<include_processor::IncludeProcessor*>(impl);
  vector<string> arguments(argv, argv + argc);
  set<string> includes;
  vector<string> env;
  for (int i = 0; envp[i]; i++) {
    env.push_back(envp[i]);
  }
  int result = scanner->ComputeIncludes(exec_id, dir, arguments, env, req);
  return result;
}

void Close(void* impl) {
  include_processor::IncludeProcessor* scanner =
      reinterpret_cast<include_processor::IncludeProcessor*>(impl);
  LOG(INFO) << "Shutting down input processor";
  scanner->Quit();
}
