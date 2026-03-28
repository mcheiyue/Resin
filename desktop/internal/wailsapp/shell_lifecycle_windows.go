//go:build windows

package wailsapp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Resinat/Resin/desktop/internal/configstore"
	"github.com/Resinat/Resin/desktop/internal/lifecycle"
	"github.com/Resinat/Resin/desktop/internal/singleinstance"
	"github.com/Resinat/Resin/desktop/internal/supervisor"
	"github.com/Resinat/Resin/desktop/internal/tray"
)

type Window interface {
	Show() error
	Hide() error
}

type PathOpener interface {
	Open(path string) error
}

type ShellRuntime interface {
	Exit() error
	ValidateFixedRuntime(rootDir string) error
}

type TrayManager interface {
	Init(menu tray.Menu, handler func(tray.ActionID) error) error
}

type CoreSupervisor interface {
	Start(ctx context.Context) (*supervisor.StartResult, error)
	Shutdown(ctx context.Context) (*supervisor.ShutdownResult, error)
}

type ShellLifecycleConfig struct {
	RootDir           string
	Bootstrap         *configstore.BootstrapResult
	BootstrapErr      error
	Bootstrapper      Bootstrapper
	Supervisor        CoreSupervisor
	SupervisorFactory SupervisorFactory
	Window            Window
	Tray              TrayManager
	PathOpener        PathOpener
	Runtime           ShellRuntime
	WizardRequired    bool
}

type ShellLifecycle struct {
	bootstrap         *configstore.BootstrapResult
	bootstrapErr      error
	webBridge         *DesktopWebBridge
	bootstrapper      Bootstrapper
	supervisor        CoreSupervisor
	supervisorFactory SupervisorFactory
	window            Window
	tray              TrayManager
	pathOpener        PathOpener
	runtime           ShellRuntime
	wizardRequired    bool
	wizardCompleted   bool
	trayReady         bool
	menu              tray.Menu
	machine           *lifecycle.Machine
	startResult       *supervisor.StartResult
	paths             ShellPaths
	launchConfig      ShellLaunchConfig
	supervisorConfig  ShellLaunchConfig
	wizard            *FirstRunWizardViewModel
	diagnostics       *DiagnosticsViewModel
}

func NewShellLifecycle(config ShellLifecycleConfig) (*ShellLifecycle, error) {
	paths, err := deriveShellPaths(config.RootDir, config.Bootstrap)
	if err != nil {
		return nil, err
	}
	if config.Window == nil {
		return nil, fmt.Errorf("window adapter is required")
	}
	if config.Tray == nil {
		return nil, fmt.Errorf("tray adapter is required")
	}
	if config.PathOpener == nil {
		return nil, fmt.Errorf("path opener is required")
	}
	if config.Runtime == nil {
		return nil, fmt.Errorf("shell runtime is required")
	}
	if config.Supervisor == nil && config.SupervisorFactory == nil {
		return nil, fmt.Errorf("supervisor or supervisor factory is required")
	}
	bootstrapper := config.Bootstrapper
	if bootstrapper == nil {
		bootstrapper = configstore.Bootstrap
	}

	return &ShellLifecycle{
		bootstrap:         config.Bootstrap,
		bootstrapErr:      config.BootstrapErr,
		bootstrapper:      bootstrapper,
		supervisor:        config.Supervisor,
		supervisorFactory: config.SupervisorFactory,
		window:            config.Window,
		tray:              config.Tray,
		pathOpener:        config.PathOpener,
		runtime:           config.Runtime,
		wizardRequired:    config.WizardRequired,
		menu:              tray.DefaultMenu(),
		machine:           lifecycle.NewMachine(),
		paths:             paths,
		launchConfig:      defaultShellLaunchConfig(),
	}, nil
}

func (s *ShellLifecycle) State() lifecycle.State {
	return s.machine.State()
}

func (s *ShellLifecycle) DesktopWebBridge() (*DesktopWebBridge, error) {
	if err := s.ensureDesktopWebBridge(); err != nil {
		return nil, err
	}
	return s.webBridge, nil
}

func (s *ShellLifecycle) TrayMenu() tray.Menu {
	return s.menu
}

func (s *ShellLifecycle) ViewModel() ShellViewModel {
	return ShellViewModel{
		State:       s.machine.State(),
		Wizard:      s.wizard,
		Diagnostics: s.diagnostics,
	}
}

