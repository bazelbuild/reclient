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

// Package rbeflag allows parsing of flags that can be set in environment variables.
package rbeflag

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"

	log "github.com/golang/glog"
)

var (
	rgx = regexp.MustCompile(`[\s=]`)
)

// Parse parses flags which are set in environment variables using the RBE_ prefix, otherwise
// FLAG_ prefix. If the flag is set in the command line, the command line value of the flag
// takes precedence over the environment variable value.
// If the flag 'cfg' is set to a file (either on the command-line or via an RBE_ prefixed
// environment variable), that file will be processed and any flag that is defined in that
// file that is not already set will be set to the value defined in the file. Flags already
// set via an environment variable or directly on the command line will not be overridden
// by the contents of the config file.
func Parse() {
	if !flag.Parsed() {
		cfgFile := flag.String("cfg", "", "Optional configuration file containing command-line argument settings")
		ParseFromEnv()
		flag.Parse()
		if *cfgFile != "" {
			cfgMap, err := parseFromFile(*cfgFile)
			if err != nil {
				log.Fatalf("failed reading config file %v: %v", *cfgFile, err)
			}
			// Remove keys from the map that are already set.
			flag.Visit(func(f *flag.Flag) {
				delete(cfgMap, f.Name)
			})
			// Set the flags remaining in the config map.
			for k, v := range cfgMap {
				if err := flag.Set(k, v); err != nil {
					log.Warningf("Failed to set flag %v to %q: %v", k, v, err)
				}
			}
		}
	}
}

// parseFromFile parses flags which are defined in a configuration file. The file format is a
// single argument per line. For arguments assigning values, they should be separated by '=' or
// whitespace.
// Prefixed dashes (single or double) should not be included, but will be removed if they are.
// Returns a map for the contents of the configuration file.
func parseFromFile(cfg string) (map[string]string, error) {
	f, err := os.Open(cfg)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	cfgFlags := make(map[string]string)
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		splits := rgx.Split(line, 2)
		splits[0] = strings.TrimPrefix(strings.TrimPrefix(splits[0], "-"), "-")
		if len(splits) == 1 {
			cfgFlags[splits[0]] = "true"
		} else {
			cfgFlags[splits[0]] = splits[1]
		}
	}
	if err := scan.Err(); err != nil {
		return nil, err
	}
	return cfgFlags, nil
}

// ParseFromEnv parses flags which are set in environment variables using the RBE_ prefix, and if
// not set, fallback to the FLAG_ prefix.
func ParseFromEnv() {
	flag.VisitAll(func(f *flag.Flag) {
		for _, prefix := range []string{"RBE_", "FLAG_"} {
			if v, ok := os.LookupEnv(prefix + f.Name); ok {
				flag.Set(f.Name, v)
				return
			}
		}
	})
}

// LogAllFlags logs the current values of all flags.
func LogAllFlags(verbosity log.Level) {
	var cmd []string
	flag.VisitAll(func(f *flag.Flag) {
		cmd = append(cmd, fmt.Sprintf("--%v=%v", f.Name, f.Value))
	})
	log.V(verbosity).Infof("Command line flags:\n%s", strings.Join(cmd, " \\\n"))
}
