package wailsapp

import (
	"context"
	"fmt"
	"sync"

	"github.com/Resinat/Resin/desktop/internal/configstore"
	"github.com/Resinat/Resin/desktop/internal/lifecycle"
	"github.com/Resinat/Resin/desktop/internal/singleinstance"
	"github.com/Resinat/Resin/desktop/internal/tray"
)

type Shell struct {
	Name        string
	FrontendDir string
}

type AppConfig struct {
	RootDir           string
	Bootstrap         *configstore.BootstrapResult
	BootstrapErr      error
	Bootstrapper      Bootstrapper
	Supervisor        CoreSupervisor
	SupervisorFactory SupervisorFactory
	WizardRequired    bool
	TrayManager       TrayManager
	Window            Window
	PathOpener        PathOpener
	Runtime           ShellRuntime
	Bindings          *RuntimeBindings
}

type App struct {
	shell           Shell
	bindings        *RuntimeBindings
	lifecycle       *ShellLifecycle
	startupMu       sync.RWMutex
	startupInFlight bool
}

func NewShell() Shell {
	return Shell{
		Name:        "resin-desktop",
		FrontendDir: "frontend",
	}
}

func NewApp(config AppConfig) (*App, error) {
	shell := NewShell()
	bindings := config.Bindings
	if bindings == nil {
		bindings = NewRuntimeBindings()
	}

	trayManager := config.TrayManager
	if trayManager == nil {
		trayManager = tray.NewManager()
	}

	window := config.Window
	if window == nil {
		window = NewWailsWindowAdapter(bindings)
	}

	pathOpener := config.PathOpener
	if pathOpener == nil {
		pathOpener = NewExplorerPathOpener()
	}

	runtimeAdapter := config.Runtime
	if runtimeAdapter == nil {
		runtimeAdapter = NewWailsShellRuntime(bindings)
	}

	shellLifecycle, err := NewShellLifecycle(ShellLifecycleConfig{
		RootDir:           config.RootDir,
		Bootstrap:         config.Bootstrap,
		BootstrapErr:      config.BootstrapErr,
		Bootstrapper:      config.Bootstrapper,
		Supervisor:        config.Supervisor,
		SupervisorFactory: config.SupervisorFactory,
		Window:            window,
		Tray:              trayManager,
		PathOpener:        pathOpener,
		Runtime:           runtimeAdapter,
		WizardRequired:    config.WizardRequired,
	})
	if err != nil {
		return nil, err
	}

	return &App{
		shell:     shell,
		bindings:  bindings,
		lifecycle: shellLifecycle,
	}, nil
}

func (a *App) Shell() Shell {
	if a == nil {
		return NewShell()
	}
	return a.shell
}

func (a *App) Startup(ctx context.Context) error {
	if a == nil {
		return fmt.Errorf("wails app is nil")
	}
	a.bindings.BindContext(ctx)
	a.setStartupInFlight(true)
	go func() {
		defer a.setStartupInFlight(false)
		_ = a.lifecycle.Start(ctx)
	}()
	return nil
}

func (a *App) BeforeClose(ctx context.Context) bool {
	if a == nil {
		return false
	}
	a.bindings.BindContext(ctx)
	return a.handleCloseRequest(ctx, a.lifecycle.HandleWindowCloseRequested)
}

func (a *App) HandleAltF4(ctx context.Context) bool {
	if a == nil {
		return false
	}
	a.bindings.BindContext(ctx)
	return a.handleCloseRequest(ctx, a.lifecycle.HandleAltF4)
}

func (a *App) HandleTaskbarClose(ctx context.Context) bool {
	if a == nil {
		return false
	}
	a.bindings.BindContext(ctx)
	return a.handleCloseRequest(ctx, a.lifecycle.HandleTaskbarCloseRequested)
}

func (a *App) AttachSingleInstanceSignals(signals <-chan singleinstance.Signal) {
	if a == nil || signals == nil {
		return
	}
	go func() {
		for signal := range signals {
			_ = a.lifecycle.HandleSingleInstanceSignal(signal)
		}
	}()
}

func (a *App) State() lifecycle.State {
	if a == nil {
		return lifecycle.StateError
	}
	if a.startupPending() {
		if progress := a.lifecycle.ProgressView(); progress != nil {
			return progress.State
		}
		if state := a.lifecycle.State(); state != lifecycle.StateBooting {
			return state
		}
		return lifecycle.StateBooting
	}
	return a.lifecycle.State()
}

