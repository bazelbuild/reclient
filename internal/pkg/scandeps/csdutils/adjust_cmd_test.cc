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

#include <algorithm>
#include <string>
#include <vector>

using namespace csdutils;

TEST(AdjustCmdTest, ClangCommandMissing_MT_MQ_MD_O) {
  std::vector<std::string> cmd = {"/path/to/clang++", "-MF", "out.d"};
  AdjustCmd(cmd, "somefile.d", {});
  EXPECT_EQ(cmd, std::vector<std::string>(
                     {"/path/to/clang++", "-MF", "out.d", "-o", "/dev/null",
                      "-M", "-MT", "somefile.o", "-Xclang", "-Eonly", "-Xclang",
                      "-sys-header-deps", "-Wno-error", "-Wno-everything"}));
}

TEST(AdjustCmdTest, ClangCommandBackslashMissing_MT_MQ_MD_O) {
  std::vector<std::string> cmd = {"\\path\\to\\clang++", "-MF", "out.d"};
  AdjustCmd(cmd, "somefile.d", {});
  EXPECT_EQ(cmd, std::vector<std::string>(
                     {"\\path\\to\\clang++", "-MF", "out.d", "-o", "/dev/null",
                      "-M", "-MT", "somefile.o", "-Xclang", "-Eonly", "-Xclang",
                      "-sys-header-deps", "-Wno-error", "-Wno-everything"}));
}

TEST(AdjustCmdTest, ClangCommandCLBadPrefixMissing_MT_MQ_MD_O) {
  std::vector<std::string> cmd = {"/path/to/aaaclang-cl", "-MF", "out.d"};
  AdjustCmd(cmd, "somefile.d", {});
  EXPECT_EQ(cmd, std::vector<std::string>(
                     {"/path/to/aaaclang-cl", "-MF", "out.d", "-o", "/dev/null",
                      "-M", "-MT", "somefile.o", "-Xclang", "-Eonly", "-Xclang",
                      "-sys-header-deps", "-Wno-error", "-Wno-everything"}));
}

TEST(AdjustCmdTest, ClangCommandCLBadPrefixBackslashMissing_MT_MQ_MD_O) {
  std::vector<std::string> cmd = {"\\path\\to\\aaaclang-cl", "-MF", "out.d"};
  AdjustCmd(cmd, "somefile.d", {});
  EXPECT_EQ(cmd,
            std::vector<std::string>(
                {"\\path\\to\\aaaclang-cl", "-MF", "out.d", "-o", "/dev/null",
                 "-M", "-MT", "somefile.o", "-Xclang", "-Eonly", "-Xclang",
                 "-sys-header-deps", "-Wno-error", "-Wno-everything"}));
}

TEST(AdjustCmdTest, ClangCommandCLWinBadPrefixMissing_MT_MQ_MD_O) {
  std::vector<std::string> cmd = {"/path/to/aaaclang-cl.exe", "-MF", "out.d"};
  AdjustCmd(cmd, "somefile.d", {});
  EXPECT_EQ(cmd,
            std::vector<std::string>(
                {"/path/to/aaaclang-cl.exe", "-MF", "out.d", "-o", "/dev/null",
                 "-M", "-MT", "somefile.o", "-Xclang", "-Eonly", "-Xclang",
                 "-sys-header-deps", "-Wno-error", "-Wno-everything"}));
}

TEST(AdjustCmdTest, ClangCommandCLWinBadPrefixBackslashMissing_MT_MQ_MD_O) {
  std::vector<std::string> cmd = {"\\path\\to\\aaaclang-cl.exe", "-MF",
                                  "out.d"};
  AdjustCmd(cmd, "somefile.d", {});
  EXPECT_EQ(cmd, std::vector<std::string>(
                     {"\\path\\to\\aaaclang-cl.exe", "-MF", "out.d", "-o",
                      "/dev/null", "-M", "-MT", "somefile.o", "-Xclang",
                      "-Eonly", "-Xclang", "-sys-header-deps", "-Wno-error",
                      "-Wno-everything"}));
}

TEST(AdjustCmdTest, ClangCommandMissing_MT_MQ_MD) {
  std::vector<std::string> cmd = {"/path/to/clang++", "-o", "somefile"};
  AdjustCmd(cmd, "somefile.o", {});
  EXPECT_EQ(cmd, std::vector<std::string>(
                     {"/path/to/clang++", "-o", "somefile", "-o", "/dev/null",
                      "-M", "-MT", "somefile", "-Xclang", "-Eonly", "-Xclang",
                      "-sys-header-deps", "-Wno-error", "-Wno-everything"}));
}

