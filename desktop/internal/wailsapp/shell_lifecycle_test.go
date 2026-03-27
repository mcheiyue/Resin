//go:build windows

package wailsapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Resinat/Resin/desktop/internal/configstore"
	"github.com/Resinat/Resin/desktop/internal/lifecycle"
	"github.com/Resinat/Resin/desktop/internal/supervisor"
	"github.com/Resinat/Resin/desktop/internal/tray"
)

func TestShellLifecycle_CloseHidesToTray(t *testing.T) {
	t.Parallel()

	fx := newShellLifecycleFixture(t)

	if err := fx.shell.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	assertTrayMenu(t, fx.tray.menu)
	if fx.shell.State() != lifecycle.StateRunningVisible {
		t.Fatalf("state after Start() = %q, want %q", fx.shell.State(), lifecycle.StateRunningVisible)
	}

	action, err := fx.shell.HandleWindowCloseRequested()
	if err != nil {
		t.Fatalf("HandleWindowCloseRequested() error = %v", err)
	}
	assertHiddenToTray(t, fx, action)

	if err := fx.shell.ShowMainWindow(); err != nil {
		t.Fatalf("ShowMainWindow() after close error = %v", err)
	}

	action, err = fx.shell.HandleAltF4()
	if err != nil {
		t.Fatalf("HandleAltF4() error = %v", err)
	}
	assertHiddenToTray(t, fx, action)

	if err := fx.shell.ShowMainWindow(); err != nil {
		t.Fatalf("ShowMainWindow() after Alt+F4 error = %v", err)
	}

	action, err = fx.shell.HandleTaskbarCloseRequested()
	if err != nil {
		t.Fatalf("HandleTaskbarCloseRequested() error = %v", err)
	}
	assertHiddenToTray(t, fx, action)

	if fx.window.hideCalls != 3 {
		t.Fatalf("window.Hide() calls = %d, want 3", fx.window.hideCalls)
	}
	if fx.supervisor.shutdownCalls != 0 {
		t.Fatalf("supervisor.Shutdown() calls = %d, want 0", fx.supervisor.shutdownCalls)
	}
	if got, want := fx.window.showCalls, 3; got != want {
		t.Fatalf("window.Show() calls = %d, want %d", got, want)
	}

	if err := fx.shell.HandleTrayAction(context.Background(), tray.ActionOpenLogDirectory); err != nil {
		t.Fatalf("HandleTrayAction(OPEN_LOG_DIRECTORY) error = %v", err)
	}
	if got, want := fx.opener.paths, []string{fx.bootstrap.Layout.LogDir}; !reflect.DeepEqual(got, want) {
		t.Fatalf("opened paths = %#v, want %#v", got, want)
	}
	if err := fx.shell.HandleSingleInstanceSignal("SHOW_MAIN_WINDOW"); err != nil {
		t.Fatalf("HandleSingleInstanceSignal() error = %v", err)
	}
	if got, want := fx.runtime.validatedRoots, []string{fx.bootstrap.Layout.RootDir}; !reflect.DeepEqual(got, want) {
		t.Fatalf("validated runtime roots = %#v, want %#v", got, want)
	}
}

