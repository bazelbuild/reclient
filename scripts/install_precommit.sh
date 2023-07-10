#!/bin/sh
# Copyright 2023 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Script to install the precommit hook to your local git folder.
if [ ! -f .git/hooks/pre-commit ]; then
  echo "#!/bin/bash" > .git/hooks/pre-commit
fi
if ! grep -q scripts/precommit.sh .git/hooks/pre-commit; then
  echo scripts/precommit.sh >> .git/hooks/pre-commit
fi
chmod +x .git/hooks/pre-commit

# pre-commit hooks requires golint
go install golang.org/x/lint/golint@latest
