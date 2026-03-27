//go:build windows

package supervisor

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Resinat/Resin/desktop/internal/configstore"
)

const (
	helperMarkerEnv      = "RESIN_SUPERVISOR_TEST_HELPER"
	helperModeEnv        = "RESIN_SUPERVISOR_HELPER_MODE"
	helperSignalFileEnv  = "RESIN_SUPERVISOR_SIGNAL_FILE"
	helperReadyFileEnv   = "RESIN_SUPERVISOR_READY_FILE"
	helperAuditFileEnv   = "RESIN_SUPERVISOR_AUDIT_FILE"
	helperModeHealthy    = "healthy"
	helperModeGraceful   = "graceful"
	helperModeSleep      = "sleep"
	helperModeHoldHealth = "hold-health"
)

func TestProcessSupervisor_StartCoreAndReachHealthz(t *testing.T) {
	bootstrap := newTestBootstrap(t)
	auditFile := filepath.Join(t.TempDir(), "args-audit.txt")
	supervisor := newTestSupervisor(t, bootstrap, helperModeHealthy, map[string]string{
		helperAuditFileEnv: auditFile,
	})

	result, err := supervisor.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, shutdownErr := supervisor.Shutdown(shutdownCtx); shutdownErr != nil {
			t.Fatalf("Shutdown() cleanup error = %v", shutdownErr)
		}
	})

	if result.Mode != ModeStartedCore {
		t.Fatalf("result.Mode = %q, want %q", result.Mode, ModeStartedCore)
	}
	if result.PID <= 0 {
		t.Fatalf("result.PID = %d, want > 0", result.PID)
	}

	assertHealthzReady(t, result.HealthURL)

	auditPayload, err := os.ReadFile(auditFile)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v", auditFile, err)
	}
	if got := strings.TrimSpace(string(auditPayload)); got != "OK" {
		t.Fatalf("helper args audit = %q, want OK", got)
	}

	state, err := readStateFile(supervisor.config.statePath)
	if err != nil {
		t.Fatalf("readStateFile() error = %v", err)
	}
	if state.Fingerprint != supervisor.config.fingerprint {
		t.Fatalf("state fingerprint = %q, want %q", state.Fingerprint, supervisor.config.fingerprint)
	}
}

func TestProcessSupervisor_GracefulExitByCtrlBreak(t *testing.T) {
	bootstrap := newTestBootstrap(t)
	signalFile := filepath.Join(t.TempDir(), "signal.txt")
	supervisor := newTestSupervisor(t, bootstrap, helperModeGraceful, map[string]string{
		helperSignalFileEnv: signalFile,
	})

	if _, err := supervisor.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := supervisor.Shutdown(shutdownCtx)
	if err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if result.ForceKilled {
		t.Fatal("Shutdown() should not require force kill")
	}
	if len(result.ExposedEnv) != 0 {
		t.Fatalf("Shutdown() exposed env = %#v, want empty", result.ExposedEnv)
	}

	signalPayload, err := os.ReadFile(signalFile)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v", signalFile, err)
	}
	if got := strings.TrimSpace(string(signalPayload)); got != os.Interrupt.String() {
		t.Fatalf("graceful signal = %q, want %q", got, os.Interrupt.String())
	}

	if _, err := os.Stat(supervisor.config.statePath); !os.IsNotExist(err) {
		t.Fatalf("supervisor state file should be removed, stat err = %v", err)
	}
}

func TestProcessSupervisor_PortCollision(t *testing.T) {
	bootstrap := newTestBootstrap(t)
	supervisor := newTestSupervisor(t, bootstrap, helperModeHealthy, nil)
	listener, err := net.Listen("tcp", supervisor.listenAddress())
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer listener.Close()

	_, err = supervisor.Start(context.Background())
	if err == nil {
		t.Fatal("Start() error = nil, want non-nil")
	}
	if got := ErrorCodeOf(err); got != ErrorCodePortInUse {
		t.Fatalf("ErrorCodeOf(err) = %q, want %q (err=%v)", got, ErrorCodePortInUse, err)
	}
}

func TestProcessSupervisor_StartTimeout(t *testing.T) {
	bootstrap := newTestBootstrap(t)
	supervisor := newTestSupervisor(t, bootstrap, helperModeSleep, nil)
	supervisor.config.startTimeout = 300 * time.Millisecond
	supervisor.config.shutdownTimeout = 200 * time.Millisecond
	supervisor.config.pollInterval = 50 * time.Millisecond

	_, err := supervisor.Start(context.Background())
	if err == nil {
		t.Fatal("Start() error = nil, want non-nil")
	}
	if got := ErrorCodeOf(err); got != ErrorCodeCoreStartTimeout {
		t.Fatalf("ErrorCodeOf(err) = %q, want %q (err=%v)", got, ErrorCodeCoreStartTimeout, err)
	}

	portAvailable, _, portErr := supervisor.portAvailable()
	if portErr != nil {
		t.Fatalf("portAvailable() error = %v", portErr)
	}
	if !portAvailable {
		t.Fatal("expected fixed core port to be free after timeout cleanup")
	}
}

