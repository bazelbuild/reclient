// Copyright 2024 Google LLC
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

#include "adjust_cmd.h"

#include <algorithm>

// Checks if the given executable is clang-cl.
bool IsClangClCommand(std::string exec) {
  std::transform(exec.begin(), exec.end(), exec.begin(),
                 [](unsigned char c) { return std::tolower(c); });
  auto slashPos = exec.find_last_of("/\\");
  if (slashPos != std::string::npos) {
    exec = exec.substr(slashPos + 1);
  }
  return exec == "clang-cl" || exec == "clang-cl.exe";
}

// Adjusts the given command to be compatible with clangscandeps.
void clangscandeps::AdjustCmd(std::vector<std::string>& cmd,
                              std::string filename,
                              const std::set<std::string>& ignoredPlugins) {
  if (cmd.empty()) {
    return;
  }
  bool hasMT = false;
  bool hasMQ = false;
  bool hasMD = false;
  std::string lastO = "";
  for (auto i = cmd.size() - 1; i > 0; i--) {
    auto arg = cmd.at(i);
    if (arg == "-o") {
      lastO = cmd.at(i + 1);
    } else if (arg == "-MT") {
      hasMT = true;
    } else if (arg == "-MQ") {
      hasMQ = true;
    } else if (arg == "-MD") {
      hasMD = true;
    }
    // Check for -Xclang -add-plugin -Xclang [ignored-plugin]
    if (i + 3 < cmd.size() && arg == "-Xclang" && cmd[i + 1] == "-add-plugin" &&
        cmd[i + 2] == "-Xclang" &&
        ignoredPlugins.find(cmd[i + 3]) != ignoredPlugins.end()) {
      cmd.erase(cmd.begin() + i, cmd.begin() + i + 4);
    }
  }
  bool isClangCl = IsClangClCommand(cmd.front());
  if (isClangCl) {
    cmd.push_back("/FoNUL");
  } else {
    cmd.insert(cmd.end(), {"-o", "/dev/null"});
  }
  if (!isClangCl && !hasMT && !hasMQ) {
    cmd.insert(cmd.end(), {"-M", "-MT"});
    if (!hasMD) {
      // FIXME: We are missing the directory
      // unless the -o value is an absolute path.
      if (lastO.empty()) {
        auto objName = filename;
        auto dotPos = objName.find_last_of('.');
        if (dotPos != std::string::npos) {
          objName = objName.substr(0, dotPos);
        }
        cmd.push_back(objName + ".o");
      } else {
        cmd.push_back(lastO);
      }
    } else {
      cmd.push_back(filename);
    }
  }
  cmd.insert(cmd.end(), {"-Xclang", "-Eonly", "-Xclang", "-sys-header-deps",
                         "-Wno-error"});
}