func (s *ShellLifecycle) DesktopAccessView() DesktopAccessView {
	return buildDesktopAccessView(s.paths, s.launchConfig, s.bootstrap)
}

func (s *ShellLifecycle) Start(ctx context.Context) error {
	if err := s.ensureTray(); err != nil {
		return err
	}
	return s.startOrDiagnose(ctx, false)
}

func (s *ShellLifecycle) ContinueFromWizard(ctx context.Context) error {
	launchConfig := normalizeShellLaunchConfig(s.launchConfig)
	if err := launchConfig.Validate(); err != nil {
		return s.enterDiagnostics(ErrorCodeConfigValidationFailed, err)
	}
	if err := s.ensureBootstrap(false); err != nil {
		return s.enterDiagnostics(codeForBootstrapError(err), err)
	}
	if err := s.ensureDesktopWebBridge(); err != nil {
		return s.machine.Fail(fmt.Errorf("create desktop web bridge: %w", err))
	}
	if err := s.runtime.ValidateFixedRuntime(s.paths.RootDir); err != nil {
		return s.enterDiagnostics(ErrorCodeWebView2RuntimeInvalid, err)
	}
	if err := saveCompletedShellConfig(s.paths, launchConfig); err != nil {
		return s.enterDiagnostics(configstore.ErrorCodeConfigRootNotWritable, fmt.Errorf("persist first-run confirmation: %w", err))
	}
	s.wizardRequired = false
	s.wizardCompleted = true
	s.wizard = nil
	s.launchConfig = launchConfig
	return s.startCoreVisible(ctx)
}

func (s *ShellLifecycle) RetryStart(ctx context.Context) error {
	launchConfig := normalizeShellLaunchConfig(s.launchConfig)
	if err := launchConfig.Validate(); err != nil {
		return s.enterDiagnostics(ErrorCodeConfigValidationFailed, err)
	}
	if _, err := s.machine.RetryFromDiagnostics(); err != nil {
		return err
	}
	if err := saveShellConfig(s.paths, launchConfig, s.wizardCompleted); err != nil {
		return s.enterDiagnostics(configstore.ErrorCodeConfigRootNotWritable, fmt.Errorf("persist shell launch config: %w", err))
	}
	s.launchConfig = launchConfig
	s.startResult = nil
	s.diagnostics = nil
	return s.startOrDiagnose(ctx, true)
}

func (s *ShellLifecycle) HandleWindowCloseRequested() (lifecycle.Action, error) {
	return s.hideToTray()
}

func (s *ShellLifecycle) HandleAltF4() (lifecycle.Action, error) {
	return s.hideToTray()
}

func (s *ShellLifecycle) HandleTaskbarCloseRequested() (lifecycle.Action, error) {
	return s.hideToTray()
}

func (s *ShellLifecycle) HandleTrayAction(ctx context.Context, actionID tray.ActionID) error {
	switch actionID {
	case tray.ActionShowMainWindow:
		return s.ShowMainWindow()
	case tray.ActionOpenLogDirectory:
		return s.OpenLogDirectory()
	case tray.ActionExit:
		return s.ExplicitExit(ctx)
	default:
		return fmt.Errorf("unsupported tray action %q", actionID)
	}
}

func (s *ShellLifecycle) HandleSingleInstanceSignal(signal singleinstance.Signal) error {
	if signal != singleinstance.SignalShowMainWindow {
		return nil
	}
	return s.ShowMainWindow()
}

func (s *ShellLifecycle) ShowMainWindow() error {
	transition, err := s.machine.RequestShowMainWindow()
	if err != nil {
		return err
	}
	if transition.Action != lifecycle.ActionShowMainWindow {
		return nil
	}
	if err := s.window.Show(); err != nil {
		return s.machine.Fail(fmt.Errorf("show main window: %w", err))
	}
	return nil
}

func (s *ShellLifecycle) OpenLogDirectory() error {
	return s.pathOpener.Open(s.paths.LogDir)
}

func (s *ShellLifecycle) CopyDiagnostics() string {
	if s.diagnostics == nil {
		return ""
	}
	return s.diagnostics.CopyText
}

func (s *ShellLifecycle) ProxyAccessToken() string {
	if s.bootstrap == nil {
		return ""
	}
	return strings.TrimSpace(s.bootstrap.Secrets.ProxyToken)
}

