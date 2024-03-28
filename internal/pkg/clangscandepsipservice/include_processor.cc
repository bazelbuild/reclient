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

#include "include_processor.h"

#include <glog/logging.h>

#include <memory>
#include <sstream>
#include <string>
#include <vector>

#include "adjust_cmd.h"
#include "clang/Tooling/CommonOptionsParser.h"
#include "clang/Tooling/CompilationDatabase.h"
#include "clang/Tooling/DependencyScanning/DependencyScanningTool.h"
#include "internal/pkg/version/version.h"

// Some of reclient's use cases require ubuntu 16.04, which is only shipped
// with GLIBC 2.23 at the latest, by default (or so is the ubuntu:16.04 docker
// image). GLIBC isn't forwards compatible, so we have to explicitly link
// against the older version.
//
// See explanation example in https://thecharlatan.ch/GLIBC-Back-Compat/
#ifdef __linux__
extern "C" {
double __exp2_compatible(double);
double __pow_compatible(double, double);
float __log2f_compatible(float);
}

#ifndef __GLIBC_NEW__
asm(".symver __exp2_compatible, exp2@GLIBC_2.2.5");
asm(".symver __pow_compatible, pow@GLIBC_2.2.5");
asm(".symver __log2f_compatible, log2f@GLIBC_2.2.5");
#else
asm(".symver __exp2_compatible, exp2@GLIBC_2.29");
asm(".symver __pow_compatible, pow@GLIBC_2.29");
asm(".symver __log2f_compatible, log2f@GLIBC_2.27");
#endif  // __GLIBC_NEW__

extern "C" {
double __wrap_exp2(double exp) { return __exp2_compatible(exp); }

double __wrap_pow(double x, double y) { return __pow_compatible(x, y); }

float __wrap_log2f(float a) { return __log2f_compatible(a); }
}
#endif  // __linux__

class SingleCommandCompilationDatabase
    : public clang::tooling::CompilationDatabase {
 public:
  SingleCommandCompilationDatabase(clang::tooling::CompileCommand Cmd)
      : Command(std::move(Cmd)) {}

  std::vector<clang::tooling::CompileCommand> getCompileCommands(
      llvm::StringRef FilePath) const override {
    return {Command};
  }

  std::vector<clang::tooling::CompileCommand> getAllCompileCommands()
      const override {
    return {Command};
  }

 private:
  clang::tooling::CompileCommand Command;
};

// deps format
// <output>: <input> ...
// <input> is space sparated
// '\'+newline is space
// '\'+space is an escaped space (not separater)
std::set<std::string> ParseDeps(std::string depsStr) {
  std::set<std::string> dependencies;

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
        dependencies.insert(dependency);
      }
      dependency.clear();
      continue;
    }

    // \\ followed by \n is a space. Skip this character.
    if (c == '\\' && i + 1 < deps.length() && deps[i + 1] == '\n') {
      continue;
    }

    // \\ followed by a ' ' is not an escape character. Only append ' '.
    if (c == '\\' && i + 1 < deps.length() && deps[i + 1] == ' ') {
      dependency += ' ';
      i++;
    } else {
      dependency += c;
    }
  }

  if (dependency.length() > 0) {
    dependencies.insert(dependency);
  }

  return dependencies;
}

std::set<std::string> ParsePluginsToIgnore() {
  const char* envVal = std::getenv("RBE_clang_depscan_ignored_plugins");
  if (envVal == NULL || *envVal == '\0') {
    return {};
  }
  std::stringstream ss(envVal);
  std::set<std::string> result;
  while (ss.good()) {
    std::string substr;
    getline(ss, substr, ',');
    result.insert(substr);
  }
  return result;
};

class DependencyScanner final : public include_processor::IncludeProcessor {
 public:
  DependencyScanner()
      : Service(clang::tooling::dependencies::DependencyScanningService(
            clang::tooling::dependencies::ScanningMode::
                DependencyDirectivesScan,
            clang::tooling::dependencies::ScanningOutputFormat::Make, true,
            true)),
        PluginsToIgnore(ParsePluginsToIgnore()) {}

  void ComputeIncludes(const std::string& exec_id, const std::string& cwd,
                       const std::vector<std::string>& args,
                       const std::vector<std::string>& envs,
                       std::shared_ptr<include_processor::Result> req) {
    auto deps = computeIncludes(req->filename, req->directory, args);
    if (deps) {
      req->dependencies = deps.get();
    } else {
      std::string err;
      llvm::handleAllErrors(deps.takeError(), [&err](llvm::StringError& Err) {
        err += Err.getMessage();
      });
      req->error = err;
    }
    req->result_complete = true;
    req->result_condition.notify_all();
  }

 private:
  llvm::Expected<std::set<std::string>> computeIncludes(
      std::string Filename, std::string Directory,
      std::vector<std::string> CommandLine) {
    clangscandeps::AdjustCmd(CommandLine, Filename, PluginsToIgnore);

    clang::tooling::CompileCommand command(Directory, Filename, CommandLine,
                                           llvm::StringRef());
    std::unique_ptr<SingleCommandCompilationDatabase> Compilations =
        std::make_unique<SingleCommandCompilationDatabase>(std::move(command));
    // The command options are rewritten to run Clang in preprocessor only
    // mode.
    auto AdjustingCompilations =
        std::make_unique<clang::tooling::ArgumentsAdjustingCompilations>(
            std::move(Compilations));

    auto cmds = AdjustingCompilations->getAllCompileCommands();
    if (cmds.size() < 1) {
      return llvm::createStringError(
          std::errc::argument_out_of_domain,
          "unexpected number of cmds from AdjustingCompilation");
    }

    std::unique_ptr<clang::tooling::dependencies::DependencyScanningTool>
        WorkerTool = std::make_unique<
            clang::tooling::dependencies::DependencyScanningTool>(Service);

    auto DependencyScanningRes =
        WorkerTool->getDependencyFile(cmds[0].CommandLine, Directory);
    if (!DependencyScanningRes) {
      return DependencyScanningRes.takeError();
    }
    return ParseDeps(DependencyScanningRes.get());
  }

 private:
  clang::tooling::dependencies::DependencyScanningService Service;
  std::set<std::string> PluginsToIgnore;
};

namespace include_processor {
std::unique_ptr<include_processor::IncludeProcessor> NewDepsScanner() {
  return std::make_unique<DependencyScanner>();
}
}  // namespace include_processor
