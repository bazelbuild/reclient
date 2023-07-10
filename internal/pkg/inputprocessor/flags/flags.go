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

// Package flags provides structs for holding information about an action's command flags.
package flags

// Flag is the in-memory representation of a specific flag associated with the command-line
// invocation of an action.
type Flag struct {
	Key   string
	Value string
	// If true, key and value should be joined together to form a command line argument
	// (e.g -I/include/path or --sysroot=/path)
	Joined bool

	// originalKey is an unnormalized key
	originalKey string
}

// OriginalKey returns value original (unnormalized) key
// that was passed to New function when instantianting the Flag
// if it's not set, it returns Key
func (f *Flag) OriginalKey() string {
	if f.originalKey != "" {
		return f.originalKey
	}
	return f.Key
}

// New returns a new instance of Flag
func New(key, originalKey, value string, joined bool) *Flag {
	return &Flag{Key: key, originalKey: originalKey, Value: value, Joined: joined}
}

// CommandFlags is the in-memory representation of all the flags associated with the
// commandline invocation of an action. It also stores certain key information
// associated with the build command, like the executable being invoked.
type CommandFlags struct {
	// ExecutablePath is the path to the executable used to run the build action.
	ExecutablePath string

	// TargetFilePaths is a list of filepaths on which the build action is supposed
	// to work. The paths are as seen in the command.
	// For example, for a C++ compile action, this would be the list of C++ files
	// being compiled.
	TargetFilePaths []string

	// Dependencies is a list of dependencies that are inferred from the command.
	Dependencies []string

	// EmittedDependencyFile is a file emitted by the action that lists all
	// dependencies required to execute the action.
	EmittedDependencyFile string

	// OutputFilePaths is a list of filepaths produced by the command as outputs.
	OutputFilePaths []string

	// OutputDirPaths is a list of directory paths indicated by the command as output
	// directory paths.
	OutputDirPaths []string

	// IncludeDirPaths is a list of directory paths where include files will be looked
	// for by the C++ compiler. The field is only relevant for C++ actions.
	IncludeDirPaths []string

	// VirtualDirectories is a list of directories that should exist for the correct
	// execution of the action.
	VirtualDirectories []string

	// WorkingDirectory is the directory inside which the build action must be run.
	WorkingDirectory string

	// ExecRoot is the execution root of the build action.
	ExecRoot string

	// Flags is the list of in-memory flag objects associated with the build action.
	Flags []*Flag
}

// Copy creates a deep copy of CommandFlags.
func (c *CommandFlags) Copy() *CommandFlags {
	return &CommandFlags{
		ExecutablePath:        c.ExecutablePath,
		WorkingDirectory:      c.WorkingDirectory,
		ExecRoot:              c.ExecRoot,
		IncludeDirPaths:       c.IncludeDirPaths,
		TargetFilePaths:       append([]string(nil), c.TargetFilePaths...),
		OutputFilePaths:       append([]string(nil), c.OutputFilePaths...),
		OutputDirPaths:        append([]string(nil), c.OutputDirPaths...),
		EmittedDependencyFile: c.EmittedDependencyFile,
		Flags:                 append([]*Flag(nil), c.Flags...),
	}
}
