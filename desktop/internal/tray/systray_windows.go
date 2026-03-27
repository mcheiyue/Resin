//go:build windows

package tray

import (
	"fmt"
	"sync"
	"time"

	"github.com/getlantern/systray"
)

const trayReadyTimeout = 5 * time.Second

type systrayBackend struct {
	once sync.Once
}

func newSystrayBackend() Backend {
	return &systrayBackend{}
}

func (b *systrayBackend) Start(menu Menu, handler Handler) error {
	if handler == nil {
		return fmt.Errorf("tray handler is required")
	}

	readyCh := make(chan struct{})
	startErrCh := make(chan error, 1)

	b.once.Do(func() {
		go systray.Run(func() {
			systray.SetTitle("Resin")
			systray.SetTooltip("Resin Desktop")
			for _, item := range menu.Items {
				menuItem := systray.AddMenuItem(item.Label, item.Label)
				go func(actionID ActionID, clickedCh <-chan struct{}) {
					for range clickedCh {
						_ = handler(actionID)
					}
				}(item.ID, menuItem.ClickedCh)
			}
			close(readyCh)
		}, func() {})
	})

	select {
	case <-readyCh:
		return nil
	case err := <-startErrCh:
		return err
	case <-time.After(trayReadyTimeout):
		return fmt.Errorf("systray backend did not become ready within %s", trayReadyTimeout)
	}
}
