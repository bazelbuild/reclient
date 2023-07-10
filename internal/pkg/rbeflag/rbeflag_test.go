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

package rbeflag

import (
	"flag"
	"os"
	"testing"
)

func TestParseUnset(t *testing.T) {
	cl := flag.CommandLine
	t.Cleanup(func() {
		flag.CommandLine = cl
	})
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	f := flag.String("value", "", "Some value")

	Parse()
	if *f != "" {
		t.Errorf("Flag has wrong value, want '', got %q", *f)
	}
}

func TestParseSet(t *testing.T) {
	cl := flag.CommandLine
	t.Cleanup(func() {
		flag.CommandLine = cl
		os.Setenv("RBE_value", "")
	})
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	f := flag.String("value", "", "Some value")
	os.Setenv("RBE_value", "test")
	Parse()
	if *f != "test" {
		t.Errorf("Flag has wrong value, want 'test', got %q", *f)
	}
}

func TestParseDefault(t *testing.T) {
	cl := flag.CommandLine
	t.Cleanup(func() {
		flag.CommandLine = cl
	})
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	f := flag.String("value", "default", "Some value")
	Parse()
	if *f != "default" {
		t.Errorf("Flag has wrong value, want 'default', got %q", *f)
	}
}

func TestParseSetRBEWins(t *testing.T) {
	cl := flag.CommandLine
	t.Cleanup(func() {
		flag.CommandLine = cl
	})
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	f := flag.String("value", "", "Some value")
	os.Setenv("FLAG_value", "test")
	os.Setenv("RBE_value", "test2")
	t.Cleanup(func() {
		os.Setenv("RBE_value", "")
		os.Setenv("FLAG_value", "")
	})
	Parse()
	if *f != "test2" {
		t.Errorf("Flag has wrong value, want 'test2', got %q", *f)
	}
}

func TestParseCommandLineWins(t *testing.T) {
	cl := flag.CommandLine
	t.Cleanup(func() {
		flag.CommandLine = cl
	})
	oldArgs := os.Args
	os.Args = []string{"cmd", "--value=cmd"}
	t.Cleanup(func() {
		os.Args = oldArgs
	})
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	f := flag.String("value", "", "Some value")
	os.Setenv("RBE_value", "test")
	t.Cleanup(func() {
		os.Setenv("RBE_value", "")
	})
	Parse()
	if *f != "cmd" {
		t.Errorf("Flag has wrong value, want 'cmd', got %q", *f)
	}
}

// TestParseConfigFile checks that the config file parsing is working as
// it should.  It doesn't check any interaction with the flag setup directly.
func TestParseConfigFile(t *testing.T) {
	cfgFile, err := os.CreateTemp(t.TempDir(), "test.cfg")
	if err != nil {
		t.Fatalf("Failed creating tmp file: %v", err)
	}
	// Write configuration file to read.
	cfgFile.WriteString("#This comment should be ignored\n")
	cfgFile.WriteString("\n")
	cfgFile.WriteString("arg=xyz\n")
	cfgFile.WriteString("boolarg\n")
	cfgFile.WriteString("lbl=a=b\n")
	cfgFile.WriteString("ext 45=67")
	cfgFile.Close()
	cfgFlags, err := parseFromFile(cfgFile.Name())

	v, ok := cfgFlags[""]
	if ok {
		t.Errorf("wanted nothing, got key \"\"")
	}
	v, ok = cfgFlags["#This"]
	if ok {
		t.Errorf("wanted nothing, got comment")
	}
	v, ok = cfgFlags["arg"]
	if !ok {
		t.Errorf("wanted value 'xyz', got nothing")
	}
	if v != "xyz" {
		t.Errorf("wanted value 'xyz', got %v", v)
	}
	v, ok = cfgFlags["boolarg"]
	if !ok {
		t.Errorf("wanted value 'true', got nothing")
	}
	if v != "true" {
		t.Errorf("wanted value 'true', got %v", v)
	}
	v, ok = cfgFlags["lbl"]
	if !ok {
		t.Errorf("wanted value 'a=b', got nothing")
	}
	if v != "a=b" {
		t.Errorf("wanted value 'a=b', got %v", v)
	}
	v, ok = cfgFlags["ext"]
	if !ok {
		t.Errorf("wanted value '45=67', got nothing")
	}
	if v != "45=67" {
		t.Errorf("wanted value '45=67', got %v", v)
	}
}

// TestParseWithConfigFile checks that a defined flag that is set in the
// configuration file actually sets the appropriate flag.
func TestParseWithConfigFile(t *testing.T) {
	cfgFile, err := os.CreateTemp(t.TempDir(), "test.cfg")
	if err != nil {
		t.Fatalf("Failed creating tmp file: %v", err)
	}
	// Write configuration file to read.
	cfgFile.WriteString("arg=xyz\n")
	cfgFile.WriteString("boolarg\n")
	cfgFile.Close()

	// Make a copy of the original os.Args
	argsCopy := make([]string, len(os.Args))
	copy(argsCopy, os.Args)
	t.Cleanup(func() {
		copy(os.Args, argsCopy)
	})

	cl := flag.CommandLine
	t.Cleanup(func() {
		flag.CommandLine = cl
	})
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	os.Args = []string{os.Args[0], "--cfg", cfgFile.Name()}
	// Note, the cfg flag is defined in Parse, so don't need to define it here.
	f := flag.String("arg", "", "Some value")
	flag.CommandLine.Set("cfg", cfgFile.Name())
	Parse()
	if *f != "xyz" {
		t.Errorf("Flag has wrong value, want 'xyz', got %q", *f)
	}
}

// TestParseWithConfigFileCmdLineWins checks that an argument set on both
// the command line and in the config file, the value from the command line
// is set.
func TestParseWithConfigFileCmdLineWins(t *testing.T) {
	cfgFile, err := os.CreateTemp(t.TempDir(), "test.cfg")
	if err != nil {
		t.Fatalf("Failed creating tmp file: %v", err)
	}
	// Write configuration file to read.
	cfgFile.WriteString("arg=xyz\n")
	cfgFile.Close()

	// Make a copy of the original os.Args
	argsCopy := make([]string, len(os.Args))
	copy(argsCopy, os.Args)
	t.Cleanup(func() {
		copy(os.Args, argsCopy)
	})

	cl := flag.CommandLine
	t.Cleanup(func() {
		flag.CommandLine = cl
	})
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	os.Args = []string{os.Args[0], "--cfg", cfgFile.Name(), "--arg", "abc"}
	// Note, the cfg flag is defined in Parse, so don't need to define it here.
	f := flag.String("arg", "", "Some value")
	flag.CommandLine.Set("cfg", cfgFile.Name())
	Parse()
	if *f != "abc" {
		t.Errorf("Flag has wrong value, want 'abc', got %q", *f)
	}
}
