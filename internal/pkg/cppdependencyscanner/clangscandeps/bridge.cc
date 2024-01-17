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

#include <memory>
#include <set>
#include <sstream>
#include <string>
#include <vector>

#include "adjust_cmd.h"
#include "clang/Tooling/CommonOptionsParser.h"
#include "clang/Tooling/CompilationDatabase.h"
#include "clang/Tooling/DependencyScanning/DependencyScanningService.h"
#include "clang/Tooling/DependencyScanning/DependencyScanningTool.h"
#include "clang/Tooling/DependencyScanning/DependencyScanningWorker.h"

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

class DependencyScanner {
 public:
  DependencyScanner()
      : Service(clang::tooling::dependencies::DependencyScanningService(
            clang::tooling::dependencies::ScanningMode::
                DependencyDirectivesScan,
            clang::tooling::dependencies::ScanningOutputFormat::Make, true,
            true)),
        PluginsToIgnore(ParsePluginsToIgnore()) {}

  int getDependencies(int argc, const char** argv, const char* filename,
                      const char* directory, char** deps, char** errp) {
    std::vector<std::string> CommandLine;
    for (int i = 0; i < argc; i++) {
      CommandLine.push_back(std::string(argv[i]));
    }
    std::string Filename(filename);
    std::string Directory(directory);

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
      *errp = strdup("unexpected number of cmds from AdjustingCompilation");
      return 1;
    }

    std::unique_ptr<clang::tooling::dependencies::DependencyScanningTool>
        WorkerTool = std::make_unique<
            clang::tooling::dependencies::DependencyScanningTool>(Service);

    auto DependencyScanningRes =
        WorkerTool->getDependencyFile(cmds[0].CommandLine, Directory);
    if (!DependencyScanningRes) {
      std::string ErrorMessage = "";
      llvm::handleAllErrors(DependencyScanningRes.takeError(),
                            [&ErrorMessage](llvm::StringError& Err) {
                              ErrorMessage += Err.getMessage();
                            });
      *errp = strdup(ErrorMessage.c_str());
      return 1;
    }
    *deps = strdup(DependencyScanningRes.get().c_str());
    return 0;
  }

 private:
  clang::tooling::dependencies::DependencyScanningService Service;
  std::set<std::string> PluginsToIgnore;
};

void* NewDepsScanner() { return new (DependencyScanner); }

void DeleteDepsScanner(void* impl) {
  delete reinterpret_cast<DependencyScanner*>(impl);
}

int ScanDependencies(void* impl, int argc, const char** argv,
                     const char* filename, const char* dir, char** deps,
                     char** errs) {
  DependencyScanner* scanner = reinterpret_cast<DependencyScanner*>(impl);
  return scanner->getDependencies(argc, argv, filename, dir, deps, errs);
}
