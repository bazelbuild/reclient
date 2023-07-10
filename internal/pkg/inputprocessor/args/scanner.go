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

package args

import (
	"strings"
)

// Scanner scans command line arguments.
// it scans args and gets (flag, args, value) in each Next call.
//
// for `-flagname`, it is recognized as flag by default,
// and it returns ("-flagname", ["-flagname"], nil).
//
// for `-flagnameflagvalue`, need to set "-flagname" in joined,
// and it returns ("-flagname", ["-flagnameflagvalue"], ["flagvalue"]).
//
// for `-flagname flagvalue`, need to set flags["-flagname"] = 1,
// and it returns ("-flagname", ["-flagname", "flagvalue"], ["flagvalue"]).
//
// for non-flag "parameter", it returns ("", ["parameter"], nil).
//
// if "-flagname" is used for both `-flagnameflagvalue`,
// `-flagname flagvalue`,
// need to set it both in joined and flags.
//
// to accept "/flagname", need to use "/flagname" for flags, joined.
//
// for `--flagname`, need to use "--flagname" for flags, joined.
type Scanner struct {
	// Args is remaining arguments.
	Args []string

	// Flags are map keyed by -flag and number of argments for the flag.
	// The flag requires additional value from args if value > 0.
	// The flag doesn't require additional value from args if value == 0
	Flags map[string]int

	// Joined are prefixes of flag that has value in the same arg.
	// sorted Prefixes by reverse lexicographic order.
	Joined []PrefixOption

	// flag normalization.
	Normalized map[string]string
}

// NextResult is a result of Scanner's Next operation
type NextResult struct {
	Args          []string
	NormalizedKey string
	OriginalKey   string
	Values        []string
	Joined        bool
}

// PrefixOption is option for joined flags.
type PrefixOption struct {
	Prefix  string
	NumArgs int
}

func newNextResult(normalizedKey, originalKey string, args, values []string, joined bool) *NextResult {
	return &NextResult{NormalizedKey: normalizedKey, OriginalKey: originalKey, Args: args, Values: values, Joined: joined}
}

// HasNext returns true if there is more args to process.
func (s *Scanner) HasNext() bool {
	return len(s.Args) > 0
}

// NextResult returns next flag.
// NormalizedKey is normalized flag,
// or empty string if not started with "-".
// OriginalKey is a key before the normalization
// Args are consumed arguments.
// Values are flag value if flag needs value (next arg in args) or
// Joined (rest after prefix in the arg).
func (s *Scanner) NextResult() *NextResult {
	flag := s.Args[0]
	normalizedFlag, values, args := s.normalizedFlag(s.Args[0]), []string{}, []string{}
	numArgs, ok := s.Flags[normalizedFlag]
	if ok {
		if numArgs < 0 {
			args = s.Args
			s.Args = nil
		} else {
			args, s.Args = s.Args[:numArgs+1], s.Args[numArgs+1:]
		}
		if numArgs > 0 {
			values = args[1:]
		}
		return newNextResult(normalizedFlag, flag, args, values, false)
	}
	for _, f := range s.Joined {
		if strings.HasPrefix(flag, f.Prefix) {
			values = []string{strings.TrimPrefix(flag, f.Prefix)}
			flag, normalizedFlag = f.Prefix, s.normalizedFlag(f.Prefix)
			if f.NumArgs < 0 {
				args = s.Args
				s.Args = nil
			} else {
				args, s.Args = s.Args[:f.NumArgs+1], s.Args[f.NumArgs+1:]
			}
			if len(args) > 0 {
				values = append(values, args[1:]...)
			}
			return newNextResult(normalizedFlag, flag, args, values, true)
		}
	}
	args, s.Args = s.Args[:1], s.Args[1:]
	if strings.HasPrefix(flag, "-") {
		if len(args) > 0 {
			values = args[1:]
		}
		return newNextResult(flag, flag, args, values, false)
	}
	values = args[:]
	return newNextResult("", "", args, values, false)
}

// Next returns next flag.
// flag is normalized flag,
// empty string if not started with "-".
// args are consumed arguments.
// values are flag value if flag needs value (next arg in args) or
// joined (rest after prefix in the arg).
func (s *Scanner) Next() (string, []string, []string, bool) {
	result := s.NextResult()
	return result.NormalizedKey, result.Args, result.Values, result.Joined
}

func (s *Scanner) normalizedFlag(flag string) string {
	if normalizedFlag, ok := s.Normalized[flag]; ok {
		return normalizedFlag
	}
	return flag
}

// LookAhead looks ahead next flag.
func (s *Scanner) LookAhead() string {
	flag, args, _, _ := s.Next()
	s.Args = append(args, s.Args...)
	return flag
}