func TestShellLifecycle_ExplicitExitStopsCore(t *testing.T) {
	t.Parallel()

	fx := newShellLifecycleFixture(t)

	if err := fx.shell.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := fx.shell.HandleTrayAction(context.Background(), tray.ActionExit); err != nil {
		t.Fatalf("HandleTrayAction(EXIT) error = %v", err)
	}

	if fx.shell.State() != lifecycle.StateStopping {
		t.Fatalf("state after explicit exit = %q, want %q", fx.shell.State(), lifecycle.StateStopping)
	}
	if fx.supervisor.shutdownCalls != 1 {
		t.Fatalf("supervisor.Shutdown() calls = %d, want 1", fx.supervisor.shutdownCalls)
	}
	if fx.runtime.exitCalls != 1 {
		t.Fatalf("runtime.Exit() calls = %d, want 1", fx.runtime.exitCalls)
	}
	if got, want := fx.events, []string{"tray-init", "runtime-validate", "supervisor-start", "window-show", "supervisor-shutdown", "runtime-exit"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event order = %#v, want %#v", got, want)
	}
	if fx.supervisor.startCalls != 1 {
		t.Fatalf("supervisor.Start() calls = %d, want 1", fx.supervisor.startCalls)
	}
	if fx.window.hideCalls != 0 {
		t.Fatalf("window.Hide() calls = %d, want 0", fx.window.hideCalls)
	}
	if fx.window.showCalls != 1 {
		t.Fatalf("window.Show() calls = %d, want 1", fx.window.showCalls)
	}
	if fx.runtime.exitedBeforeShutdown {
		t.Fatal("runtime exited before supervisor shutdown completed")
	}
	if fx.shell.startResult == nil {
		t.Fatal("startResult should be captured after successful startup")
	}
	if fx.shell.startResult.HealthURL == "" {
		t.Fatal("startResult.HealthURL should not be empty")
	}
	if fx.shell.startResult.Mode == "" {
		t.Fatal("startResult.Mode should not be empty")
	}
	if fx.shell.startResult.PID <= 0 {
		t.Fatalf("startResult.PID = %d, want > 0", fx.shell.startResult.PID)
	}
	if got := filepath.Clean(fx.bootstrap.Layout.LogDir); got == "" {
		t.Fatal("bootstrap log directory should not be empty")
	}
}

func TestShellLifecycle_TrayInitFailureBlocksStartup(t *testing.T) {
	t.Parallel()

	fx := newShellLifecycleFixture(t)
	fx.tray.initErr = errors.New("boom")

	err := fx.shell.Start(context.Background())
	if err == nil {
		t.Fatal("Start() error = nil, want non-nil")
	}
	if got := lifecycle.ErrorCodeOf(err); got != lifecycle.ErrorCodeTrayInitFailed {
		t.Fatalf("ErrorCodeOf(err) = %q, want %q (err=%v)", got, lifecycle.ErrorCodeTrayInitFailed, err)
	}
	if fx.shell.State() != lifecycle.StateError {
		t.Fatalf("state after tray init failure = %q, want %q", fx.shell.State(), lifecycle.StateError)
	}
	if fx.supervisor.startCalls != 0 {
		t.Fatalf("supervisor.Start() calls = %d, want 0", fx.supervisor.startCalls)
	}
	if fx.supervisor.shutdownCalls != 0 {
		t.Fatalf("supervisor.Shutdown() calls = %d, want 0", fx.supervisor.shutdownCalls)
	}
	if fx.window.showCalls != 0 {
		t.Fatalf("window.Show() calls = %d, want 0", fx.window.showCalls)
	}
	if fx.runtime.exitCalls != 0 {
		t.Fatalf("runtime.Exit() calls = %d, want 0", fx.runtime.exitCalls)
	}
	if got, want := fx.events, []string{"tray-init"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event order = %#v, want %#v", got, want)
	}
	if fx.tray.menu.Items == nil {
		t.Fatal("tray menu should still be built before init failure")
	}
	assertTrayMenu(t, fx.tray.menu)
	if fx.opener.paths != nil {
		t.Fatalf("opened paths = %#v, want nil", fx.opener.paths)
	}
	if fx.tray.handler == nil {
		t.Fatal("tray handler should be wired during init attempt")
	}
	if got, want := fx.tray.initCalls, 1; got != want {
		t.Fatalf("tray.Init() calls = %d, want %d", got, want)
	}
	if fx.shell.startResult != nil {
		t.Fatal("startResult should remain nil on failed startup")
	}
	if filepath.Clean(fx.bootstrap.Layout.LogDir) == "" {
		t.Fatal("bootstrap log directory should not be empty")
	}
}

