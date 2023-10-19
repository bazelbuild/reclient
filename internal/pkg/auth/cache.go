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
	"os"
	"time"

	apb "github.com/bazelbuild/reclient/api/auth"

	log "github.com/golang/glog"
	"github.com/hectane/go-acl"
	"golang.org/x/oauth2"
	"google.golang.org/protobuf/encoding/prototext"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
)

// CachedCredentials are the credentials cached to disk.
type cachedCredentials struct {
	m          Mechanism
	refreshExp time.Time
	token      *oauth2.Token
}

func loadFromDisk(tf string) (cachedCredentials, error) {
	if tf == "" {
		return cachedCredentials{}, nil
	}
	blob, err := os.ReadFile(tf)
	if err != nil {
		return cachedCredentials{}, err
	}
	cPb := &apb.Credentials{}
	if err := prototext.Unmarshal(blob, cPb); err != nil {
		return cachedCredentials{}, err
	}
	accessToken := cPb.GetToken()
	exp := TimeFromProto(cPb.GetExpiry())
	var token *oauth2.Token
	if accessToken != "" && !exp.IsZero() {
		token = &oauth2.Token{
			AccessToken: accessToken,
			Expiry:      exp,
		}
	}
	c := cachedCredentials{
		m:          protoToMechanism(cPb.GetMechanism()),
		token:      token,
		refreshExp: TimeFromProto(cPb.GetRefreshExpiry()),
	}
	if !c.m.Cacheable() {
		// Purge non cacheable credentials from disk.
		if err := os.Remove(tf); err != nil {
			log.Warningf("Unable to remove cached credentials file %q, err=%v", tf, err)
		}
		// TODO(b/2028466): Do not use the non-cacheable mechanism even for the
		// current run.
	}
	log.Infof("Loaded cached credentials of type %v, expires at %v", c.m, exp)
	return c, nil
}

func saveToDisk(c cachedCredentials, tf string) error {
	if tf == "" {
		return nil
	}
	cPb := &apb.Credentials{}
	cPb.Mechanism = mechanismToProto(c.m)
	if c.token != nil {
		cPb.Token = c.token.AccessToken
		cPb.Expiry = TimeToProto(c.token.Expiry)
	}
	if !c.refreshExp.IsZero() {
		cPb.RefreshExpiry = TimeToProto(c.refreshExp)
	}
	f, err := os.Create(tf)
	if err != nil {
		return err
	}
	// Only owner can read/write the credential cache.
	// This is consistent with gcloud's credentials.db.
	// os.OpenFile(..., 0600) is not used because it does not properly set ACLs on windows.
	if err := acl.Chmod(tf, 0600); err != nil {
		return err
	}
	defer f.Close()
	f.WriteString(prototext.Format(cPb))
	log.Infof("Saved cached credentials of type %v, expires at %v to %v", c.m, cPb.Expiry, tf)
	return nil
}

func mechanismToProto(m Mechanism) apb.AuthMechanism_Value {
	switch m {
	case Unknown:
		return apb.AuthMechanism_UNSPECIFIED
	case ADC:
		return apb.AuthMechanism_ADC
	case GCE:
		return apb.AuthMechanism_GCE
	case CredentialFile:
		return apb.AuthMechanism_CREDENTIAL_FILE
	case None:
		return apb.AuthMechanism_NONE
	default:
		return apb.AuthMechanism_UNSPECIFIED
	}
}

func protoToMechanism(p apb.AuthMechanism_Value) Mechanism {
	switch p {
	case apb.AuthMechanism_UNSPECIFIED:
		return Unknown
	case apb.AuthMechanism_ADC:
		return ADC
	case apb.AuthMechanism_GCE:
		return GCE
	case apb.AuthMechanism_NONE:
		return None
	case apb.AuthMechanism_CREDENTIAL_FILE:
		return CredentialFile
	default:
		return Unknown
	}
}

// TimeToProto converts a valid time.Time into a proto Timestamp.
func TimeToProto(t time.Time) *tspb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return tspb.New(t)
}

// TimeFromProto converts a valid Timestamp proto into a time.Time.
func TimeFromProto(tPb *tspb.Timestamp) time.Time {
	if tPb == nil {
		return time.Time{}
	}
	return tPb.AsTime()
}
