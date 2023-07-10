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

package reproxy

import (
	"context"
	"path/filepath"
	"sort"

	"team/foundry-x/re-client/internal/pkg/protoencoding"

	"github.com/bazelbuild/remote-apis-sdks/go/pkg/digest"

	lpb "team/foundry-x/re-client/api/log"

	log "github.com/golang/glog"
)

func compareAction(ctx context.Context, s *Server, a *action) {
	if !a.compare {
		return
	}
	mismatches := make(map[string]*lpb.Verification_Mismatch)
	numVerified := 0

	for _, rerun := range a.rec.LocalMetadata.RerunMetadata {
		numVerified += findMismatches(rerun.OutputFileDigests, rerun.OutputDirectoryDigests, mismatches, a, false)
	}

	if a.rec.RemoteMetadata != nil {
		for _, rerun := range a.rec.RemoteMetadata.RerunMetadata {
			numVerified += findMismatches(rerun.OutputFileDigests, rerun.OutputDirectoryDigests, mismatches, a, true)
		}
	}

	for fp, mismatch := range mismatches {
		localMismatches := len(mismatch.LocalDigests)
		remoteMismatches := len(mismatch.RemoteDigests)

		if localMismatches > 1 {
			mismatch.Determinism = lpb.DeterminismStatus_NON_DETERMINISTIC
		} else if remoteMismatches > 1 {
			mismatch.Determinism = lpb.DeterminismStatus_REMOTE_NON_DETERMINISTIC
		} else if localMismatches == 1 && remoteMismatches == 1 && (a.numLocalReruns > 1 || a.numRemoteReruns > 1 || mismatch.LocalDigests[0] == mismatch.RemoteDigests[0]) {
			mismatch.Determinism = lpb.DeterminismStatus_DETERMINISTIC
			if mismatch.LocalDigests[0] == mismatch.RemoteDigests[0] {
				delete(mismatches, fp)
			}
		} else {
			mismatch.Determinism = lpb.DeterminismStatus_UNKNOWN
		}

	}
	verRes := mismatchesToProto(mismatches, numVerified)
	a.rec.LocalMetadata.Verification = verRes
	if len(mismatches) > 0 {
		log.Errorf(
			"%v: Found diff in outputs:\n%s",
			a.cmd.Identifiers.ExecutionID,
			protoencoding.TextWithIndent.Format(a.rec.LocalMetadata.Verification),
		)
	}
}

func mismatchesToProto(mismatches map[string]*lpb.Verification_Mismatch, numVerified int) *lpb.Verification {
	// We need to output mismatches in fixed order, for tests.
	var keys []string
	for n := range mismatches {
		keys = append(keys, n)
	}
	sort.Strings(keys)
	res := &lpb.Verification{}
	for _, path := range keys {
		res.Mismatches = append(res.Mismatches, mismatches[path])
	}
	res.TotalMismatches = int32(len(mismatches))
	res.TotalVerified = int64(numVerified)
	return res
}

func compareResults(localDigests, remoteDigests map[string]digest.Digest, actionDigest string) (map[string]*lpb.Verification_Mismatch, int) {
	mismatches := make(map[string]*lpb.Verification_Mismatch)
	numVerified := 0
	for path, dl := range localDigests {
		numVerified++
		m := &lpb.Verification_Mismatch{
			Path:         path,
			LocalDigest:  dl.String(),
			ActionDigest: actionDigest,
		}
		if dr, ok := remoteDigests[path]; ok {
			delete(remoteDigests, path)
			if dr != dl {
				m.RemoteDigests = []string{dr.String()}
				mismatches[path] = m
			}
			continue
		}
		mismatches[path] = m
	}
	for path, dr := range remoteDigests {
		numVerified++
		mismatches[path] = &lpb.Verification_Mismatch{
			Path:          path,
			RemoteDigests: []string{dr.String()},
			ActionDigest:  actionDigest,
		}
	}
	return mismatches, numVerified
}

func findMismatches(fileDg map[string]string, dirDg map[string]string, mismatches map[string]*lpb.Verification_Mismatch, a *action, remote bool) int {
	numVerified := 0
	allDg := make(map[string]string)
	for k, v := range fileDg {
		allDg[k] = v
	}
	for k, v := range dirDg {
		allDg[k] = v
	}

	for fp, dg := range allDg {
		// Paths need to be normalized to forward slashes as all RBE paths use forward slashes irrespective of platform.
		fp = filepath.ToSlash(fp)
		if _, ok := mismatches[fp]; !ok {
			numVerified++
			mismatches[fp] = &lpb.Verification_Mismatch{
				Path:         fp,
				ActionDigest: a.digest,
			}
		}
		if remote {
			mismatches[fp].RemoteDigests = dedup(append(mismatches[fp].RemoteDigests, dg))
		} else {
			mismatches[fp].LocalDigests = dedup(append(mismatches[fp].LocalDigests, dg))
		}
	}
	return numVerified
}
