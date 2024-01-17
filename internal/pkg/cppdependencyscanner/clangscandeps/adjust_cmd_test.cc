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

#include <gtest/gtest.h>

#include <string>
#include <vector>

TEST(AdjustCmdTest, ClangCommandMissing_MT_MQ_MD_O) {
  std::vector<std::string> cmd = {"/path/to/clang++", "-MF", "out.d"};
  clangscandeps::AdjustCmd(cmd, "somefile.d", {});
  EXPECT_EQ(cmd, std::vector<std::string>(
                     {"/path/to/clang++", "-MF", "out.d", "-o", "/dev/null",
                      "-M", "-MT", "somefile.o", "-Xclang", "-Eonly", "-Xclang",
                      "-sys-header-deps", "-Wno-error"}));
}

TEST(AdjustCmdTest, ClangCommandMissing_MT_MQ_MD) {
  std::vector<std::string> cmd = {"/path/to/clang++", "-o", "somefile"};
  clangscandeps::AdjustCmd(cmd, "somefile.o", {});
  EXPECT_EQ(cmd, std::vector<std::string>(
                     {"/path/to/clang++", "-o", "somefile", "-o", "/dev/null",
                      "-M", "-MT", "somefile", "-Xclang", "-Eonly", "-Xclang",
                      "-sys-header-deps", "-Wno-error"}));
}

TEST(AdjustCmdTest, ClangCommandMissing_MT_MQ) {
  std::vector<std::string> cmd = {"/path/to/clang++", "-MD"};
  clangscandeps::AdjustCmd(cmd, "somefile", {});
  EXPECT_EQ(cmd, std::vector<std::string>({"/path/to/clang++", "-MD", "-o",
                                           "/dev/null", "-M", "-MT", "somefile",
                                           "-Xclang", "-Eonly", "-Xclang",
                                           "-sys-header-deps", "-Wno-error"}));
}

TEST(AdjustCmdTest, ClangCommandMissing_MT) {
  std::vector<std::string> cmd = {"/path/to/clang++", "-MQ"};
  clangscandeps::AdjustCmd(cmd, "somefile", {});
  EXPECT_EQ(cmd, std::vector<std::string>(
                     {"/path/to/clang++", "-MQ", "-o", "/dev/null", "-Xclang",
                      "-Eonly", "-Xclang", "-sys-header-deps", "-Wno-error"}));
}

TEST(AdjustCmdTest, ClangCommandMissing_MQ) {
  std::vector<std::string> cmd = {"/path/to/clang++", "-MT"};
  clangscandeps::AdjustCmd(cmd, "somefile", {});
  EXPECT_EQ(cmd, std::vector<std::string>(
                     {"/path/to/clang++", "-MT", "-o", "/dev/null", "-Xclang",
                      "-Eonly", "-Xclang", "-sys-header-deps", "-Wno-error"}));
}

TEST(AdjustCmdTest, ClangClCommandMissing_MT_MQ_MD_O) {
  std::vector<std::string> cmd = {"/path/to/clang-cl", "-MF", "out.d"};
  clangscandeps::AdjustCmd(cmd, "somefile.d", {});
  EXPECT_EQ(cmd, std::vector<std::string>(
                     {"/path/to/clang-cl", "-MF", "out.d", "/FoNUL", "-Xclang",
                      "-Eonly", "-Xclang", "-sys-header-deps", "-Wno-error"}));
}

TEST(AdjustCmdTest, ClangClCommandMissing_MT_MQ_MD) {
  std::vector<std::string> cmd = {"/path/to/clang-cl", "-o", "somefile"};
  clangscandeps::AdjustCmd(cmd, "somefile.o", {});
  EXPECT_EQ(cmd,
            std::vector<std::string>({"/path/to/clang-cl", "-o", "somefile",
                                      "/FoNUL", "-Xclang", "-Eonly", "-Xclang",
                                      "-sys-header-deps", "-Wno-error"}));
}

TEST(AdjustCmdTest, ClangClCommandMissing_MT_MQ) {
  std::vector<std::string> cmd = {"/path/to/clang-cl", "-MD"};
  clangscandeps::AdjustCmd(cmd, "somefile", {});
  EXPECT_EQ(cmd, std::vector<std::string>({"/path/to/clang-cl", "-MD", "/FoNUL",
                                           "-Xclang", "-Eonly", "-Xclang",
                                           "-sys-header-deps", "-Wno-error"}));
}

