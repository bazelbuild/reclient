// Copyright 2026 Google LLC
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

//go:build !windows

package ipc

import (
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestListen_RejectsNonUnixAddress(t *testing.T) {
	tests := []struct {
		name string
		addr string
	}{
		{"empty", ""},
		{"empty_unix_path", "unix://"},
		{"loopback_ip", "127.0.0.1:0"},
		{"localhost", "localhost:8000"},
		{"windows_pipe", "pipe://reproxy.ipc"}, // pipe:// is only supported on Windows (listen_windows.go), not POSIX.
	}

	const wantErr = "only unix:///path addresses are supported"
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l, err := Listen(tc.addr)
			if err == nil {
				l.Close()
				t.Fatalf("Listen(%q) succeeded, want error containing %q", tc.addr, wantErr)
			}
			if !strings.Contains(err.Error(), wantErr) {
				t.Errorf("Listen(%q) error = %q, want substring %q", tc.addr, err.Error(), wantErr)
			}
		})
	}
}

func TestListen_UnixSocketPermissionsAndDial(t *testing.T) {
	// Use os.MkdirTemp("/tmp", ...) instead of t.TempDir() because macOS
	// TMPDIR paths (/var/folders/...) are often 70+ chars and can exceed the
	// 104-byte Unix socket limit when combined with deep test names.
	sockPath := shortSocketPath(t, "reproxy.sock")
	addr := "unix://" + sockPath

	l, err := Listen(addr)
	if err != nil {
		t.Fatalf("Listen(%q) unexpected error: %v", addr, err)
	}
	t.Cleanup(func() { l.Close() })

	fi, err := os.Stat(sockPath)
	if err != nil {
		t.Fatalf("os.Stat(%q) failed: %v", sockPath, err)
	}
	if fi.Mode().Type() != fs.ModeSocket {
		t.Errorf("file %q mode type = %v, want socket", sockPath, fi.Mode().Type())
	}
	if got, want := fi.Mode().Perm(), fs.FileMode(0o600); got != want {
		t.Errorf("socket %q permissions = %04o, want %04o", sockPath, got, want)
	}

	// Verify the listener actually accepts connections.
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("net.Dial(unix, %q) failed: %v", sockPath, err)
	}
	conn.Close()
}

func TestListen_PermissiveUmask(t *testing.T) {
	origMask := syscall.Umask(0)
	t.Cleanup(func() { syscall.Umask(origMask) })

	sockPath := shortSocketPath(t, "umask.sock")
	l, err := Listen("unix://" + sockPath)
	if err != nil {
		t.Fatalf("Listen(%q) unexpected error: %v", sockPath, err)
	}
	t.Cleanup(func() { l.Close() })

	// Verify Listen restored the caller's previous umask (0).
	if got := syscall.Umask(origMask); got != 0 {
		t.Errorf("umask after Listen = %04o, want 0000", got)
	}

	fi, err := os.Stat(sockPath)
	if err != nil {
		t.Fatalf("os.Stat(%q) failed: %v", sockPath, err)
	}
	if got, want := fi.Mode().Perm(), fs.FileMode(0o600); got != want {
		t.Errorf("socket %q permissions under 0000 umask = %04o, want %04o", sockPath, got, want)
	}
}

func TestListen_ReplacesStaleSocket(t *testing.T) {
	sockPath := shortSocketPath(t, "stale.sock")
	if err := os.WriteFile(sockPath, []byte("stale"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) failed: %v", sockPath, err)
	}

	l, err := Listen("unix://" + sockPath)
	if err != nil {
		t.Fatalf("Listen on stale path %q failed: %v", sockPath, err)
	}
	t.Cleanup(func() { l.Close() })

	fi, err := os.Stat(sockPath)
	if err != nil {
		t.Fatalf("os.Stat(%q) failed: %v", sockPath, err)
	}
	if fi.Mode().Type() != fs.ModeSocket {
		t.Errorf("stale file was not replaced with socket; got type %v", fi.Mode().Type())
	}
}

func TestListen_FailsOnNonEmptyDirectory(t *testing.T) {
	// Ensures os.Remove is used rather than os.RemoveAll so accidental
	// directory paths aren't wiped out.
	dir := shortSocketPath(t, "not-a-socket-dir")
	if err := os.MkdirAll(filepath.Join(dir, "keep"), 0o700); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	l, err := Listen("unix://" + dir)
	if err == nil {
		l.Close()
		t.Fatalf("Listen(%q) succeeded on non-empty directory, want error", dir)
	}
}

func TestListen_UnixSocketPathTooLong(t *testing.T) {
	longPath := filepath.Join(t.TempDir(), strings.Repeat("a", 110)+".sock")
	addr := "unix://" + longPath

	l, err := Listen(addr)
	if err == nil {
		l.Close()
		t.Fatalf("Listen(%q) succeeded, want error for >108 byte socket path", addr)
	}
	const wantErr = "likely exceeds OS Unix socket length limit"
	if !strings.Contains(err.Error(), wantErr) {
		t.Errorf("Listen(%q) error = %q, want substring %q", addr, err.Error(), wantErr)
	}
}

// shortSocketPath creates a temporary directory directly under /tmp (rather
// than TMPDIR) so valid socket tests never trip the 104-byte macOS limit.
func shortSocketPath(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ipc-*")
	if err != nil {
		// Fall back to default OS temp dir on platforms without /tmp (e.g., Windows).
		dir = t.TempDir()
	} else {
		t.Cleanup(func() { os.RemoveAll(dir) })
	}
	return filepath.Join(dir, name)
}