func (s *ShellLifecycle) SetLaunchPort(port int) error {
	next := normalizeShellLaunchConfig(ShellLaunchConfig{
		ListenAddress: fixedListenAddress,
		Port:          port,
	})
	if err := next.Validate(); err != nil {
		return err
	}

	switch s.machine.State() {
	case lifecycle.StateRunningVisible, lifecycle.StateRunningTray, lifecycle.StateStartingCore, lifecycle.StateStopping:
		return fmt.Errorf("cannot change launch port from state %q", s.machine.State())
	}

	s.launchConfig = next
	s.startResult = nil
	if s.supervisorFactory != nil {
		s.supervisor = nil
	}
	if s.wizard != nil {
		s.wizard = buildWizardView(s.paths, s.launchConfig)
	}
	if s.diagnostics != nil {
		s.diagnostics = buildDiagnosticsView(s.paths, s.diagnostics.Code, errors.New(s.diagnostics.Details), "", s.launchConfig, nil)
	}
	return nil
}

func (s *ShellLifecycle) ExplicitExit(ctx context.Context) error {
	transition, err := s.machine.BeginExplicitExit()
	if err != nil {
		return err
	}
	if transition.Action != lifecycle.ActionStopCoreAndExit {
		return nil
	}

	var shutdownErr error
	if s.supervisor != nil {
		_, shutdownErr = s.supervisor.Shutdown(ctx)
	}
	exitErr := s.runtime.Exit()
	if shutdownErr != nil || exitErr != nil {
		return s.machine.Fail(errors.Join(shutdownErr, exitErr))
	}
	return nil
}

func (s *ShellLifecycle) ensureTray() error {
	if s.trayReady {
		return nil
	}
	if err := s.tray.Init(s.menu, func(actionID tray.ActionID) error {
		return s.HandleTrayAction(context.Background(), actionID)
	}); err != nil {
		return s.machine.FailWithCode(
			lifecycle.ErrorCodeTrayInitFailed,
			fmt.Errorf("initialize tray menu: %w", err),
		)
	}
	s.trayReady = true
	return nil
}

func (s *ShellLifecycle) startCoreVisible(ctx context.Context) error {
	if err := s.resolveSupervisor(); err != nil {
		return s.handleSupervisorStartFailure(err)
	}
	if _, err := s.machine.BeginCoreStart(); err != nil {
		return err
	}

	result, err := s.supervisor.Start(ctx)
	if err != nil {
		return s.handleSupervisorStartFailure(err)
	}
	s.startResult = result
	s.diagnostics = nil
	s.wizard = nil

	if _, err := s.machine.CoreStartedVisible(); err != nil {
		return s.machine.Fail(err)
	}
	if err := s.window.Show(); err != nil {
		_, shutdownErr := s.supervisor.Shutdown(ctx)
		return s.machine.Fail(errors.Join(fmt.Errorf("show main window: %w", err), shutdownErr))
	}
	return nil
}

func (s *ShellLifecycle) hideToTray() (lifecycle.Action, error) {
	transition, err := s.machine.RequestHideToTray()
	if err != nil {
		return lifecycle.ActionNone, err
	}
	if transition.Action != lifecycle.ActionHideToTray {
		return transition.Action, nil
	}
	if err := s.window.Hide(); err != nil {
		return lifecycle.ActionNone, s.machine.Fail(fmt.Errorf("hide main window to tray: %w", err))
	}
	return transition.Action, nil
}

func (s *ShellLifecycle) startOrDiagnose(ctx context.Context, retry bool) error {
	if err := s.ensureBootstrap(retry); err != nil {
		return s.enterDiagnostics(codeForBootstrapError(err), err)
	}
	if err := s.ensureDesktopWebBridge(); err != nil {
		return s.machine.Fail(fmt.Errorf("create desktop web bridge: %w", err))
	}

	wizardRequired, err := s.shouldShowWizard()
	if err != nil {
		return s.enterDiagnostics(ErrorCodeConfigValidationFailed, err)
	}
	if err := s.runtime.ValidateFixedRuntime(s.paths.RootDir); err != nil {
		return s.enterDiagnostics(ErrorCodeWebView2RuntimeInvalid, err)
	}
	if wizardRequired {
		s.diagnostics = nil
		s.wizard = buildWizardView(s.paths, s.launchConfig)
		if _, err := s.machine.EnterWizard(); err != nil {
			return err
		}
		if err := s.window.Show(); err != nil {
			return s.machine.Fail(fmt.Errorf("show wizard window: %w", err))
		}
		return nil
	}

	return s.startCoreVisible(ctx)
}

