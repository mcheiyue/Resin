//go:build windows

package configstore

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	ErrorCodeConfigRootNotWritable = "CONFIG_ROOT_NOT_WRITABLE"

	portableDataDirRelative    = "data"
	portableStateDirRelative   = "data/state"
	portableCacheDirRelative   = "data/cache"
	portableLogDirRelative     = "data/logs"
	portableDesktopDirRelative = "data/desktop"

	writeProbeFileName = ".resin-write-probe"
	secretsFileName    = "secrets.dpapi"

	tokenEntropyBytes = 32
)

type BootstrapError struct {
	Code string
	Path string
	Err  error
}

func (e *BootstrapError) Error() string {
	if e == nil {
		return ""
	}
	if e.Path == "" {
		return fmt.Sprintf("%s: %v", e.Code, e.Err)
	}
	return fmt.Sprintf("%s: %s: %v", e.Code, e.Path, e.Err)
}

func (e *BootstrapError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func ErrorCodeOf(err error) string {
	var bootstrapErr *BootstrapError
	if errors.As(err, &bootstrapErr) {
		return bootstrapErr.Code
	}
	return ""
}

type Layout struct {
	RootDir     string
	DataDir     string
	StateDir    string
	CacheDir    string
	LogDir      string
	DesktopDir  string
	SecretsPath string
}

type Secrets struct {
	AdminToken string
	ProxyToken string
}

type BootstrapResult struct {
	Layout  Layout
	Secrets Secrets
}

func Bootstrap(rootDir string) (*BootstrapResult, error) {
	layout, err := resolveLayout(rootDir)
	if err != nil {
		return nil, err
	}

	if err := ensureRootWritable(layout.RootDir); err != nil {
		return nil, &BootstrapError{
			Code: ErrorCodeConfigRootNotWritable,
			Path: layout.RootDir,
			Err:  err,
		}
	}

	for _, dir := range []string{layout.StateDir, layout.CacheDir, layout.LogDir, layout.DesktopDir} {
		if err := ensureWritableDirectory(dir); err != nil {
			return nil, fmt.Errorf("ensure writable portable directory %s: %w", dir, err)
		}
	}

	secrets, err := loadOrCreateProtectedSecrets(layout.SecretsPath)
	if err != nil {
		return nil, fmt.Errorf("bootstrap protected secrets: %w", err)
	}

	return &BootstrapResult{
		Layout:  layout,
		Secrets: secrets,
	}, nil
}

func resolveLayout(rootDir string) (Layout, error) {
	cleanRoot := strings.TrimSpace(rootDir)
	if cleanRoot == "" {
		return Layout{}, fmt.Errorf("root directory must not be empty")
	}

	absoluteRoot, err := filepath.Abs(cleanRoot)
	if err != nil {
		return Layout{}, fmt.Errorf("resolve root directory: %w", err)
	}

	return Layout{
		RootDir:     absoluteRoot,
		DataDir:     filepath.Join(absoluteRoot, filepath.FromSlash(portableDataDirRelative)),
		StateDir:    filepath.Join(absoluteRoot, filepath.FromSlash(portableStateDirRelative)),
		CacheDir:    filepath.Join(absoluteRoot, filepath.FromSlash(portableCacheDirRelative)),
		LogDir:      filepath.Join(absoluteRoot, filepath.FromSlash(portableLogDirRelative)),
		DesktopDir:  filepath.Join(absoluteRoot, filepath.FromSlash(portableDesktopDirRelative)),
		SecretsPath: filepath.Join(absoluteRoot, filepath.FromSlash(portableDesktopDirRelative), secretsFileName),
	}, nil
}

func ensureRootWritable(rootDir string) error {
	info, err := os.Stat(rootDir)
	if err != nil {
		return fmt.Errorf("stat root directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("root path is not a directory")
	}
	if err := probeWritable(rootDir); err != nil {
		return fmt.Errorf("probe root directory: %w", err)
	}
	return nil
}

func ensureWritableDirectory(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	if err := probeWritable(dir); err != nil {
		return fmt.Errorf("probe directory: %w", err)
	}
	return nil
}

func probeWritable(dir string) error {
	probePath := filepath.Join(dir, writeProbeFileName)
	file, err := os.OpenFile(probePath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	removeProbe := true
	defer func() {
		_ = file.Close()
		if removeProbe {
			_ = os.Remove(probePath)
		}
	}()

	if _, err := file.Write([]byte("ok")); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	removeProbe = true
	return nil
}

func generateToken() (string, error) {
	raw := make([]byte, tokenEntropyBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("read secure random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