func TestApp_CloseEntrypointsConvergeToHideToTray(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fx := newShellLifecycleFixture(t)
	app, err := NewApp(AppConfig{
		Bootstrap:   fx.bootstrap,
		Supervisor:  fx.supervisor,
		TrayManager: fx.tray,
		Window:      fx.window,
		PathOpener:  fx.opener,
		Runtime:     fx.runtime,
		Bindings:    NewRuntimeBindings(),
	})
	if err != nil {
		t.Fatalf("NewApp() error = %v", err)
	}

	if err := app.Startup(ctx); err != nil {
		t.Fatalf("Startup() error = %v", err)
	}
	if !app.BeforeClose(ctx) {
		t.Fatal("BeforeClose() = false, want true to prevent close and hide to tray")
	}
	if err := app.ShowMainWindow(ctx); err != nil {
		t.Fatalf("ShowMainWindow() after BeforeClose error = %v", err)
	}
	if !app.HandleAltF4(ctx) {
		t.Fatal("HandleAltF4() = false, want true")
	}
	if err := app.ShowMainWindow(ctx); err != nil {
		t.Fatalf("ShowMainWindow() after HandleAltF4 error = %v", err)
	}
	if !app.HandleTaskbarClose(ctx) {
		t.Fatal("HandleTaskbarClose() = false, want true")
	}
}

func TestShellLifecycle_FirstRunWizardCompletes(t *testing.T) {
	t.Parallel()

	fx := newFirstRunShellLifecycleFixture(t)

	if err := fx.shell.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if fx.shell.State() != lifecycle.StateWizard {
		t.Fatalf("state after Start() = %q, want %q", fx.shell.State(), lifecycle.StateWizard)
	}
	viewModel := fx.shell.ViewModel()
	if viewModel.Wizard == nil {
		t.Fatal("wizard view model should not be nil")
	}
	if viewModel.Wizard.PortableRootDir != fx.bootstrap.Layout.RootDir {
		t.Fatalf("wizard portable root = %q, want %q", viewModel.Wizard.PortableRootDir, fx.bootstrap.Layout.RootDir)
	}
	if viewModel.Wizard.ListenAddress != fixedListenAddress {
		t.Fatalf("wizard listen address = %q, want %q", viewModel.Wizard.ListenAddress, fixedListenAddress)
	}
	if viewModel.Wizard.Port != fixedPort {
		t.Fatalf("wizard port = %d, want %d", viewModel.Wizard.Port, fixedPort)
	}
	if !strings.Contains(viewModel.Wizard.TokenSummary, "自动生成") {
		t.Fatalf("wizard token summary = %q, want mention of auto generation", viewModel.Wizard.TokenSummary)
	}
	if fx.supervisor.startCalls != 0 {
		t.Fatalf("supervisor.Start() calls before confirm = %d, want 0", fx.supervisor.startCalls)
	}

	if err := fx.shell.ContinueFromWizard(context.Background()); err != nil {
		t.Fatalf("ContinueFromWizard() error = %v", err)
	}
	if fx.shell.State() != lifecycle.StateRunningVisible {
		t.Fatalf("state after ContinueFromWizard() = %q, want %q", fx.shell.State(), lifecycle.StateRunningVisible)
	}
	if fx.supervisor.startCalls != 1 {
		t.Fatalf("supervisor.Start() calls = %d, want 1", fx.supervisor.startCalls)
	}
	if fx.window.showCalls != 2 {
		t.Fatalf("window.Show() calls = %d, want 2", fx.window.showCalls)
	}

	config, found, err := loadShellConfig(fx.shell.paths)
	if err != nil {
		t.Fatalf("loadShellConfig() error = %v", err)
	}
	if !found {
		t.Fatal("first-run confirmation should persist shell config")
	}
	if !config.WizardCompleted {
		t.Fatal("persisted shell config should mark wizard completed")
	}

	followUp := newShellLifecycleFixtureFromRoot(t, fx.rootDir)
	if err := followUp.shell.Start(context.Background()); err != nil {
		t.Fatalf("follow-up Start() error = %v", err)
	}
	if followUp.shell.State() != lifecycle.StateRunningVisible {
		t.Fatalf("follow-up state = %q, want %q", followUp.shell.State(), lifecycle.StateRunningVisible)
	}
	if followUp.shell.ViewModel().Wizard != nil {
		t.Fatal("wizard should not be shown again once first-run confirmation is persisted")
	}
}

