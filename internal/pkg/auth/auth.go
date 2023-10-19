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
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"


	log "github.com/golang/glog"
	"golang.org/x/oauth2"
	grpcOauth "google.golang.org/grpc/credentials/oauth"
)

const (
	googleTokenInfoURL = "https://oauth2.googleapis.com"
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
	// Invalid implies a valid mechanism exists, but it cannot provide credentials
	// at the moment (invalid password or other similar problems)
	Invalid
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
	case Invalid:
		return "Invalid"
	default:
		return "Incorrect Value"
	}
}

const (
	// TODO(b/261172745): define these flags in reproxy rather than in the SDK.

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

var nowFn = time.Now

// Error is an error occured during authenticating or initializing credentials.
type Error struct {
	error
	// ExitCode is the exit code for the error.
	ExitCode int
}

// Credentials provides auth functionalities with a specific auth mechanism.
type Credentials struct {
	m           Mechanism
	refreshExp  time.Time
	tokenSource *grpcOauth.TokenSource
	credsFile   string
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

// Cacheable returns true if this mechanism should be cached to disk
func (m Mechanism) Cacheable() bool {
	return false
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


// NewCredentials initializes a credentials object.
func NewCredentials(m Mechanism, credsFile string, channelInitTimeout time.Duration) (*Credentials, error) {
	return newCredentials(m, credsFile, googleTokenInfoURL, channelInitTimeout)
}

func newCredentials(m Mechanism, credsFile, tokenInfoURL string, channelInitTimeout time.Duration) (*Credentials, error) {
	cc, err := loadFromDisk(credsFile)
	if err != nil {
		log.Warningf("Failed to load credentials cache file from %v: %v", credsFile, err)
		return buildCredentials(cachedCredentials{m: m}, credsFile, tokenInfoURL, channelInitTimeout)
	}
	if cc.m != m {
		log.Warningf("Cached mechanism (%v) is not the same as requested mechanism (%v). Will attempt to authenticate using the requested mechanism.", cc.m, m)
		return buildCredentials(cachedCredentials{m: m}, credsFile, tokenInfoURL, channelInitTimeout)
	}
	return buildCredentials(cc, credsFile, tokenInfoURL, channelInitTimeout)
}

func buildCredentials(baseCreds cachedCredentials, credsFile, tokenInfoURL string, channelInitTimeout time.Duration) (*Credentials, error) {
	if baseCreds.m == Unknown {
		return nil, errors.New("cannot initialize credentials with unknown mechanism")
	}
	c := &Credentials{
		m:          baseCreds.m,
		refreshExp: baseCreds.refreshExp,
		credsFile:  credsFile,
	}
	return c, nil
}

// SaveToDisk saves credentials to disk.
func (c *Credentials) SaveToDisk() {
	if c == nil {
		return
	}
	if !c.m.Cacheable() {
		return
	}
	cc := cachedCredentials{m: c.m, refreshExp: c.refreshExp}
	if c.tokenSource != nil && c.refreshExp.IsZero() {
		// Since c.tokenSource is always wrapped in a oauth2.ReuseTokenSourceWithExpiry
		// this will return a cached credential if one exists.
		t, err := c.tokenSource.Token()
		if err != nil {
			log.Errorf("Failed to get token to persist to disk: %v", err)
			return
		}
		cc.token = t
	}
	if err := saveToDisk(cc, c.credsFile); err != nil {
		log.Errorf("Failed to save credentials to disk: %v", err)
	}
}

// RemoveFromDisk deletes the credentials cache on disk.
func (c *Credentials) RemoveFromDisk() {
	if c == nil {
		return
	}
	if err := os.Remove(c.credsFile); err != nil {
		log.Errorf("Failed to remove credentials from disk: %v", err)
	}
}

// UpdateStatus updates the refresh expiry time if it is expired
func (c *Credentials) UpdateStatus() (int, error) {
	if nowFn().Before(c.refreshExp) {
		return 0, nil
	}
	return 0, nil
}

// ReproxyAuthenticationFlags retrieves the auth flags to use to start reproxy.
func (m Mechanism) ReproxyAuthenticationFlags() []string {
	bm := make(map[string]bool, len(boolAuthFlags))
	sm := make(map[string]string, len(stringAuthFlags))
	for _, f := range boolAuthFlags {
		bm[f] = false
	}
	for _, f := range stringAuthFlags {
		sm[f] = ""
	}
	switch m {
	case GCE:
		bm[UseGCECredsFlag] = true
	case ADC:
		bm[UseAppDefaultCredsFlag] = true
	case CredentialFile:
		if f := flag.Lookup(CredentialFileFlag); f != nil {
			sm[CredentialFileFlag] = f.Value.String()
		}
	case None:
		bm[ServiceNoAuthFlag] = true
	}
	vals := make([]string, 0, len(boolAuthFlags)+len(stringAuthFlags))
	for _, f := range boolAuthFlags {
		vals = append(vals, fmt.Sprintf("--%v=%v", f, bm[f]))
	}
	for _, f := range stringAuthFlags {
		vals = append(vals, fmt.Sprintf("--%v=%v", f, sm[f]))
	}
	return vals
}

// Mechanism returns the authentication mechanism of the credentials object.
func (c *Credentials) Mechanism() Mechanism {
	if c == nil {
		return None
	}
	return c.m
}

// TokenSource returns a token source for this credentials instance.
// If this credential type does not produce credentials nil will be returned.
func (c *Credentials) TokenSource() *grpcOauth.TokenSource {
	if c == nil {
		return nil
	}
	return c.tokenSource
}

// runAuthCommand runs a command to obtain credentials of the given authentication mechanism.
func runAuthCommand(m Mechanism) error {
	var cmd *exec.Cmd
	switch m {
	case ADC:
		cmd = exec.Command("gcloud", "auth", "application-default", "login")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
	default:
		return nil
	}
	fmt.Printf("Running %v\n", strings.Join(cmd.Args, " "))
	return cmd.Run()
}

func checkADCStatus() error {
	cmd := exec.Command("gcloud", "auth", "application-default", "print-access-token")
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

type gcpTokenProvider interface {
	String() string
	Token() (string, error)
}

// gcpTokenSource uses a gcpTokenProvider to obtain gcp oauth tokens.
// This should be wrapped in a "golang.org/x/oauth2".ReuseTokenSource
// to avoid obtaining new tokens each time.
type gcpTokenSource struct {
	p            gcpTokenProvider
	tokenInfoURL string
}

// Token retrieves a token from the underlying provider and check the expiration
// via the google HTTP endpoint.
func (ts *gcpTokenSource) Token() (*oauth2.Token, error) {
	t, err := ts.p.Token()
	if err != nil {
		return nil, err
	}
	expiry, err := getExpiry(ts.tokenInfoURL, t)
	if err != nil {
		return nil, err
	}
	log.Infof("%s credentials refreshed at %v, expires at %v", ts.p.String(), time.Now(), expiry)
	return &oauth2.Token{
		AccessToken: t,
		Expiry:      expiry,
	}, nil
}

func getExpiry(baseURL, token string) (time.Time, error) {
	resp, err := http.Get(fmt.Sprintf("%s/tokeninfo?access_token=%s", baseURL, token))
	if err != nil {
		return time.Time{}, fmt.Errorf("unable to verify token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return time.Time{}, fmt.Errorf("Error reading response body: %w", err)
		}
		return time.Time{}, fmt.Errorf("tokeninfo call failed with status: %d and body: %s", resp.StatusCode, body)
	}
	var ti struct {
		Exp int64 `json:"exp,string"`
	}
	json.NewDecoder(resp.Body).Decode(&ti)
	if ti.Exp == 0 {
		return time.Time{}, fmt.Errorf("tokeninfo did not return an expiry time")
	}
	return time.Unix(ti.Exp, 0), nil
}
