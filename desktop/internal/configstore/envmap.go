//go:build windows

package configstore

import "fmt"

const (
	envAuthVersion   = "RESIN_AUTH_VERSION"
	envAdminToken    = "RESIN_ADMIN_TOKEN"
	envProxyToken    = "RESIN_PROXY_TOKEN"
	envListenAddress = "RESIN_LISTEN_ADDRESS"
	envPort          = "RESIN_PORT"
	envStateDir      = "RESIN_STATE_DIR"
	envCacheDir      = "RESIN_CACHE_DIR"
	envLogDir        = "RESIN_LOG_DIR"

	fixedAuthVersion   = "V1"
	fixedListenAddress = "127.0.0.1"
	fixedPort          = 2260
)

func (r BootstrapResult) EnvMap() map[string]string {
	return map[string]string{
		envAuthVersion:   fixedAuthVersion,
		envAdminToken:    r.Secrets.AdminToken,
		envProxyToken:    r.Secrets.ProxyToken,
		envListenAddress: fixedListenAddress,
		envPort:          fmt.Sprintf("%d", fixedPort),
		envStateDir:      r.Layout.StateDir,
		envCacheDir:      r.Layout.CacheDir,
		envLogDir:        r.Layout.LogDir,
	}
}

func (r BootstrapResult) EnvList() []string {
	envMap := r.EnvMap()
	return []string{
		envAuthVersion + "=" + envMap[envAuthVersion],
		envAdminToken + "=" + envMap[envAdminToken],
		envProxyToken + "=" + envMap[envProxyToken],
		envListenAddress + "=" + envMap[envListenAddress],
		envPort + "=" + envMap[envPort],
		envStateDir + "=" + envMap[envStateDir],
		envCacheDir + "=" + envMap[envCacheDir],
		envLogDir + "=" + envMap[envLogDir],
	}
}
