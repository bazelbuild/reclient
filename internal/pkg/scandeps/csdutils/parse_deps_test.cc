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

#include "parse_deps.h"

#include <gtest/gtest.h>

#include <set>
#include <string>

TEST(ParseDepsTest, BasicTest) {
  EXPECT_EQ(csdutils::ParseDeps(
                "example.o: example.c /usr/include/stdio.h /path/with\\ "
                "space/file.h \\\n /some/file/on/another/line"),
            std::set<std::string>({"example.c", "/usr/include/stdio.h",
                                   "/path/with space/file.h",
                                   "/some/file/on/another/line"}));
}
