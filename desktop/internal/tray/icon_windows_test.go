//go:build windows

package tray

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestTrayIconMatchesCanonicalWindowsIcon(t *testing.T) {
	t.Parallel()

	canonicalPath := filepath.Join("..", "..", "build", "windows", "icon.ico")
	canonicalBytes, err := os.ReadFile(canonicalPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v", canonicalPath, err)
	}
	if len(canonicalBytes) == 0 {
		t.Fatalf("canonical icon %q is empty", canonicalPath)
	}
	if len(resinTrayIcon) == 0 {
		t.Fatal("embedded tray icon should not be empty")
	}
	if !bytes.Equal(canonicalBytes, resinTrayIcon) {
		t.Fatalf("embedded tray icon differs from canonical windows icon %q", canonicalPath)
	}
}
