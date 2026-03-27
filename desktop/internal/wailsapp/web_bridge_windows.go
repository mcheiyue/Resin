//go:build windows

package wailsapp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Resinat/Resin/desktop/internal/configstore"
)

const (
	desktopWebUIBaseRoute = "/ui/"
	desktopWebStatusRoute = "/ui/desktop"
	desktopBootstrapJSKey = "__RESIN_DESKTOP_BOOTSTRAP__"
)

type DesktopBootstrap struct {
	Desktop bool   `json:"desktop"`
	Token   string `json:"token"`
}

type DesktopWebBridge struct {
	bootstrap DesktopBootstrap
}

func NewDesktopWebBridge(bootstrap *configstore.BootstrapResult) (*DesktopWebBridge, error) {
	if bootstrap == nil {
		return nil, fmt.Errorf("bootstrap result is required")
	}

	token := strings.TrimSpace(bootstrap.Secrets.AdminToken)
	if token == "" {
		return nil, fmt.Errorf("desktop web bridge requires a non-empty admin session token")
	}

	return &DesktopWebBridge{
		bootstrap: DesktopBootstrap{
			Desktop: true,
			Token:   token,
		},
	}, nil
}

func (b *DesktopWebBridge) Bootstrap() DesktopBootstrap {
	if b == nil {
		return DesktopBootstrap{}
	}
	return b.bootstrap
}

func (b *DesktopWebBridge) BootstrapScript() (string, error) {
	if b == nil {
		return "", fmt.Errorf("desktop web bridge is nil")
	}

	payload, err := json.Marshal(b.bootstrap)
	if err != nil {
		return "", fmt.Errorf("marshal desktop bootstrap payload: %w", err)
	}

	return fmt.Sprintf("window.%s = %s;", desktopBootstrapJSKey, string(payload)), nil
}

func (b *DesktopWebBridge) WebUIBaseRoute() string {
	return desktopWebUIBaseRoute
}

func (b *DesktopWebBridge) DesktopStatusRoute() string {
	return desktopWebStatusRoute
}
