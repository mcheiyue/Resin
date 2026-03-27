//go:build windows

package wailsapp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateFixedRuntime_AllowsLitePackageWithoutBundledRuntime(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	if err := validateFixedRuntime(rootDir); err != nil {
		t.Fatalf("validateFixedRuntime() error = %v, want nil for lite package without bundled runtime", err)
	}
}

func TestValidateFixedRuntime_RejectsInvalidBundledExecutable(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	runtimePath := filepath.Join(rootDir, filepath.FromSlash(fixedRuntimeExecutableRelative))
	if err := os.MkdirAll(filepath.Dir(runtimePath), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(runtimePath, nil, 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	if err := validateFixedRuntime(rootDir); err == nil {
		t.Fatal("validateFixedRuntime() error = nil, want invalid bundled runtime error")
	}
}