func TestProcessSupervisor_ReattachOrphanCore(t *testing.T) {
	t.Run("match fingerprint reattaches", func(t *testing.T) {
		bootstrap := newTestBootstrap(t)
		supervisor := newTestSupervisor(t, bootstrap, helperModeHealthy, nil)
		cmd, waitCh := startHelperProcess(t, bootstrap, supervisor.config.healthURL, helperModeHoldHealth, map[string]string{
			resinListenAddressKey: supervisor.config.listenAddr,
			resinPortKey:          supervisor.config.port,
		})
		statePath := filepath.Join(bootstrap.Layout.DesktopDir, stateFileName)
		if err := writeStateFile(statePath, persistedState{
			Version:        stateVersion,
			PID:            cmd.Process.Pid,
			Fingerprint:    supervisor.config.fingerprint,
			ExecutablePath: cmd.Path,
			HealthURL:      supervisor.config.healthURL,
		}); err != nil {
			t.Fatalf("writeStateFile() error = %v", err)
		}

		result, err := supervisor.Start(context.Background())
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		if result.Mode != ModeReattachCore {
			t.Fatalf("result.Mode = %q, want %q", result.Mode, ModeReattachCore)
		}
		if result.PID != cmd.Process.Pid {
			t.Fatalf("result.PID = %d, want %d", result.PID, cmd.Process.Pid)
		}

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := supervisor.Shutdown(shutdownCtx); err != nil {
			t.Fatalf("Shutdown() error = %v", err)
		}
		assertWaitResult(t, waitCh)
	})

	t.Run("mismatch fingerprint conflicts", func(t *testing.T) {
		bootstrap := newTestBootstrap(t)
		supervisor := newTestSupervisor(t, bootstrap, helperModeHealthy, nil)
		cmd, waitCh := startHelperProcess(t, bootstrap, supervisor.config.healthURL, helperModeHoldHealth, map[string]string{
			resinListenAddressKey: supervisor.config.listenAddr,
			resinPortKey:          supervisor.config.port,
		})
		statePath := filepath.Join(bootstrap.Layout.DesktopDir, stateFileName)
		if err := writeStateFile(statePath, persistedState{
			Version:        stateVersion,
			PID:            cmd.Process.Pid,
			Fingerprint:    "mismatch-fingerprint",
			ExecutablePath: cmd.Path,
			HealthURL:      supervisor.config.healthURL,
		}); err != nil {
			t.Fatalf("writeStateFile() error = %v", err)
		}

		_, err := supervisor.Start(context.Background())
		if err == nil {
			t.Fatal("Start() error = nil, want non-nil")
		}
		if got := ErrorCodeOf(err); got != ErrorCodeCoreRecoveryConflict {
			t.Fatalf("ErrorCodeOf(err) = %q, want %q (err=%v)", got, ErrorCodeCoreRecoveryConflict, err)
		}

		cleanupHelperProcess(t, cmd, waitCh)
	})
}

func TestIsAddressInUseError(t *testing.T) {
	t.Parallel()

	bindErr := &net.OpError{
		Op:  "listen",
		Net: "tcp",
		Err: &os.SyscallError{Syscall: "bind", Err: windowsAddrInUseErr},
	}
	if !isAddressInUseError(bindErr) {
		t.Fatal("isAddressInUseError() = false, want true for wrapped address-in-use error")
	}

	otherErr := &net.OpError{
		Op:  "listen",
		Net: "tcp",
		Err: errors.New("synthetic listen failure"),
	}
	if isAddressInUseError(otherErr) {
		t.Fatal("isAddressInUseError() = true, want false for non address-in-use error")
	}
}

func TestProcessSupervisor_HelperCore(t *testing.T) {
	if os.Getenv(helperMarkerEnv) != "1" {
		t.Skip("helper process only")
	}

	mode := os.Getenv(helperModeEnv)
	adminToken := os.Getenv("RESIN_ADMIN_TOKEN")
	proxyToken := os.Getenv("RESIN_PROXY_TOKEN")
	if auditPath := os.Getenv(helperAuditFileEnv); auditPath != "" {
		status := "OK"
		for _, arg := range os.Args {
			if (adminToken != "" && strings.Contains(arg, adminToken)) || (proxyToken != "" && strings.Contains(arg, proxyToken)) {
				status = "TOKEN_IN_ARGS"
				break
			}
		}
		if err := os.WriteFile(auditPath, []byte(status), 0o600); err != nil {
			t.Fatalf("write args audit: %v", err)
		}
	}

	switch mode {
	case helperModeHealthy, helperModeGraceful, helperModeHoldHealth:
		runHealthHelper(t, http.StatusOK)
	case helperModeSleep:
		runHealthHelper(t, http.StatusServiceUnavailable)
	default:
		t.Fatalf("unknown helper mode %q", mode)
	}
}

