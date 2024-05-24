
#include "parse_deps.h"

#include <set>

// deps format
// <output>: <input> ...
// <input> is space sparated
// '\'+newline is space
// '\'+space is an escaped space (not separater)
std::set<std::string> csdutils::ParseDeps(std::string_view depsStr) {
  std::set<std::string> dependencies;

  // Skip until ':'
  size_t start = depsStr.find_first_of(":");
  if (start < 0) {
    return dependencies;
  }
  auto deps = depsStr.substr(start + 1);

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