func (a *App) ShowMainWindow(ctx context.Context) error {
	if a == nil {
		return fmt.Errorf("wails app is nil")
	}
	a.bindings.BindContext(ctx)
	return a.lifecycle.ShowMainWindow()
}

func (a *App) ShellViewModel() ShellViewModel {
	if a == nil {
		return ShellViewModel{State: lifecycle.StateError}
	}
	if a.startupPending() {
		if progress := a.lifecycle.ProgressView(); progress != nil {
			return ShellViewModel{State: progress.State, Progress: progress}
		}
		viewModel := a.lifecycle.ViewModel()
		if viewModel.State != lifecycle.StateBooting {
			return viewModel
		}
		return ShellViewModel{
			State: lifecycle.StateBooting,
			Progress: &ShellProgressViewModel{
				State:   lifecycle.StateBooting,
				Phase:   "booting-shell",
				Summary: "正在准备桌面壳",
				Detail:  "桌面窗口已经打开，正在初始化本地启动链路。",
			},
		}
	}
	return a.lifecycle.ViewModel()
}

func (a *App) DesktopAccessView() DesktopAccessView {
	if a == nil {
		return DesktopAccessView{}
	}
	return a.lifecycle.DesktopAccessView()
}

func (a *App) desktopWebBridge() (*DesktopWebBridge, error) {
	if a == nil {
		return nil, fmt.Errorf("wails app is nil")
	}
	return a.lifecycle.DesktopWebBridge()
}

func (a *App) WebUIBaseRoute() (string, error) {
	bridge, err := a.desktopWebBridge()
	if err != nil {
		return "", err
	}
	return bridge.WebUIBaseRoute(), nil
}

func (a *App) DesktopStatusRoute() (string, error) {
	bridge, err := a.desktopWebBridge()
	if err != nil {
		return "", err
	}
	return bridge.DesktopStatusRoute(), nil
}

func (a *App) BootstrapScript() (string, error) {
	bridge, err := a.desktopWebBridge()
	if err != nil {
		return "", err
	}
	return bridge.BootstrapScript()
}

func (a *App) ContinueFirstRun() error {
	if a == nil {
		return fmt.Errorf("wails app is nil")
	}
	return a.lifecycle.ContinueFromWizard(a.operationContext())
}

func (a *App) SetLaunchPort(port int) error {
	if a == nil {
		return fmt.Errorf("wails app is nil")
	}
	return a.lifecycle.SetLaunchPort(port)
}

func (a *App) RetryStartup() error {
	if a == nil {
		return fmt.Errorf("wails app is nil")
	}
	return a.lifecycle.RetryStart(a.operationContext())
}

func (a *App) OpenLogDirectory() error {
	if a == nil {
		return fmt.Errorf("wails app is nil")
	}
	return a.lifecycle.OpenLogDirectory()
}

func (a *App) CopyDiagnostics() string {
	if a == nil {
		return ""
	}
	return a.lifecycle.CopyDiagnostics()
}

func (a *App) ProxyAccessToken() string {
	if a == nil {
		return ""
	}
	return a.lifecycle.ProxyAccessToken()
}

func (a *App) handleCloseRequest(ctx context.Context, fn func() (lifecycle.Action, error)) bool {
	if a.bindings.IsQuitRequested() {
		return false
	}
	action, err := fn()
	if err != nil {
		if a.shouldExplicitlyExitOnCloseError() {
			go func() {
				_ = a.lifecycle.ExplicitExit(ctx)
			}()
			return true
		}
		return true
	}
	return action == lifecycle.ActionHideToTray
}

func (a *App) shouldExplicitlyExitOnCloseError() bool {
	if a == nil || a.lifecycle == nil {
		return false
	}
	switch a.lifecycle.State() {
	case lifecycle.StateBooting, lifecycle.StateStartingCore, lifecycle.StateStopping, lifecycle.StateError:
		return true
	default:
		return false
	}
}

func (a *App) operationContext() context.Context {
	if a == nil || a.bindings == nil {
		return context.Background()
	}
	ctx, err := a.bindings.Context()
	if err != nil {
		return context.Background()
	}
	return ctx
}

func (a *App) setStartupInFlight(active bool) {
	a.startupMu.Lock()
	defer a.startupMu.Unlock()
	a.startupInFlight = active
}

func (a *App) startupPending() bool {
	a.startupMu.RLock()
	defer a.startupMu.RUnlock()
	return a.startupInFlight
}
