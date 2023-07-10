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

package localresources

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestLock_InsufficientSystemResources(t *testing.T) {
	ctx := context.Background()
	m := NewManager(1, 512)
	if _, err := m.Lock(ctx, 2, 1024); err != nil {
		t.Fatalf("Lock(%v, %v) returned err: %v", 2, 1024, err)
	}
}

func TestLock_ExpiredContext(t *testing.T) {
	ctx := context.Background()
	m := NewManager(1, 512)
	rel, err := m.Lock(ctx, 1, 512)
	if err != nil {
		t.Fatalf("Lock(%v, %v) returned err: %v", 1, 512, err)
	}
	defer rel()
	cCtx, cancel := context.WithCancel(ctx)
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		rel2, err := m.Lock(cCtx, 1, 256)
		if err == nil {
			t.Errorf("Lock(%v, %v) returned nil, want context canceled error", 1, 256)
			rel2()
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Lock(%v, %v) returned err: %v, want context canceled", 1, 256, err)
		}
	}()
	time.Sleep(time.Second)
	cancel()
	wg.Wait()
}
