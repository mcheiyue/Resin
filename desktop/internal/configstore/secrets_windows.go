//go:build windows

package configstore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	crypt32                    = syscall.NewLazyDLL("crypt32.dll")
	kernel32                   = syscall.NewLazyDLL("kernel32.dll")
	procCryptProtectData       = crypt32.NewProc("CryptProtectData")
	procCryptUnprotectData     = crypt32.NewProc("CryptUnprotectData")
	procLocalFree              = kernel32.NewProc("LocalFree")
	procGetLastError           = kernel32.NewProc("GetLastError")
	protectedSecretsFileHeader = []byte("RESIN-DPAPI-SECRETS-V1\n")
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

type secretMaterial struct {
	AdminToken string `json:"admin_token"`
	ProxyToken string `json:"proxy_token"`
}

func loadOrCreateProtectedSecrets(secretsPath string) (Secrets, error) {
	secrets, err := loadProtectedSecrets(secretsPath)
	if err == nil {
		return secrets, nil
	}
	if !os.IsNotExist(err) {
		return Secrets{}, err
	}

	adminToken, err := generateToken()
	if err != nil {
		return Secrets{}, fmt.Errorf("generate admin token: %w", err)
	}
	proxyToken, err := generateToken()
	if err != nil {
		return Secrets{}, fmt.Errorf("generate proxy token: %w", err)
	}

	secrets = Secrets{
		AdminToken: adminToken,
		ProxyToken: proxyToken,
	}
	if err := saveProtectedSecrets(secretsPath, secrets); err != nil {
		return Secrets{}, err
	}
	return secrets, nil
}

func loadProtectedSecrets(secretsPath string) (Secrets, error) {
	encoded, err := os.ReadFile(secretsPath)
	if err != nil {
		return Secrets{}, err
	}
	if !bytes.HasPrefix(encoded, protectedSecretsFileHeader) {
		return Secrets{}, fmt.Errorf("unexpected secrets file header")
	}

	payload, err := dpapiUnprotect(encoded[len(protectedSecretsFileHeader):])
	if err != nil {
		return Secrets{}, fmt.Errorf("unprotect secrets payload: %w", err)
	}

	var material secretMaterial
	if err := json.Unmarshal(payload, &material); err != nil {
		return Secrets{}, fmt.Errorf("decode secrets payload: %w", err)
	}
	if material.AdminToken == "" || material.ProxyToken == "" {
		return Secrets{}, fmt.Errorf("protected secrets payload is incomplete")
	}
	return Secrets{
		AdminToken: material.AdminToken,
		ProxyToken: material.ProxyToken,
	}, nil
}

func saveProtectedSecrets(secretsPath string, secrets Secrets) error {
	payload, err := json.Marshal(secretMaterial{
		AdminToken: secrets.AdminToken,
		ProxyToken: secrets.ProxyToken,
	})
	if err != nil {
		return fmt.Errorf("encode secrets payload: %w", err)
	}

	protected, err := dpapiProtect(payload)
	if err != nil {
		return fmt.Errorf("protect secrets payload: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(secretsPath), 0o755); err != nil {
		return fmt.Errorf("create secrets directory: %w", err)
	}

	tempFile, err := os.CreateTemp(filepath.Dir(secretsPath), "resin-secrets-*.tmp")
	if err != nil {
		return fmt.Errorf("create protected secrets temp file: %w", err)
	}
	tempPath := tempFile.Name()
	keepTemp := false
	defer func() {
		_ = tempFile.Close()
		if !keepTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if err := tempFile.Chmod(0o600); err != nil {
		return fmt.Errorf("chmod protected secrets temp file: %w", err)
	}
	if _, err := tempFile.Write(protectedSecretsFileHeader); err != nil {
		return fmt.Errorf("write protected secrets header: %w", err)
	}
	if _, err := tempFile.Write(protected); err != nil {
		return fmt.Errorf("write protected secrets payload: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		return fmt.Errorf("sync protected secrets temp file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close protected secrets temp file: %w", err)
	}
	if err := os.Rename(tempPath, secretsPath); err != nil {
		return fmt.Errorf("move protected secrets file into place: %w", err)
	}
	keepTemp = true
	return nil
}

func dpapiProtect(plain []byte) ([]byte, error) {
	if len(plain) == 0 {
		return nil, fmt.Errorf("plain payload must not be empty")
	}

	in := newDataBlob(plain)
	var out dataBlob
	r1, _, _ := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0,
		0,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&out)),
	)
	if r1 == 0 {
		return nil, fmt.Errorf("CryptProtectData failed: %w", syscall.Errno(lastErrno()))
	}
	defer localFree(out.pbData)

	return blobBytes(out), nil
}

func dpapiUnprotect(protected []byte) ([]byte, error) {
	if len(protected) == 0 {
		return nil, fmt.Errorf("protected payload must not be empty")
	}

	in := newDataBlob(protected)
	var out dataBlob
	r1, _, _ := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0,
		0,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&out)),
	)
	if r1 == 0 {
		return nil, fmt.Errorf("CryptUnprotectData failed: %w", syscall.Errno(lastErrno()))
	}
	defer localFree(out.pbData)

	return blobBytes(out), nil
}

func newDataBlob(src []byte) dataBlob {
	if len(src) == 0 {
		return dataBlob{}
	}
	return dataBlob{
		cbData: uint32(len(src)),
		pbData: &src[0],
	}
}

func blobBytes(blob dataBlob) []byte {
	if blob.cbData == 0 || blob.pbData == nil {
		return nil
	}
	data := unsafe.Slice(blob.pbData, blob.cbData)
	clone := make([]byte, len(data))
	copy(clone, data)
	return clone
}

func localFree(ptr *byte) {
	if ptr == nil {
		return
	}
	_, _, _ = procLocalFree.Call(uintptr(unsafe.Pointer(ptr)))
}

func lastErrno() uintptr {
	r1, _, _ := procGetLastError.Call()
	return r1
}
