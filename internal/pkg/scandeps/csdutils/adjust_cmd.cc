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

char norm_char(char a) { return a == '\\' ? '/' : std::tolower(a); }

bool norm_char_equals(char a, char b) { return norm_char(a) == norm_char(b); }

bool BaseEquals(const std::string_view exec, const std::string_view base) {
  return (
      exec.size() >= base.size() &&
      std::equal(base.rbegin(), base.rend(), exec.rbegin(), norm_char_equals) &&
      (exec.size() == base.size() ||
       norm_char(exec[exec.size() - base.size() - 1]) == '/'));
}

// Checks if the given executable is clang-cl.
bool IsClangClCommand(const std::string_view exec) {
  return BaseEquals(exec, "clang-cl") || BaseEquals(exec, "clang-cl.exe");
}

// Adjusts the given command to be compatible with clangscandeps.
void csdutils::AdjustCmd(std::vector<std::string>& cmd,
                         const std::string filename,
                         const std::set<std::string>& ignoredPlugins) {
  if (cmd.empty()) {
    return;
  }
  bool hasMT = false;
  bool hasMQ = false;
  bool hasMD = false;
  std::string lastO = "";
  for (int i = cmd.size() - 1; i > 0; i--) {
    auto arg = cmd[i];
    if (arg == "-o") {
      lastO = cmd[i + 1];
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
  bool isClangCl = IsClangClCommand(cmd[0]);
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
  // A dependency scan needs the set of headers a translation unit opens, and
  // nothing else, so no diagnostic short of a hard error should decide whether
  // an action is scannable.
  //
  // This matters because the bundled clang-scan-deps is not the same build as
  // the compiler that runs the action. A newer scanner knows diagnostics the
  // older compiler does not: scanning with clang 23 reports "'__COUNTER__' is
  // a C2y extension" for a file clang 22 compiles without complaint. Under
  // -pedantic-errors that is fatal, and the action fails to scan for a reason
  // that has nothing to do with its dependencies.
  //
  // -pedantic-errors has to be removed rather than countered: it sets clang's
  // extension-diagnostic behaviour to "error" outright, which neither
  // -Wno-error nor -Wno-everything undoes. Dropping it cannot change the
  // header set, only which diagnostics fire.
  cmd.erase(
      std::remove(cmd.begin(), cmd.end(), std::string("-pedantic-errors")),
      cmd.end());
  cmd.insert(cmd.end(), {"-Xclang", "-Eonly", "-Xclang", "-sys-header-deps",
                         "-Wno-error", "-Wno-everything"});
}