//go:build windows

package wailsapp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type RuntimeBindings struct {
	mu            sync.RWMutex
	ctx           context.Context
	quitRequested atomic.Bool
}

func NewRuntimeBindings() *RuntimeBindings {
	return &RuntimeBindings{}
}

func (b *RuntimeBindings) BindContext(ctx context.Context) {
	if b == nil || ctx == nil {
		return
	}
	b.mu.Lock()
	b.ctx = ctx
	b.mu.Unlock()
}

func (b *RuntimeBindings) Context() (context.Context, error) {
	if b == nil {
		return nil, fmt.Errorf("runtime bindings is nil")
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.ctx == nil {
		return nil, fmt.Errorf("wails runtime context is not bound")
	}
	return b.ctx, nil
}

func (b *RuntimeBindings) RequestQuit() {
	if b != nil {
		b.quitRequested.Store(true)
	}
}

func (b *RuntimeBindings) IsQuitRequested() bool {
	if b == nil {
		return false
	}
	return b.quitRequested.Load()
}

type WailsWindowAdapter struct {
	bindings *RuntimeBindings
}

func NewWailsWindowAdapter(bindings *RuntimeBindings) *WailsWindowAdapter {
	return &WailsWindowAdapter{bindings: bindings}
}

func (a *WailsWindowAdapter) Show() error {
	ctx, err := a.bindings.Context()
	if err != nil {
		return err
	}
	runtime.WindowShow(ctx)
	return nil
}

func (a *WailsWindowAdapter) Hide() error {
	ctx, err := a.bindings.Context()
	if err != nil {
		return err
	}
	runtime.WindowHide(ctx)
	return nil
}

type ExplorerPathOpener struct{}

func NewExplorerPathOpener() *ExplorerPathOpener {
	return &ExplorerPathOpener{}
}

func (o *ExplorerPathOpener) Open(path string) error {
	cmd := exec.Command("explorer.exe", path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open path in explorer: %w", err)
	}
	return nil
}

type WailsShellRuntime struct {
	bindings *RuntimeBindings
}

func NewWailsShellRuntime(bindings *RuntimeBindings) *WailsShellRuntime {
	return &WailsShellRuntime{bindings: bindings}
}

func (r *WailsShellRuntime) Exit() error {
	ctx, err := r.bindings.Context()
	if err != nil {
		return err
	}
	r.bindings.RequestQuit()
	runtime.Quit(ctx)
	return nil
}

func (r *WailsShellRuntime) ValidateFixedRuntime(rootDir string) error {
	return validateFixedRuntime(rootDir)
}

func validateFixedRuntime(rootDir string) error {
	runtimePath := filepath.Join(rootDir, filepath.FromSlash(fixedRuntimeExecutableRelative))
	info, err := os.Stat(runtimePath)
	if err != nil {
		return fmt.Errorf("stat fixed runtime executable %q: %w", runtimePath, err)
	}
	if info.IsDir() {
		return fmt.Errorf("fixed runtime executable %q is a directory", runtimePath)
	}
	if info.Size() <= 0 {
		return fmt.Errorf("fixed runtime executable %q is empty", runtimePath)
	}
	return nil
}
