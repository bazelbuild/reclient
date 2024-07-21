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

// Package auth implements common functionality to authenticate reclient against GCP.
package auth

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	log "github.com/golang/glog"
	"golang.org/x/oauth2"
	googleOauth "golang.org/x/oauth2/google"
)

// Exit codes to indicate various causes of authentication failure.
const (
	// ExitCodeNoAuth is the exit code when no auth option is specified.
	ExitCodeNoAuth = 14
	// ExitCodeCredsFileAuth is the exit code when there is a failure to authenticate using a credentials file.
	ExitCodeCredsFileAuth = 13
	// ExitCodeGCECredsAuth is the exit code when there is a failure in GCE credentials.
	ExitCodeGCECredsAuth = 12
	// ExitCodeExternalTokenAuth is the exit code when there is a failure to authenticate with an external token.
	ExitCodeExternalTokenAuth = 11
	// ExitCodeAppDefCredsAuth is the exit code when there is a failure to authenticate with ADC.
	ExitCodeAppDefCredsAuth = 10
	// ExitCodeUnknown is the exit code when there is an unknown auth issue.
	ExitCodeUnknown = 19
)

// Mechanism is a mechanism of authentication to the remote execution service.
type Mechanism int

const (
	// Unknown is an unknown auth mechanism.
	Unknown Mechanism = iota

	// ADC is GCP's application default credentials authentication mechanism.
	ADC
	// GCE is authentication using GCE VM service accounts.
	GCE
	// CredentialFile is using service account credentials from a proviced file
	CredentialFile
	// None implies that the user will not use authentication
	None
)

// String returns the string representation of the auth mechanism.
func (m Mechanism) String() string {
	switch m {
	case Unknown:
		return "Unknown"
	case ADC:
		return "ADC"
	case GCE:
		return "GCE"
	case CredentialFile:
		return "CredentialFile"
	case None:
		return "None"
	default:
		return "Incorrect Value"
	}
}

const (
	// CredshelperPathFlag is the path to the credentials helper binary.
	CredshelperPathFlag = "experimental_credentials_helper"
	// CredshelperArgsFlag is the flag used to pass in the arguments to the credentials helper binary.
	CredshelperArgsFlag = "experimental_credentials_helper_args"
	// UseAppDefaultCredsFlag is used to authenticate with application default credentials.
	UseAppDefaultCredsFlag = "use_application_default_credentials"
	// UseExternalTokenFlag indicates the user will authenticate with a provided token.
	UseExternalTokenFlag = "use_external_auth_token"
	// UseGCECredsFlag indicates the user will authenticate with GCE VM credentials.
	UseGCECredsFlag = "use_gce_credentials"
	// ServiceNoAuthFlag indicates the user will not use authentication
	ServiceNoAuthFlag = "service_no_auth"
	// CredentialFileFlag indicates the user authenticate with a credential file
	CredentialFileFlag = "credential_file"
)

var boolAuthFlags = []string{
	UseAppDefaultCredsFlag,
	UseGCECredsFlag,
	UseExternalTokenFlag,
	ServiceNoAuthFlag,
}

var stringAuthFlags = []string{
	CredentialFileFlag,
}

// Error is an error occured during authenticating or initializing credentials.
type Error struct {
	error
	// ExitCode is the exit code for the error.
	ExitCode int
}

// MechanismFromFlags returns an auth Mechanism based on flags currently set.
func MechanismFromFlags() (Mechanism, error) {
	vals := make(map[string]bool, len(boolAuthFlags)+len(stringAuthFlags))
	var errs []string
	for _, name := range boolAuthFlags {
		b, err := boolFlagVal(name)
		if err != nil {
			errs = append(errs, err.Error())
		}
		vals[name] = b
	}
	if len(errs) > 0 {
		return Unknown, fmt.Errorf("encountered error(s) parsing auth flags:\n%v", strings.Join(errs, "\n"))
	}
	for _, name := range stringAuthFlags {
		f := flag.Lookup(name)
		vals[name] = f != nil && f.Value.String() != ""
	}
	if vals[ServiceNoAuthFlag] {
		return None, nil
	}
	if vals[CredentialFileFlag] {
		return CredentialFile, nil
	}
	if vals[UseAppDefaultCredsFlag] {
		return ADC, nil
	}
	if vals[UseGCECredsFlag] {
		return GCE, nil
	}
	return Unknown, &Error{fmt.Errorf("couldn't determine auth mechanism from flags %v", vals), ExitCodeNoAuth}
}

func boolFlagVal(flagName string) (bool, error) {
	if f := flag.Lookup(flagName); f != nil && f.Value.String() != "" {
		b, err := strconv.ParseBool(f.Value.String())
		if err != nil {
			return false, fmt.Errorf("unable to parse boolean flag --%s: %w", flagName, err)
		}
		return b, nil
	}
	return false, nil
}

// UpdateStatus updates ADC credentials status if expired
func UpdateStatus(m Mechanism) (int, error) {
	if m == ADC {
		_, err := checkADCStatus()
		if err != nil {
			return ExitCodeAppDefCredsAuth, fmt.Errorf("application default credentials were invalid: %v", err)
		}
	}
	return 0, nil
}

func checkADCStatus() (time.Time, error) {
	ts, err := googleOauth.FindDefaultCredentialsWithParams(context.Background(), googleOauth.CredentialsParams{
		Scopes:            []string{"https://www.googleapis.com/auth/cloud-platform"},
		EarlyTokenRefresh: 5 * time.Minute,
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("could not find Application Default Credentials: %w", err)
	}
	token, err := ts.TokenSource.Token()
	if err != nil {
		aerr, ok := err.(*googleOauth.AuthenticationError)
		if !ok {
			return time.Time{}, fmt.Errorf("could not get valid Application Default Credentials token: %w", err)
		}
		if aerr.Temporary() {
			log.Errorf("Ignoring temporary ADC error: %v", err)
			return time.Time{}, nil
		}
		rerr, ok := aerr.Unwrap().(*oauth2.RetrieveError)
		if !ok {
			return time.Time{}, fmt.Errorf("could not get valid Application Default Credentials token: %w", err)
		}
		var resp struct {
			Error        string `json:"error"`
			ErrorSubtype string `json:"error_subtype"`
		}
		if err := json.Unmarshal(rerr.Body, &resp); err == nil &&
			resp.Error == "invalid_grant" &&
			resp.ErrorSubtype == "invalid_rapt" {
			return time.Time{}, fmt.Errorf("reauth required, run `gcloud auth application-default login` and try again")
		}
		return time.Time{}, fmt.Errorf("could not get valid Application Default Credentials token: %w", err)
	}
	if !token.Valid() {
		log.Errorf("Could not get valid Application Default Credentials token: %v", err)
		return time.Time{}, fmt.Errorf("could not get valid Application Default Credentials token: %w", err)
	}
	return token.Expiry, nil
}
