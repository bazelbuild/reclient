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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
	cmd := exec.Command("echo", `{"token":"testToken", "expiry":"", "refresh_expiry":""}`)
	baseTs := &externalTokenSource{credsHelperCmd: cmd}
	ts := &grpcOauth.TokenSource{
		TokenSource: oauth2.ReuseTokenSourceWithExpiry(
			&oauth2.Token{},
			baseTs,
			5*time.Minute,
		),
	}
	creds := &Credentials{
		m:              CredentialsHelper,
		refreshExp:     time.Time{},
		tokenSource:    ts,
		credsFile:      cf,
		credsHelperCmd: cmd,
	}
	creds.SaveToDisk()
	c, err := LoadCredsFromDisk(cf, cmd)
	if err != nil {
		t.Errorf("LoadCredsFromDisk failed: %v", err)
	}
	if creds.m != c.m {
		t.Errorf("mechanism was cached incorrectly, got: %v, want: %v", c.m, creds.m)
	}
}

func TestExternalToken(t *testing.T) {
	// TODO(b/316005337): This test is disabled for windows since it doesn't work on windows yet. This should be fixed before experimentalCredentialsHelper is used for windows.
	if runtime.GOOS != "windows" {
		expiry := time.Now().Truncate(time.Second)
		exp := expiry.Format(time.UnixDate)
		tk := "testToken"
		credshelper := "echo"
		credshelperArgs := fmt.Sprintf(`{"token":"%v","expiry":"%s","refresh_expiry":""}`, tk, exp)
		credsHelperCmd := exec.Command(credshelper, strings.Fields(credshelperArgs)...)
		ts := &externalTokenSource{
			credsHelperCmd: credsHelperCmd,
		}
		oauth2tk, err := ts.Token()
		if err != nil {
			t.Errorf("externalTokenSource.Token() returned an error: %v", err)
		}
		if oauth2tk.AccessToken != tk {
			t.Errorf("externalTokenSource.Token() returned token=%s, want=%s", oauth2tk.AccessToken, tk)
		}
		if !oauth2tk.Expiry.Equal(expiry) {
			t.Errorf("externalTokenSource.Token() returned expiry=%s, want=%s", oauth2tk.Expiry, exp)
		}
	}

}

func TestNewExternalCredentials(t *testing.T) {
	testToken := "token"
	credshelper := "echo"
	exp := time.Now().Add(time.Hour).Truncate(time.Second)
	expStr := exp.String()
	unixExp := exp.Format(time.UnixDate)
	tests := []struct {
		name            string
		wantErr         bool
		checkExp        bool
		credshelperArgs string
	}{{
		name:            "No Token",
		wantErr:         true,
		credshelperArgs: fmt.Sprintf(`{"token":"","expiry":"","refresh_expiry":""}`),
	}, {
		name:            "Credshelper Command Passed - No Expiry",
		credshelperArgs: fmt.Sprintf(`{"token":"%v","expiry":"","refresh_expiry":""}`, testToken),
	}, {
		name:            "Credshelper Command Passed - Expiry",
		checkExp:        true,
		credshelperArgs: fmt.Sprintf(`{"token":"%v","expiry":"%v","refresh_expiry":""}`, testToken, unixExp),
	}, {
		name:            "Credshelper Command Passed - Refresh Expiry",
		checkExp:        true,
		credshelperArgs: fmt.Sprintf(`{"token":"%v","expiry":"%v","refresh_expiry":"%v"}`, testToken, unixExp, unixExp),
	}, {
		name:            "Wrong Expiry Format",
		wantErr:         true,
		credshelperArgs: fmt.Sprintf(`{"token":"%v","expiry":"%v","refresh_expiry":"%v"}`, testToken, expStr, expStr),
	}}
	if runtime.GOOS != "windows" {
		// TODO(b/316005337): This test is disabled for windows since it doesn't work on windows yet. This should be fixed before experimentalCredentialsHelper is used for windows.
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				c, err := NewExternalCredentials(credshelper, test.credshelperArgs, "")
				if test.wantErr && err == nil {
					t.Fatalf("NewExternalCredentials did not return an error.")
				}
				if !test.wantErr {
					if err != nil {
						t.Fatalf("NewExternalCredentials returned an error: %v", err)
					}
					if c.m != CredentialsHelper {
						t.Errorf("NewExternalCredentials returned credentials with mechanism=%v, want=%v", c.m, CredentialsHelper)
					}
					if c.tokenSource == nil {
						t.Fatalf("NewExternalCredentials returned credentials with a nil tokensource.")
					}
					tk, err := c.tokenSource.Token()
					if err != nil {
						t.Fatalf("tokensource.Token() call failed: %v", err)
					}
					if tk.AccessToken != testToken {
						t.Fatalf("tokensource.Token() gave token=%s, want=%s",
							tk.AccessToken, testToken)
					}
					if test.checkExp && !exp.Equal(tk.Expiry) {
						t.Fatalf("tokensource.Token() gave expiry=%v, want=%v",
							tk.Expiry, exp)
					}
				}
			})
		}
	}
}