func runHealthHelper(t *testing.T, healthStatus int) {
	t.Helper()

	listenAddress := os.Getenv(resinListenAddressKey)
	port := os.Getenv(resinPortKey)
	address := net.JoinHostPort(listenAddress, port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("net.Listen(%q) error = %v", address, err)
	}
	defer listener.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(healthStatus)
		_, _ = w.Write([]byte("ok"))
	})
	server := &http.Server{Handler: mux}
	go func() {
		_ = server.Serve(listener)
	}()

	if readyFile := os.Getenv(helperReadyFileEnv); readyFile != "" {
		if err := os.WriteFile(readyFile, []byte("ready"), 0o600); err != nil {
			t.Fatalf("write ready file: %v", err)
		}
	}

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signalCh)

	sig := <-signalCh
	if signalFile := os.Getenv(helperSignalFileEnv); signalFile != "" {
		if err := os.WriteFile(signalFile, []byte(sig.String()), 0o600); err != nil {
			t.Fatalf("write signal file: %v", err)
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	_ = server.Shutdown(shutdownCtx)
	cancel()
}

func newTestBootstrap(t *testing.T) *configstore.BootstrapResult {
	t.Helper()
	bootstrap, err := configstore.Bootstrap(t.TempDir())
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	return bootstrap
}

func newTestSupervisor(t *testing.T, bootstrap *configstore.BootstrapResult, helperMode string, extraEnv map[string]string) *ProcessSupervisor {
	t.Helper()
	testBinary := testBinaryPath(t)
	port := findAvailablePort(t)
	helperEnv := map[string]string{
		helperMarkerEnv:       "1",
		helperModeEnv:         helperMode,
		resinListenAddressKey: "127.0.0.1",
		resinPortKey:          port,
	}
	for key, value := range extraEnv {
		helperEnv[key] = value
	}
	healthURL := helperHealthURL(helperEnv[resinListenAddressKey], helperEnv[resinPortKey])

	supervisor, err := New(Config{
		Bootstrap:          bootstrap,
		CoreExecutablePath: testBinary,
		Arguments:          []string{"-test.run=^TestProcessSupervisor_HelperCore$"},
		ExtraEnv:           helperEnv,
		HealthURL:          healthURL,
		StartTimeout:       2 * time.Second,
		ShutdownTimeout:    2 * time.Second,
		HealthPollInterval: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return supervisor
}

func startHelperProcess(t *testing.T, bootstrap *configstore.BootstrapResult, healthURL string, helperMode string, extraEnv map[string]string) (*exec.Cmd, <-chan error) {
	t.Helper()
	cmd := newCoreCommand(testBinaryPath(t), "-test.run=^TestProcessSupervisor_HelperCore$")
	helperEnv := map[string]string{
		helperMarkerEnv: "1",
		helperModeEnv:   helperMode,
	}
	for key, value := range extraEnv {
		helperEnv[key] = value
	}
	cmd.Env = buildEnv(os.Environ(), bootstrap.EnvList(), helperEnv)
	if err := cmd.Start(); err != nil {
		t.Fatalf("helper cmd.Start() error = %v", err)
	}
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	assertHealthzReady(t, healthURL)
	return cmd, waitCh
}

func TestNewCoreCommand_HidesWindowAndKeepsProcessGroup(t *testing.T) {
	t.Parallel()

	cmd := newCoreCommand("resin-core.exe", "serve")
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr should not be nil")
	}
	if cmd.SysProcAttr.CreationFlags != syscall.CREATE_NEW_PROCESS_GROUP {
		t.Fatalf("CreationFlags = %d, want %d", cmd.SysProcAttr.CreationFlags, syscall.CREATE_NEW_PROCESS_GROUP)
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Fatal("HideWindow should be true")
	}
}

func cleanupHelperProcess(t *testing.T, cmd *exec.Cmd, waitCh <-chan error) {
	t.Helper()
	process, err := openManagedProcess(cmd.Process.Pid, nil, nil)
	if err != nil {
		t.Fatalf("openManagedProcess() error = %v", err)
	}
	if _, err := shutdownManagedProcess(process, 2*time.Second); err != nil {
		t.Fatalf("shutdownManagedProcess() error = %v", err)
	}
	assertWaitResult(t, waitCh)
}

func assertHealthzReady(t *testing.T, rawURL string) {
	t.Helper()
	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(rawURL)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s to return 200", rawURL)
}

func assertWaitResult(t *testing.T, waitCh <-chan error) {
	t.Helper()
	select {
	case err := <-waitCh:
		if err != nil {
			t.Fatalf("helper wait error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for helper process to exit")
	}
}

func testBinaryPath(t *testing.T) string {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	return path
}

func findAvailablePort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen(127.0.0.1:0) error = %v", err)
	}
	defer listener.Close()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("net.SplitHostPort() error = %v", err)
	}
	return port
}

func helperHealthURL(listenAddress string, port string) string {
	return "http://" + net.JoinHostPort(listenAddress, port) + "/healthz"
}
