// Package diagnostics is used to provide diagnostic functionality to triage
// problems with reclient during failure scenarios.
package diagnostics

import (
	"context"

	log "github.com/golang/glog"
)

// DiagnosticInputs struct holds key state necessary for diagnostics to run.
type DiagnosticInputs struct {
	UDSAddr string
}

// Run runs the diagnostics.
func Run(ctx context.Context, in *DiagnosticInputs) {
	if err := CheckUDSAddrWorks(ctx, in.UDSAddr); err != nil {
		log.Errorf("DIAGNOSTIC_ERROR: UDS address check for %v had errors: %v", in.UDSAddr, err)
	} else {
		log.Infof("DIAGNOSTIC_SUCCESS: UDS address %v works with toy RPC server", in.UDSAddr)
	}
}
