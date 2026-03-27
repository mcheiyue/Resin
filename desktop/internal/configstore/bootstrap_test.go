//go:build windows

package configstore

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigBootstrap_PortableLayout(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	result, err := Bootstrap(rootDir)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	if result.Layout.RootDir != rootDir {
		t.Fatalf("root dir = %q, want %q", result.Layout.RootDir, rootDir)
	}

	expected := map[string]string{
		"state":   filepath.Join(rootDir, filepath.FromSlash(portableStateDirRelative)),
		"cache":   filepath.Join(rootDir, filepath.FromSlash(portableCacheDirRelative)),
		"logs":    filepath.Join(rootDir, filepath.FromSlash(portableLogDirRelative)),
		"desktop": filepath.Join(rootDir, filepath.FromSlash(portableDesktopDirRelative)),
	}

	actual := map[string]string{
		"state":   result.Layout.StateDir,
		"cache":   result.Layout.CacheDir,
		"logs":    result.Layout.LogDir,
		"desktop": result.Layout.DesktopDir,
	}

	for name, path := range actual {
		if path != expected[name] {
			t.Fatalf("%s dir = %q, want %q", name, path, expected[name])
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s dir: %v", name, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s path is not a directory", name)
		}
	}

	if result.Layout.SecretsPath != filepath.Join(expected["desktop"], secretsFileName) {
		t.Fatalf("secrets path = %q, want %q", result.Layout.SecretsPath, filepath.Join(expected["desktop"], secretsFileName))
	}
}

func TestConfigBootstrap_GenerateSecrets(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	first, err := Bootstrap(rootDir)
	if err != nil {
		t.Fatalf("first Bootstrap() error = %v", err)
	}
	if first.Secrets.AdminToken == "" {
		t.Fatal("admin token should not be empty")
	}
	if first.Secrets.ProxyToken == "" {
		t.Fatal("proxy token should not be empty")
	}
	if first.Secrets.AdminToken == first.Secrets.ProxyToken {
		t.Fatal("admin token and proxy token should differ")
	}

	rawFile, err := os.ReadFile(first.Layout.SecretsPath)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	if !bytes.HasPrefix(rawFile, protectedSecretsFileHeader) {
		t.Fatalf("secrets file should start with protected header %q", protectedSecretsFileHeader)
	}
	if bytes.Contains(rawFile, []byte(first.Secrets.AdminToken)) {
		t.Fatal("admin token leaked into protected secrets file")
	}
	if bytes.Contains(rawFile, []byte(first.Secrets.ProxyToken)) {
		t.Fatal("proxy token leaked into protected secrets file")
	}

	loaded, err := loadProtectedSecrets(first.Layout.SecretsPath)
	if err != nil {
		t.Fatalf("loadProtectedSecrets() error = %v", err)
	}
	if loaded != first.Secrets {
		t.Fatalf("loaded secrets = %#v, want %#v", loaded, first.Secrets)
	}

	second, err := Bootstrap(rootDir)
	if err != nil {
		t.Fatalf("second Bootstrap() error = %v", err)
	}
	if second.Secrets != first.Secrets {
		t.Fatalf("second bootstrap secrets = %#v, want %#v", second.Secrets, first.Secrets)
	}
}

func TestConfigBootstrap_EnvMapping(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	result, err := Bootstrap(rootDir)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	envMap := result.EnvMap()
	if len(envMap) != 8 {
		t.Fatalf("len(envMap) = %d, want 8", len(envMap))
	}

	expected := map[string]string{
		envAuthVersion:   fixedAuthVersion,
		envAdminToken:    result.Secrets.AdminToken,
		envProxyToken:    result.Secrets.ProxyToken,
		envListenAddress: fixedListenAddress,
		envPort:          "2260",
		envStateDir:      filepath.Join(rootDir, filepath.FromSlash(portableStateDirRelative)),
		envCacheDir:      filepath.Join(rootDir, filepath.FromSlash(portableCacheDirRelative)),
		envLogDir:        filepath.Join(rootDir, filepath.FromSlash(portableLogDirRelative)),
	}

	for key, want := range expected {
		if got := envMap[key]; got != want {
			t.Fatalf("envMap[%q] = %q, want %q", key, got, want)
		}
	}

	envList := result.EnvList()
	if len(envList) != 8 {
		t.Fatalf("len(envList) = %d, want 8", len(envList))
	}
	for _, entry := range envList {
		parts := bytes.SplitN([]byte(entry), []byte("="), 2)
		if len(parts) != 2 {
			t.Fatalf("invalid env entry %q", entry)
		}
		key := string(parts[0])
		value := string(parts[1])
		if expected[key] != value {
			t.Fatalf("env entry %q = %q, want %q", key, value, expected[key])
		}
	}
}

func TestConfigBootstrap_UnwritableRoot(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	conflictPath := filepath.Join(rootDir, writeProbeFileName)
	if err := os.Mkdir(conflictPath, 0o755); err != nil {
		t.Fatalf("create write probe conflict: %v", err)
	}

	_, err := Bootstrap(rootDir)
	if err == nil {
		t.Fatal("Bootstrap() error = nil, want non-nil")
	}
	if got := ErrorCodeOf(err); got != ErrorCodeConfigRootNotWritable {
		t.Fatalf("ErrorCodeOf(err) = %q, want %q (err=%v)", got, ErrorCodeConfigRootNotWritable, err)
	}
}