func TestShellLifecycle_DiagnosticsExposeLogPath(t *testing.T) {
	t.Parallel()

	fx := newShellLifecycleFixture(t)
	fx.supervisor.startErr = &supervisor.Error{Code: supervisor.ErrorCodePortInUse, Err: errors.New("occupied")}

	if err := fx.shell.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v, want nil because diagnostics page should render", err)
	}
	if fx.shell.State() != lifecycle.StateDiagnostics {
		t.Fatalf("state after Start() = %q, want %q", fx.shell.State(), lifecycle.StateDiagnostics)
	}
	viewModel := fx.shell.ViewModel()
	if viewModel.Diagnostics == nil {
		t.Fatal("diagnostics view model should not be nil")
	}
	if viewModel.Diagnostics.Code != supervisor.ErrorCodePortInUse {
		t.Fatalf("diagnostics code = %q, want %q", viewModel.Diagnostics.Code, supervisor.ErrorCodePortInUse)
	}
	if viewModel.Diagnostics.LogDir != fx.bootstrap.Layout.LogDir {
		t.Fatalf("diagnostics log dir = %q, want %q", viewModel.Diagnostics.LogDir, fx.bootstrap.Layout.LogDir)
	}
	copyText := fx.shell.CopyDiagnostics()
	if !strings.Contains(copyText, fx.bootstrap.Layout.LogDir) {
		t.Fatalf("CopyDiagnostics() = %q, want log dir %q", copyText, fx.bootstrap.Layout.LogDir)
	}
	if !strings.Contains(copyText, supervisor.ErrorCodePortInUse) {
		t.Fatalf("CopyDiagnostics() = %q, want error code %q", copyText, supervisor.ErrorCodePortInUse)
	}
	if err := fx.shell.OpenLogDirectory(); err != nil {
		t.Fatalf("OpenLogDirectory() error = %v", err)
	}
	if got, want := fx.opener.paths, []string{fx.bootstrap.Layout.LogDir}; !reflect.DeepEqual(got, want) {
		t.Fatalf("opened paths = %#v, want %#v", got, want)
	}

	fx.supervisor.startErr = nil
	if err := fx.shell.RetryStart(context.Background()); err != nil {
		t.Fatalf("RetryStart() error = %v", err)
	}
	if fx.shell.State() != lifecycle.StateRunningVisible {
		t.Fatalf("state after RetryStart() = %q, want %q", fx.shell.State(), lifecycle.StateRunningVisible)
	}
	if fx.supervisor.startCalls != 2 {
		t.Fatalf("supervisor.Start() calls = %d, want 2", fx.supervisor.startCalls)
	}
}

func TestShellLifecycle_InvalidConfigShowsDiagnostics(t *testing.T) {
	t.Parallel()

	fx := newShellLifecycleFixture(t)
	writeShellConfigPayload(t, fx.shell.paths, `{"version":1,"wizard_completed":true,"listen_address":"0.0.0.0","port":2260}`)

	if err := fx.shell.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v, want nil because diagnostics page should render", err)
	}
	if fx.shell.State() != lifecycle.StateDiagnostics {
		t.Fatalf("state after Start() = %q, want %q", fx.shell.State(), lifecycle.StateDiagnostics)
	}
	viewModel := fx.shell.ViewModel()
	if viewModel.Diagnostics == nil {
		t.Fatal("diagnostics view model should not be nil")
	}
	if viewModel.Diagnostics.Code != ErrorCodeConfigValidationFailed {
		t.Fatalf("diagnostics code = %q, want %q", viewModel.Diagnostics.Code, ErrorCodeConfigValidationFailed)
	}
	if fx.supervisor.startCalls != 0 {
		t.Fatalf("supervisor.Start() calls = %d, want 0", fx.supervisor.startCalls)
	}
}

