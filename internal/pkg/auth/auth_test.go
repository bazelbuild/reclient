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

package auth

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/oauth2"
	grpcOauth "google.golang.org/grpc/credentials/oauth"
)

func TestMechanismFromFlags(t *testing.T) {
	tests := []struct {
		name          string
		flags         []string
		wantMechanism Mechanism
	}{{
		name:          "adc",
		flags:         []string{"--" + UseAppDefaultCredsFlag},
		wantMechanism: ADC,
	}, {
		name:          "gce",
		flags:         []string{"--" + UseGCECredsFlag},
		wantMechanism: GCE,
	}, {
		name:          "none",
		flags:         []string{"--" + ServiceNoAuthFlag},
		wantMechanism: None,
	}, {
		name:          "cred file",
		flags:         []string{"--" + CredentialFileFlag + "=/myfile.json"},
		wantMechanism: CredentialFile,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cl := flag.CommandLine
			t.Cleanup(func() {
				flag.CommandLine = cl
			})
			oldArgs := os.Args
			os.Args = append([]string{"cmd"}, test.flags...)
			t.Cleanup(func() {
				os.Args = oldArgs
			})
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
			flag.Bool(UseGCECredsFlag, false, "Some value")
			flag.Bool(UseAppDefaultCredsFlag, false, "Some value")
			flag.Bool(ServiceNoAuthFlag, false, "Some value")
			flag.String(CredentialFileFlag, "", "Some value")
			flag.Parse()
			m, err := MechanismFromFlags()
			if err != nil {
				t.Errorf("MechanismFromFlags(%v) returned error: %v", test.flags, err)
			}
			if m != test.wantMechanism {
				t.Errorf("MechanismFromFlags(%v) failed, want %v, got %v", test.flags, test.wantMechanism, m)
			}
		})
	}
}

func TestCredentialsHelperCache(t *testing.T) {
	dir, err := os.MkdirTemp("", "test")
	if err != nil {
		t.Errorf("failed to create the temp directory: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	cf := filepath.Join(dir, "reproxy.creds")
	err = os.MkdirAll(filepath.Dir(cf), 0755)
	if err != nil {
		t.Errorf("failed to create dir for credentials file %q: %v", cf, err)
	}
	cmd := exec.Command("credsHelperCmd")
	baseTs := &externalTokenSource{credsHelperCmd: cmd}
	ts := &grpcOauth.TokenSource{
		TokenSource: oauth2.ReuseTokenSourceWithExpiry(
			&oauth2.Token{},
			baseTs,
			5*time.Minute,
		),
	}
	creds := &Credentials{
		m:           CredentialsHelper,
		refreshExp:  time.Time{},
		tokenSource: ts,
		credsFile:   cf,
	}
	creds.SaveCredsToDisk(cmd)
	c, err := LoadCredsFromDisk(cf, cmd)
	if err != nil {
		t.Errorf("LoadCredsFromDisk failed: %v", err)
	}
	if creds.m != c.m {
		t.Errorf("mechanism was cached incorrectly, got: %v, want: %v", c.m, creds.m)
	}
}
