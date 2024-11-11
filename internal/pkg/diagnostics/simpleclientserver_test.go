package diagnostics

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCheckUDSAddrWorks(t *testing.T) {
	var addr string
	if runtime.GOOS == "windows" {
		addr = "unix:" + filepath.Join(os.TempDir(), "test.sock")
	} else {
		addr = "unix://" + filepath.Join(os.TempDir(), "test.sock")
	}

	if err := CheckUDSAddrWorks(context.Background(), addr); err != nil {
		t.Errorf("CheckUDSAddrWorks(%v) failed: %v", addr, err)
	}
}
func TestCheckUDSAddrWorks_InvalidAddress(t *testing.T) {
	var addr string
	if runtime.GOOS == "windows" {
		addr = "unix:X:\\tmp\\test.sock"
	} else {
		addr = "unix:///rooooot/test.sock"
	}

	if err := CheckUDSAddrWorks(context.Background(), addr); err == nil {
		t.Errorf("CheckUDSAddrWorks(%v) expected error but succeeded", addr)
	}
}