func TestShellLifecycle_CoreExitedEarlyShowsDiagnostics(t *testing.T) {
	t.Parallel()

	fx := newShellLifecycleFixture(t)
	fx.supervisor.startErr = &supervisor.Error{Code: supervisor.ErrorCodeCoreProcessExited, Err: errors.New("exit status 1")}

	if err := fx.shell.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v, want nil because diagnostics page should render", err)
	}
	if fx.shell.State() != lifecycle.StateDiagnostics {
		t.Fatalf("state after Start() = %q, want %q", fx.shell.State(), lifecycle.StateDiagnostics)
	}
	viewModel := fx.shell.ViewModel()
	if viewModel.Diagnostics == nil {
		t.Fatal("diagnostics view model should not be nil")
	}
	if viewModel.Diagnostics.Code != ErrorCodeCoreExitedEarly {
		t.Fatalf("diagnostics code = %q, want %q", viewModel.Diagnostics.Code, ErrorCodeCoreExitedEarly)
	}
	if fx.supervisor.startCalls != 1 {
		t.Fatalf("supervisor.Start() calls = %d, want 1", fx.supervisor.startCalls)
	}
}

func TestShellLifecycle_FixedRuntimeMissing(t *testing.T) {
	t.Parallel()

	fx := newShellLifecycleFixture(t)
	fx.runtime.validateErr = os.ErrNotExist

	if err := fx.shell.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v, want nil because diagnostics page should render", err)
	}
	if fx.shell.State() != lifecycle.StateDiagnostics {
		t.Fatalf("state after Start() = %q, want %q", fx.shell.State(), lifecycle.StateDiagnostics)
	}
	viewModel := fx.shell.ViewModel()
	if viewModel.Diagnostics == nil {
		t.Fatal("diagnostics view model should not be nil")
	}
	if viewModel.Diagnostics.Code != ErrorCodeWebView2RuntimeInvalid {
		t.Fatalf("diagnostics code = %q, want %q", viewModel.Diagnostics.Code, ErrorCodeWebView2RuntimeInvalid)
	}
	if fx.supervisor.startCalls != 0 {
		t.Fatalf("supervisor.Start() calls = %d, want 0", fx.supervisor.startCalls)
	}
}

func TestShellLifecycle_ConfigRootNotWritableShowsDiagnostics(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	conflictPath := filepath.Join(rootDir, ".resin-write-probe")
	if err := os.Mkdir(conflictPath, 0o755); err != nil {
		t.Fatalf("create write probe conflict: %v", err)
	}
	_, bootstrapErr := configstore.Bootstrap(rootDir)
	if bootstrapErr == nil {
		t.Fatal("configstore.Bootstrap() error = nil, want non-nil")
	}

	fx := newBootstrapErrorShellLifecycleFixture(t, rootDir, bootstrapErr)
	if err := fx.shell.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v, want nil because diagnostics page should render", err)
	}
	if fx.shell.State() != lifecycle.StateDiagnostics {
		t.Fatalf("state after Start() = %q, want %q", fx.shell.State(), lifecycle.StateDiagnostics)
	}
	viewModel := fx.shell.ViewModel()
	if viewModel.Diagnostics == nil {
		t.Fatal("diagnostics view model should not be nil")
	}
	if viewModel.Diagnostics.Code != configstore.ErrorCodeConfigRootNotWritable {
		t.Fatalf("diagnostics code = %q, want %q", viewModel.Diagnostics.Code, configstore.ErrorCodeConfigRootNotWritable)
	}
	if viewModel.Diagnostics.LogDir != filepath.Join(rootDir, filepath.FromSlash("data/logs")) {
		t.Fatalf("diagnostics log dir = %q, want %q", viewModel.Diagnostics.LogDir, filepath.Join(rootDir, filepath.FromSlash("data/logs")))
	}
	if fx.supervisor.startCalls != 0 {
		t.Fatalf("supervisor.Start() calls = %d, want 0", fx.supervisor.startCalls)
	}
}

type shellLifecycleFixtureConfig struct {
	rootDir             string
	firstRun            bool
	preserveShellConfig bool
	bootstrapErr        error
}

