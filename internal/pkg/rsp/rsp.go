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

// Package rsp provides the ability to parse rsp files.
package rsp

import (
	"os"
	"strings"

	"github.com/bazelbuild/reclient/internal/pkg/inputprocessor/args"
)

// Parse parses the given rsp file to return a list of file paths specified in the file.
func Parse(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	res := strings.Fields(string(data))
	for i, flag := range res {
		res[i] = strings.Trim(flag, "'\"")
	}
	return res, nil
}

// ParseWithFunc parses the given rsp file, using the provided scanner as a template to parse the
// flags and the argHandler function to process those flags.
func ParseWithFunc(path string, scanTemplate args.Scanner, argHandler func(s *args.Scanner) error) error {
	res, err := Parse(path)
	if err != nil {
		return err
	}
	// Create a scanner to scan through the arguments from the rsp file, using the passed-in
	// scanner to create a matching one (in that it parses flags the same way).
	scanner := &args.Scanner{
		Args:       res,
		Flags:      scanTemplate.Flags,
		Joined:     scanTemplate.Joined,
		Normalized: scanTemplate.Normalized,
	}

	// Pass each result to the provided handler.
	for scanner.HasNext() {
		scanner.ReadNextFlag()
		if err := argHandler(scanner); err != nil {
			return err
		}
	}
	return nil
}