func (s *ShellLifecycle) ensureBootstrap(retry bool) error {
	if s.bootstrap != nil && !retry {
		return nil
	}
	if s.bootstrapErr != nil && !retry {
		return s.bootstrapErr
	}
	if s.bootstrapper == nil {
		if s.bootstrap != nil {
			return nil
		}
		if s.bootstrapErr != nil {
			return s.bootstrapErr
		}
		return fmt.Errorf("bootstrapper is required")
	}

	bootstrap, err := s.bootstrapper(s.paths.RootDir)
	if err != nil {
		s.bootstrap = nil
		s.bootstrapErr = err
		s.webBridge = nil
		return err
	}
	paths, err := deriveShellPaths(s.paths.RootDir, bootstrap)
	if err != nil {
		return err
	}
	s.bootstrap = bootstrap
	s.bootstrapErr = nil
	s.webBridge = nil
	s.paths = paths
	if retry && s.supervisorFactory != nil {
		s.supervisor = nil
	}
	return nil
}

func (s *ShellLifecycle) ensureDesktopWebBridge() error {
	if s.webBridge != nil {
		return nil
	}
	if err := s.ensureBootstrap(false); err != nil {
		return err
	}
	bridge, err := NewDesktopWebBridge(s.bootstrap)
	if err != nil {
		return err
	}
	s.webBridge = bridge
	return nil
}

func (s *ShellLifecycle) shouldShowWizard() (bool, error) {
	config, found, err := loadShellConfig(s.paths)
	if err != nil {
		return false, fmt.Errorf("load shell config: %w", err)
	}
	if found {
		s.launchConfig = shellLaunchConfigFromPersisted(*config)
		s.wizardCompleted = config.WizardCompleted
	}
	if s.wizardRequired {
		s.wizardCompleted = false
		return true, nil
	}
	if !found {
		s.wizardCompleted = false
		return true, nil
	}
	if !config.WizardCompleted {
		s.wizardCompleted = false
		return true, nil
	}
	s.wizardCompleted = true
	return false, nil
}

func (s *ShellLifecycle) resolveSupervisor() error {
	if s.supervisor != nil {
		if s.supervisorFactory == nil || s.supervisorConfig == normalizeShellLaunchConfig(s.launchConfig) {
			return nil
		}
	}
	if s.supervisorFactory == nil {
		return fmt.Errorf("supervisor is required")
	}
	if s.bootstrap == nil {
		return fmt.Errorf("bootstrap result is required before creating supervisor")
	}
	launchConfig := normalizeShellLaunchConfig(s.launchConfig)
	supervisorInstance, err := s.supervisorFactory(s.bootstrap, launchConfig)
	if err != nil {
		return err
	}
	s.supervisor = supervisorInstance
	s.supervisorConfig = launchConfig
	return nil
}

func (s *ShellLifecycle) handleSupervisorStartFailure(err error) error {
	code := supervisor.ErrorCodeOf(err)
	if code == "" {
		return s.enterDiagnostics(ErrorCodeCoreStartFailed, err)
	}
	if code == supervisor.ErrorCodeCoreProcessExited {
		code = ErrorCodeCoreExitedEarly
	}
	return s.enterDiagnostics(code, err)
}

func (s *ShellLifecycle) enterDiagnostics(code string, cause error) error {
	if code == "" {
		return s.machine.Fail(cause)
	}
	diagnosticErr := s.machine.Diagnose(code, cause)
	healthURL := ""
	if s.startResult != nil {
		healthURL = s.startResult.HealthURL
	}
	s.startResult = nil
	s.wizard = nil
	s.diagnostics = buildDiagnosticsView(s.paths, code, cause, healthURL, s.launchConfig, supervisor.PortConflictOf(cause))
	if err := s.window.Show(); err != nil {
		return s.machine.Fail(errors.Join(diagnosticErr, fmt.Errorf("show diagnostics window: %w", err)))
	}
	return nil
}

func codeForBootstrapError(err error) string {
	if code := configstore.ErrorCodeOf(err); code != "" {
		return code
	}
	return configstore.ErrorCodeConfigRootNotWritable
}