type shellLifecycleFixture struct {
	rootDir    string
	bootstrap  *configstore.BootstrapResult
	shell      *ShellLifecycle
	supervisor *stubSupervisor
	window     *stubWindow
	tray       *stubTrayManager
	opener     *stubPathOpener
	runtime    *stubRuntime
	events     []string
}

func newShellLifecycleFixture(t *testing.T) *shellLifecycleFixture {
	t.Helper()
	return newShellLifecycleFixtureWithConfig(t, shellLifecycleFixtureConfig{})
}

func newFirstRunShellLifecycleFixture(t *testing.T) *shellLifecycleFixture {
	t.Helper()
	return newShellLifecycleFixtureWithConfig(t, shellLifecycleFixtureConfig{firstRun: true})
}

func newShellLifecycleFixtureFromRoot(t *testing.T, rootDir string) *shellLifecycleFixture {
	t.Helper()
	return newShellLifecycleFixtureWithConfig(t, shellLifecycleFixtureConfig{
		rootDir:             rootDir,
		preserveShellConfig: true,
	})
}

func newBootstrapErrorShellLifecycleFixture(t *testing.T, rootDir string, bootstrapErr error) *shellLifecycleFixture {
	t.Helper()
	return newShellLifecycleFixtureWithConfig(t, shellLifecycleFixtureConfig{
		rootDir:             rootDir,
		bootstrapErr:        bootstrapErr,
		preserveShellConfig: true,
	})
}

func newShellLifecycleFixtureWithConfig(t *testing.T, config shellLifecycleFixtureConfig) *shellLifecycleFixture {
	t.Helper()

	rootDir := config.rootDir
	if rootDir == "" {
		rootDir = t.TempDir()
	}

	var (
		bootstrap *configstore.BootstrapResult
		err       error
	)
	if config.bootstrapErr == nil {
		bootstrap, err = configstore.Bootstrap(rootDir)
		if err != nil {
			t.Fatalf("configstore.Bootstrap() error = %v", err)
		}
	}

	paths, err := deriveShellPaths(rootDir, bootstrap)
	if err != nil {
		t.Fatalf("deriveShellPaths() error = %v", err)
	}
	if !config.preserveShellConfig && config.bootstrapErr == nil {
		if config.firstRun {
			if err := os.Remove(shellConfigPath(paths)); err != nil && !os.IsNotExist(err) {
				t.Fatalf("remove shell config: %v", err)
			}
		} else {
			if err := saveCompletedShellConfig(paths); err != nil {
				t.Fatalf("saveCompletedShellConfig() error = %v", err)
			}
		}
	}

	fixture := &shellLifecycleFixture{rootDir: rootDir, bootstrap: bootstrap}
	fixture.supervisor = &stubSupervisor{fixture: fixture}
	fixture.window = &stubWindow{fixture: fixture}
	fixture.tray = &stubTrayManager{fixture: fixture}
	fixture.opener = &stubPathOpener{}
	fixture.runtime = &stubRuntime{fixture: fixture}

	fixture.shell, err = NewShellLifecycle(ShellLifecycleConfig{
		RootDir:      rootDir,
		Bootstrap:    bootstrap,
		BootstrapErr: config.bootstrapErr,
		Supervisor:   fixture.supervisor,
		Window:       fixture.window,
		Tray:         fixture.tray,
		PathOpener:   fixture.opener,
		Runtime:      fixture.runtime,
	})
	if err != nil {
		t.Fatalf("NewShellLifecycle() error = %v", err)
	}

	return fixture
}

type stubSupervisor struct {
	fixture       *shellLifecycleFixture
	startCalls    int
	shutdownCalls int
	shutDown      bool
	startErr      error
	startResult   *supervisor.StartResult
}

type stubWindow struct {
	fixture   *shellLifecycleFixture
	showCalls int
	hideCalls int
}

func (w *stubWindow) Show() error {
	w.showCalls++
	w.fixture.events = append(w.fixture.events, "window-show")
	return nil
}

