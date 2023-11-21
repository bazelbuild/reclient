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

// Package localresources ensures locally executed tasks have enough resources to run.
package localresources

import (
	"context"
	"runtime"

	"golang.org/x/sync/semaphore"

	log "github.com/golang/glog"
)

// Manager is the manager of local resources.
type Manager struct {
	cpus *semaphore.Weighted
	ram  *semaphore.Weighted

	totalCPUs   int64
	totalRAMMBs int64
}

// NewDefaultManager retrieves a Manager with default local resources.
func NewDefaultManager() *Manager {
	numCPU := int64(runtime.NumCPU())
	ramMBs := TotalRAMMBs()
	return NewManager(numCPU, ramMBs)
}

// NewFractionalDefaultManager retrieves a Manager with the given fraction of default local
// resources.
func NewFractionalDefaultManager(fraction float64) *Manager {
	numCPU := int64(float64(runtime.NumCPU()) * fraction)
	ramMBs := int64(float64(TotalRAMMBs()) * fraction)
	return NewManager(numCPU, ramMBs)
}

// NewManager is used to initialize the manager with non default local resources.
func NewManager(cpus, ramMBs int64) *Manager {
	return &Manager{
		cpus:        semaphore.NewWeighted(max(1, cpus)),
		ram:         semaphore.NewWeighted(max(1, ramMBs)),
		totalCPUs:   max(1, cpus),
		totalRAMMBs: max(1, ramMBs),
	}
}

// Lock locks the desired resources and returns a function to release them.
func (m *Manager) Lock(ctx context.Context, cpus, ramMBs int64) (func(), error) {
	if m == nil {
		return func() {}, nil
	}
	if cpus > m.totalCPUs || ramMBs > m.totalRAMMBs {
		log.Warningf("Capping request to available system resources, cpu-max=%v(req=%v), ramMBs-max=%v(req=%v)", m.totalCPUs, cpus, m.totalRAMMBs, ramMBs)
		cpus = min(cpus, m.totalCPUs)
		ramMBs = min(ramMBs, m.totalRAMMBs)
	}
	if err := m.cpus.Acquire(ctx, cpus); err != nil {
		return nil, err
	}
	if err := m.ram.Acquire(ctx, ramMBs); err != nil {
		m.cpus.Release(cpus)
		return nil, err
	}
	return func() {
		m.cpus.Release(cpus)
		m.ram.Release(ramMBs)
	}, nil
}

func min(a int64, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