TEST(AdjustCmdTest, ClangCommandMissing_MT_MQ) {
  std::vector<std::string> cmd = {"/path/to/clang++", "-MD"};
  AdjustCmd(cmd, "somefile", {});
  EXPECT_EQ(cmd, std::vector<std::string>(
                     {"/path/to/clang++", "-MD", "-o", "/dev/null", "-M", "-MT",
                      "somefile", "-Xclang", "-Eonly", "-Xclang",
                      "-sys-header-deps", "-Wno-error", "-Wno-everything"}));
}

TEST(AdjustCmdTest, ClangCommandMissing_MT) {
  std::vector<std::string> cmd = {"/path/to/clang++", "-MQ"};
  AdjustCmd(cmd, "somefile", {});
  EXPECT_EQ(cmd, std::vector<std::string>({"/path/to/clang++", "-MQ", "-o",
                                           "/dev/null", "-Xclang", "-Eonly",
                                           "-Xclang", "-sys-header-deps",
                                           "-Wno-error", "-Wno-everything"}));
}

TEST(AdjustCmdTest, ClangCommandMissing_MQ) {
  std::vector<std::string> cmd = {"/path/to/clang++", "-MT"};
  AdjustCmd(cmd, "somefile", {});
  EXPECT_EQ(cmd, std::vector<std::string>({"/path/to/clang++", "-MT", "-o",
                                           "/dev/null", "-Xclang", "-Eonly",
                                           "-Xclang", "-sys-header-deps",
                                           "-Wno-error", "-Wno-everything"}));
}

TEST(AdjustCmdTest, ClangClCommandMissing_MT_MQ_MD_O) {
  std::vector<std::string> cmd = {"/path/to/clang-cl", "-MF", "out.d"};
  AdjustCmd(cmd, "somefile.d", {});
  EXPECT_EQ(cmd, std::vector<std::string>({"/path/to/clang-cl", "-MF", "out.d",
                                           "/FoNUL", "-Xclang", "-Eonly",
                                           "-Xclang", "-sys-header-deps",
                                           "-Wno-error", "-Wno-everything"}));
}

TEST(AdjustCmdTest, ClangClCommandBackslashMissing_MT_MQ_MD_O) {
  std::vector<std::string> cmd = {"\\path\\to\\clang-cl", "-MF", "out.d"};
  AdjustCmd(cmd, "somefile.d", {});
  EXPECT_EQ(cmd, std::vector<std::string>(
                     {"\\path\\to\\clang-cl", "-MF", "out.d", "/FoNUL",
                      "-Xclang", "-Eonly", "-Xclang", "-sys-header-deps",
                      "-Wno-error", "-Wno-everything"}));
}

TEST(AdjustCmdTest, ClangClCommandMissing_MT_MQ_MD) {
  std::vector<std::string> cmd = {"/path/to/clang-cl", "-o", "somefile"};
  AdjustCmd(cmd, "somefile.o", {});
  EXPECT_EQ(cmd, std::vector<std::string>(
                     {"/path/to/clang-cl", "-o", "somefile", "/FoNUL",
                      "-Xclang", "-Eonly", "-Xclang", "-sys-header-deps",
                      "-Wno-error", "-Wno-everything"}));
}

TEST(AdjustCmdTest, ClangClCommandMissing_MT_MQ) {
  std::vector<std::string> cmd = {"/path/to/clang-cl", "-MD"};
  AdjustCmd(cmd, "somefile", {});
  EXPECT_EQ(cmd, std::vector<std::string>({"/path/to/clang-cl", "-MD", "/FoNUL",
                                           "-Xclang", "-Eonly", "-Xclang",
                                           "-sys-header-deps", "-Wno-error",
                                           "-Wno-everything"}));
}

TEST(AdjustCmdTest, ClangClCommandMissing_MT) {
  std::vector<std::string> cmd = {"/path/to/clang-cl", "-MQ"};
  AdjustCmd(cmd, "somefile", {});
  EXPECT_EQ(cmd, std::vector<std::string>({"/path/to/clang-cl", "-MQ", "/FoNUL",
                                           "-Xclang", "-Eonly", "-Xclang",
                                           "-sys-header-deps", "-Wno-error",
                                           "-Wno-everything"}));
}

TEST(AdjustCmdTest, ClangClCommandMissing_MQ) {
  std::vector<std::string> cmd = {"/path/to/clang-cl", "-MT"};
  AdjustCmd(cmd, "somefile", {});
  EXPECT_EQ(cmd, std::vector<std::string>({"/path/to/clang-cl", "-MT", "/FoNUL",
                                           "-Xclang", "-Eonly", "-Xclang",
                                           "-sys-header-deps", "-Wno-error",
                                           "-Wno-everything"}));
}