func (w *stubWindow) Hide() error {
	w.hideCalls++
	w.fixture.events = append(w.fixture.events, "window-hide")
	return nil
}

type stubTrayManager struct {
	fixture   *shellLifecycleFixture
	initCalls int
	initErr   error
	menu      tray.Menu
	handler   func(tray.ActionID) error
}

func (m *stubTrayManager) Init(menu tray.Menu, handler func(tray.ActionID) error) error {
	m.initCalls++
	m.menu = menu
	m.handler = handler
	m.fixture.events = append(m.fixture.events, "tray-init")
	return m.initErr
}

type stubPathOpener struct {
	paths []string
}

func (o *stubPathOpener) Open(path string) error {
	o.paths = append(o.paths, path)
	return nil
}

type stubRuntime struct {
	fixture              *shellLifecycleFixture
	exitCalls            int
	exitedBeforeShutdown bool
	validateErr          error
	validatedRoots       []string
}

func (r *stubRuntime) Exit() error {
	r.exitCalls++
	if !r.fixture.supervisor.shutDown {
		r.exitedBeforeShutdown = true
	}
	r.fixture.events = append(r.fixture.events, "runtime-exit")
	return nil
}

func (r *stubRuntime) ValidateFixedRuntime(rootDir string) error {
	r.validatedRoots = append(r.validatedRoots, rootDir)
	r.fixture.events = append(r.fixture.events, "runtime-validate")
	return r.validateErr
}

func (s *stubSupervisor) Start(ctx context.Context) (*supervisor.StartResult, error) {
	s.startCalls++
	s.fixture.events = append(s.fixture.events, "supervisor-start")
	if s.startErr != nil {
		return nil, s.startErr
	}
	if s.startResult != nil {
		return s.startResult, nil
	}
	return &supervisor.StartResult{
		Mode:      supervisor.ModeStartedCore,
		PID:       4242,
		HealthURL: "http://127.0.0.1:2260/healthz",
	}, nil
}

func (s *stubSupervisor) Shutdown(ctx context.Context) (*supervisor.ShutdownResult, error) {
	s.shutdownCalls++
	s.shutDown = true
	s.fixture.events = append(s.fixture.events, "supervisor-shutdown")
	return &supervisor.ShutdownResult{}, nil
}

func writeShellConfigPayload(t *testing.T, paths ShellPaths, payload string) {
	t.Helper()
	if err := os.MkdirAll(paths.DesktopDir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", paths.DesktopDir, err)
	}
	if err := os.WriteFile(shellConfigPath(paths), []byte(payload), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", shellConfigPath(paths), err)
	}
}

func assertHiddenToTray(t *testing.T, fx *shellLifecycleFixture, action lifecycle.Action) {
	t.Helper()
	if action != lifecycle.ActionHideToTray {
		t.Fatalf("action = %q, want %q", action, lifecycle.ActionHideToTray)
	}
	if fx.shell.State() != lifecycle.StateRunningTray {
		t.Fatalf("state after close = %q, want %q", fx.shell.State(), lifecycle.StateRunningTray)
	}
}

func assertTrayMenu(t *testing.T, menu tray.Menu) {
	t.Helper()
	if got, want := len(menu.Items), 3; got != want {
		t.Fatalf("len(menu.Items) = %d, want %d", got, want)
	}
	if got, want := menu.Items[0], (tray.MenuItem{ID: tray.ActionShowMainWindow, Label: "显示主窗口"}); got != want {
		t.Fatalf("menu.Items[0] = %#v, want %#v", got, want)
	}
	if got, want := menu.Items[1], (tray.MenuItem{ID: tray.ActionOpenLogDirectory, Label: "查看日志目录"}); got != want {
		t.Fatalf("menu.Items[1] = %#v, want %#v", got, want)
	}
	if got, want := menu.Items[2], (tray.MenuItem{ID: tray.ActionExit, Label: "退出"}); got != want {
		t.Fatalf("menu.Items[2] = %#v, want %#v", got, want)
	}
}