TEST(AdjustCmdTest, ClangClCommandMissing_MT) {
  std::vector<std::string> cmd = {"/path/to/clang-cl", "-MQ"};
  clangscandeps::AdjustCmd(cmd, "somefile", {});
  EXPECT_EQ(cmd, std::vector<std::string>({"/path/to/clang-cl", "-MQ", "/FoNUL",
                                           "-Xclang", "-Eonly", "-Xclang",
                                           "-sys-header-deps", "-Wno-error"}));
}

TEST(AdjustCmdTest, ClangClCommandMissing_MQ) {
  std::vector<std::string> cmd = {"/path/to/clang-cl", "-MT"};
  clangscandeps::AdjustCmd(cmd, "somefile", {});
  EXPECT_EQ(cmd, std::vector<std::string>({"/path/to/clang-cl", "-MT", "/FoNUL",
                                           "-Xclang", "-Eonly", "-Xclang",
                                           "-sys-header-deps", "-Wno-error"}));
}

TEST(AdjustCmdTest, ClangClWinCommandMissing_MT_MQ_MD_O) {
  std::vector<std::string> cmd = {"/path/to/clang-cl.exe", "-MF", "out.d"};
  clangscandeps::AdjustCmd(cmd, "somefile.d", {});
  EXPECT_EQ(cmd,
            std::vector<std::string>({"/path/to/clang-cl.exe", "-MF", "out.d",
                                      "/FoNUL", "-Xclang", "-Eonly", "-Xclang",
                                      "-sys-header-deps", "-Wno-error"}));
}

TEST(AdjustCmdTest, ClangClWinCommandMissing_MT_MQ_MD) {
  std::vector<std::string> cmd = {"/path/to/clang-cl.exe", "-o", "somefile"};
  clangscandeps::AdjustCmd(cmd, "somefile.o", {});
  EXPECT_EQ(cmd,
            std::vector<std::string>({"/path/to/clang-cl.exe", "-o", "somefile",
                                      "/FoNUL", "-Xclang", "-Eonly", "-Xclang",
                                      "-sys-header-deps", "-Wno-error"}));
}

TEST(AdjustCmdTest, ClangClWinCommandMissing_MT_MQ) {
  std::vector<std::string> cmd = {"/path/to/clang-cl.exe", "-MD"};
  clangscandeps::AdjustCmd(cmd, "somefile", {});
  EXPECT_EQ(cmd, std::vector<std::string>(
                     {"/path/to/clang-cl.exe", "-MD", "/FoNUL", "-Xclang",
                      "-Eonly", "-Xclang", "-sys-header-deps", "-Wno-error"}));
}

TEST(AdjustCmdTest, ClangClWinCommandMissing_MT) {
  std::vector<std::string> cmd = {"/path/to/clang-cl.exe", "-MQ"};
  clangscandeps::AdjustCmd(cmd, "somefile", {});
  EXPECT_EQ(cmd, std::vector<std::string>(
                     {"/path/to/clang-cl.exe", "-MQ", "/FoNUL", "-Xclang",
                      "-Eonly", "-Xclang", "-sys-header-deps", "-Wno-error"}));
}

TEST(AdjustCmdTest, ClangClWinCommandMissing_MQ) {
  std::vector<std::string> cmd = {"/path/to/clang-cl.exe", "-MT"};
  clangscandeps::AdjustCmd(cmd, "somefile", {});
  EXPECT_EQ(cmd, std::vector<std::string>(
                     {"/path/to/clang-cl.exe", "-MT", "/FoNUL", "-Xclang",
                      "-Eonly", "-Xclang", "-sys-header-deps", "-Wno-error"}));
}

TEST(AdjustCmdTest, ClangCommandIgnorePlugin) {
  std::vector<std::string> cmd = {"/path/to/clang++",
                                  "-MF",
                                  "out.d",
                                  "-Xclang",
                                  "-add-plugin",
                                  "-Xclang",
                                  "foo",
                                  "-Xclang",
                                  "-add-plugin",
                                  "-Xclang",
                                  "bar",
                                  "-o",
                                  "out.o",
                                  "-Xclang",
                                  "-add-plugin",
                                  "-Xclang",
                                  "baz"};
  clangscandeps::AdjustCmd(cmd, "out.o", {"foo", "baz"});
  EXPECT_EQ(cmd, std::vector<std::string>(
                     {"/path/to/clang++", "-MF", "out.d", "-Xclang",
                      "-add-plugin", "-Xclang", "bar", "-o", "out.o", "-o",
                      "/dev/null", "-M", "-MT", "out.o", "-Xclang", "-Eonly",
                      "-Xclang", "-sys-header-deps", "-Wno-error"}));
}