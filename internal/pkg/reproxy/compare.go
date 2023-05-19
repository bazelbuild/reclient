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

	lpb "team/foundry-x/re-client/api/log"

	log "github.com/golang/glog"
)

func compareAction(ctx context.Context, s *Server, a *action) {
	if !a.compare {
		return
	}
	mismatches := make(map[string]*lpb.Verification_Mismatch)
	var localExitCodes []int32
	var remoteExitCodes []int32
	numVerified := 0

	for _, rerun := range a.rec.LocalMetadata.RerunMetadata {
		numVerified += findMismatches(rerun.OutputFileDigests, rerun.OutputDirectoryDigests, mismatches, a, false)
		localExitCodes = append(localExitCodes, rerun.Result.ExitCode)
	}

	if a.rec.RemoteMetadata != nil {
		for _, rerun := range a.rec.RemoteMetadata.RerunMetadata {
			numVerified += findMismatches(rerun.OutputFileDigests, rerun.OutputDirectoryDigests, mismatches, a, true)
			remoteExitCodes = append(remoteExitCodes, rerun.Result.ExitCode)
		}
	}

	localExitCodes = dedupExitCodes(localExitCodes)
	remoteExitCodes = dedupExitCodes(remoteExitCodes)

	for fp, mismatch := range mismatches {
		mismatch.LocalExitCodes = localExitCodes
		mismatch.RemoteExitCodes = remoteExitCodes

		dgStatus, shouldLogDg := compareDigests(mismatch.LocalDigests, mismatch.RemoteDigests, a.numLocalReruns, a.numRemoteReruns)
		ecStatus, shouldLogEc := compareExitCodes(mismatch.LocalExitCodes, mismatch.RemoteExitCodes, a.numLocalReruns, a.numRemoteReruns)

		// Delete the records that we don't want to keep.
		if !shouldLogDg && !shouldLogEc {
			delete(mismatches, fp)
		} else if !shouldLogDg {
			mismatch.LocalDigests = nil
			mismatch.RemoteDigests = nil
		} else if !shouldLogEc {
			mismatch.LocalExitCodes = nil
			mismatch.RemoteExitCodes = nil
		}

		switch {
		case dgStatus == lpb.DeterminismStatus_UNKNOWN || ecStatus == lpb.DeterminismStatus_UNKNOWN:
			mismatch.Determinism = lpb.DeterminismStatus_UNKNOWN
		case dgStatus == lpb.DeterminismStatus_REMOTE_NON_DETERMINISTIC || ecStatus == lpb.DeterminismStatus_REMOTE_NON_DETERMINISTIC:
			mismatch.Determinism = lpb.DeterminismStatus_REMOTE_NON_DETERMINISTIC
		case dgStatus == lpb.DeterminismStatus_NON_DETERMINISTIC || ecStatus == lpb.DeterminismStatus_NON_DETERMINISTIC:
			mismatch.Determinism = lpb.DeterminismStatus_NON_DETERMINISTIC
		default:
			mismatch.Determinism = lpb.DeterminismStatus_DETERMINISTIC
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

// Returns the determinism status of the digests and whether we should log the mismatch.
func compareDigests(localDigests []string, remoteDigests []string, numLocalReruns int, numRemoteReruns int) (lpb.DeterminismStatus, bool) {
	totalReruns := numLocalReruns + numRemoteReruns
	localMismatches := len(localDigests)
	remoteMismatches := len(remoteDigests)

	if localMismatches > 1 || remoteMismatches > 1 {
		if localMismatches == 1 {
			return lpb.DeterminismStatus_REMOTE_NON_DETERMINISTIC, true
		}
		return lpb.DeterminismStatus_NON_DETERMINISTIC, true
	}

	deterministicMismatch := localMismatches == 1 && remoteMismatches == 1 && localDigests[0] != remoteDigests[0] && totalReruns > 2
	localDeterministic := localMismatches == 1 && numRemoteReruns == 0
	remoteDeterministic := remoteMismatches == 1 && numLocalReruns == 0
	deterministic := localMismatches == 1 && remoteMismatches == 1 && localDigests[0] == remoteDigests[0]

	if localDeterministic || remoteDeterministic || deterministic || deterministicMismatch {
		return lpb.DeterminismStatus_DETERMINISTIC, deterministicMismatch
	}

	return lpb.DeterminismStatus_UNKNOWN, true
}

// Returns the determinism status of the exit codes and whether we should log the mismatch.
func compareExitCodes(localExitCodes []int32, remoteExitCodes []int32, numLocalReruns int, numRemoteReruns int) (lpb.DeterminismStatus, bool) {
	totalReruns := numLocalReruns + numRemoteReruns
	localMismatches := len(localExitCodes)
	remoteMismatches := len(remoteExitCodes)

	if localMismatches > 1 || remoteMismatches > 1 {
		if localMismatches == 1 {
			return lpb.DeterminismStatus_REMOTE_NON_DETERMINISTIC, true
		}
		return lpb.DeterminismStatus_NON_DETERMINISTIC, true
	}

	deterministicMismatch := localMismatches == 1 && remoteMismatches == 1 && localExitCodes[0] != remoteExitCodes[0] && totalReruns > 2
	localDeterministic := localMismatches == 1 && numRemoteReruns == 0
	remoteDeterministic := remoteMismatches == 1 && numLocalReruns == 0
	deterministic := localMismatches == 1 && remoteMismatches == 1 && localExitCodes[0] == remoteExitCodes[0]

	if localDeterministic || remoteDeterministic || deterministic || deterministicMismatch {
		return lpb.DeterminismStatus_DETERMINISTIC, deterministicMismatch
	}

	return lpb.DeterminismStatus_UNKNOWN, true
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

func dedupExitCodes(list []int32) []int32 {
	var res []int32
	seen := make(map[int32]bool)

	for _, n := range list {
		if _, ok := seen[n]; !ok {
			seen[n] = true
			res = append(res, n)
		}
	}

	return res
}