TEST(AdjustCmdTest, ClangClWinCommandMissing_MT_MQ_MD_O) {
  std::vector<std::string> cmd = {"/path/to/clang-cl.exe", "-MF", "out.d"};
  AdjustCmd(cmd, "somefile.d", {});
  EXPECT_EQ(cmd, std::vector<std::string>(
                     {"/path/to/clang-cl.exe", "-MF", "out.d", "/FoNUL",
                      "-Xclang", "-Eonly", "-Xclang", "-sys-header-deps",
                      "-Wno-error", "-Wno-everything"}));
}

TEST(AdjustCmdTest, ClangClWinCommandBackslashMissing_MT_MQ_MD_O) {
  std::vector<std::string> cmd = {"\\path\\to\\clang-cl.exe", "-MF", "out.d"};
  AdjustCmd(cmd, "somefile.d", {});
  EXPECT_EQ(cmd, std::vector<std::string>(
                     {"\\path\\to\\clang-cl.exe", "-MF", "out.d", "/FoNUL",
                      "-Xclang", "-Eonly", "-Xclang", "-sys-header-deps",
                      "-Wno-error", "-Wno-everything"}));
}

TEST(AdjustCmdTest, ClangClWinCommandMissing_MT_MQ_MD) {
  std::vector<std::string> cmd = {"/path/to/clang-cl.exe", "-o", "somefile"};
  AdjustCmd(cmd, "somefile.o", {});
  EXPECT_EQ(cmd, std::vector<std::string>(
                     {"/path/to/clang-cl.exe", "-o", "somefile", "/FoNUL",
                      "-Xclang", "-Eonly", "-Xclang", "-sys-header-deps",
                      "-Wno-error", "-Wno-everything"}));
}

TEST(AdjustCmdTest, ClangClWinCommandMissing_MT_MQ) {
  std::vector<std::string> cmd = {"/path/to/clang-cl.exe", "-MD"};
  AdjustCmd(cmd, "somefile", {});
  EXPECT_EQ(cmd, std::vector<std::string>({"/path/to/clang-cl.exe", "-MD",
                                           "/FoNUL", "-Xclang", "-Eonly",
                                           "-Xclang", "-sys-header-deps",
                                           "-Wno-error", "-Wno-everything"}));
}

TEST(AdjustCmdTest, ClangClWinCommandMissing_MT) {
  std::vector<std::string> cmd = {"/path/to/clang-cl.exe", "-MQ"};
  AdjustCmd(cmd, "somefile", {});
  EXPECT_EQ(cmd, std::vector<std::string>({"/path/to/clang-cl.exe", "-MQ",
                                           "/FoNUL", "-Xclang", "-Eonly",
                                           "-Xclang", "-sys-header-deps",
                                           "-Wno-error", "-Wno-everything"}));
}

TEST(AdjustCmdTest, ClangClWinCommandMissing_MQ) {
  std::vector<std::string> cmd = {"/path/to/clang-cl.exe", "-MT"};
  AdjustCmd(cmd, "somefile", {});
  EXPECT_EQ(cmd, std::vector<std::string>({"/path/to/clang-cl.exe", "-MT",
                                           "/FoNUL", "-Xclang", "-Eonly",
                                           "-Xclang", "-sys-header-deps",
                                           "-Wno-error", "-Wno-everything"}));
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
  AdjustCmd(cmd, "out.o", {"foo", "baz"});
  EXPECT_EQ(cmd, std::vector<std::string>({"/path/to/clang++",
                                           "-MF",
                                           "out.d",
                                           "-Xclang",
                                           "-add-plugin",
                                           "-Xclang",
                                           "bar",
                                           "-o",
                                           "out.o",
                                           "-o",
                                           "/dev/null",
                                           "-M",
                                           "-MT",
                                           "out.o",
                                           "-Xclang",
                                           "-Eonly",
                                           "-Xclang",
                                           "-sys-header-deps",
                                           "-Wno-error",
                                           "-Wno-everything"}));
}
// -pedantic-errors makes clang treat extension diagnostics as hard errors,
// which no -Wno- flag undoes. Since the scanner is built from a different LLVM
// revision than the compiler running the action, it knows extensions the
// compiler does not, and would fail to scan files the compiler accepts.
TEST(AdjustCmdTest, StripsPedanticErrors) {
  std::vector<std::string> cmd = {
      "/path/to/clang++", "-pedantic-errors", "-c", "a.cc", "-o", "a.o"};
  AdjustCmd(cmd, "a.cc", {});
  EXPECT_EQ(std::find(cmd.begin(), cmd.end(), "-pedantic-errors"), cmd.end());
  // -pedantic on its own only produces warnings, which -Wno-everything covers,
  // so it is left alone.
  std::vector<std::string> pedantic = {
      "/path/to/clang++", "-pedantic", "-c", "a.cc", "-o", "a.o"};
  AdjustCmd(pedantic, "a.cc", {});
  EXPECT_NE(std::find(pedantic.begin(), pedantic.end(), "-pedantic"),
            pedantic.end());
}
