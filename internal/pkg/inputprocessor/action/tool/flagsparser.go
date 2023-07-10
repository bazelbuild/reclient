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

package tool

import (
	"context"
	"fmt"

	"team/foundry-x/re-client/internal/pkg/inputprocessor/flags"
)

// parseFlags is used to parse the flags in a tool invocation command.
func parseFlags(ctx context.Context, command []string, workingDir, execRoot string) (*flags.CommandFlags, error) {
	numArgs := len(command)
	if numArgs < 1 {
		return nil, fmt.Errorf("insufficient number of arguments in command: %v", command)
	}

	res := &flags.CommandFlags{
		WorkingDirectory: workingDir,
		ExecRoot:         execRoot,
		ExecutablePath:   command[0],
	}
	for _, arg := range command[1:] {
		res.Flags = append(res.Flags, &flags.Flag{Value: arg})
	}
	return res, nil
}
