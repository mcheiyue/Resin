//go:build windows

package supervisor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Resinat/Resin/desktop/internal/configstore"
)

const (
	ModeStartedCore     StartMode = "STARTED_CORE"
	ModeReattachCore    StartMode = "REATTACH_CORE"
	forceKilledEnvKey             = "CORE_FORCE_KILLED"
	forceKilledEnvValue           = "TRUE"

	ErrorCodeCoreStartTimeout     = "CORE_START_TIMEOUT"
	ErrorCodePortInUse            = "PORT_IN_USE"
	ErrorCodeCoreRecoveryConflict = "CORE_RECOVERY_CONFLICT"
	ErrorCodeCoreProcessExited    = "CORE_PROCESS_EXITED"

	defaultHealthPath      = "/healthz"
	defaultStartTimeout    = 15 * time.Second
	defaultShutdownTimeout = 5 * time.Second
	defaultPollInterval    = 100 * time.Millisecond
	defaultProbeTimeout    = 300 * time.Millisecond

	stateFileName = "core-supervisor-state.json"
	stateVersion  = 1

	resinListenAddressKey = "RESIN_LISTEN_ADDRESS"
	resinPortKey          = "RESIN_PORT"
)

type StartMode string

type Error struct {
	Code string
	Err  error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.Code
	}
	return fmt.Sprintf("%s: %v", e.Code, e.Err)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func ErrorCodeOf(err error) string {
	var supervisorErr *Error
	if errors.As(err, &supervisorErr) {
		return supervisorErr.Code
	}
	return ""
}

type Config struct {
	Bootstrap          *configstore.BootstrapResult
	CoreExecutablePath string
	Arguments          []string
	ExtraEnv           map[string]string
	HealthURL          string
	StartTimeout       time.Duration
	ShutdownTimeout    time.Duration
	HealthPollInterval time.Duration
	HTTPClient         *http.Client
}

type StartResult struct {
	Mode      StartMode
	PID       int
	HealthURL string
}

type ShutdownResult struct {
	ForceKilled bool
	ExposedEnv  map[string]string
}

type ProcessSupervisor struct {
	config resolvedConfig

	mu      sync.Mutex
	process *managedProcess
}

type resolvedConfig struct {
	bootstrap       *configstore.BootstrapResult
	corePath        string
	arguments       []string
	extraEnv        map[string]string
	healthURL       string
	startTimeout    time.Duration
	shutdownTimeout time.Duration
	pollInterval    time.Duration
	httpClient      *http.Client
	statePath       string
	fingerprint     string
	listenAddr      string
	port            string
}

type managedProcess struct {
	pid    int
	handle syscall.Handle
	cmd    *exec.Cmd
	waitCh <-chan error
}

type persistedState struct {
	Version        int    `json:"version"`
	PID            int    `json:"pid"`
	Fingerprint    string `json:"fingerprint"`
	ExecutablePath string `json:"executable_path"`
	HealthURL      string `json:"health_url"`
}

func New(config Config) (*ProcessSupervisor, error) {
	if config.Bootstrap == nil {
		return nil, fmt.Errorf("bootstrap result is required")
	}

	corePath := strings.TrimSpace(config.CoreExecutablePath)
	if corePath == "" {
		corePath = filepath.Join(config.Bootstrap.Layout.RootDir, "resin-core.exe")
	}

	startTimeout := config.StartTimeout
	if startTimeout <= 0 {
		startTimeout = defaultStartTimeout
	}

	shutdownTimeout := config.ShutdownTimeout
	if shutdownTimeout <= 0 {
		shutdownTimeout = defaultShutdownTimeout
	}

	pollInterval := config.HealthPollInterval
	if pollInterval <= 0 {
		pollInterval = defaultPollInterval
	}

	healthURL := strings.TrimSpace(config.HealthURL)

	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultProbeTimeout}
	}

	envMap := effectiveResinEnvMap(config.Bootstrap.EnvMap(), config.ExtraEnv)
	listenAddress := strings.TrimSpace(envMap[resinListenAddressKey])
	if listenAddress == "" {
		listenAddress = "127.0.0.1"
	}
	port := strings.TrimSpace(envMap[resinPortKey])
	if port == "" {
		port = "2260"
	}
	if healthURL == "" {
		healthURL = "http://" + net.JoinHostPort(listenAddress, port) + defaultHealthPath
	}

	return &ProcessSupervisor{
		config: resolvedConfig{
			bootstrap:       config.Bootstrap,
			corePath:        corePath,
			arguments:       append([]string(nil), config.Arguments...),
			extraEnv:        cloneMap(config.ExtraEnv),
			healthURL:       healthURL,
			startTimeout:    startTimeout,
			shutdownTimeout: shutdownTimeout,
			pollInterval:    pollInterval,
			httpClient:      httpClient,
			statePath:       filepath.Join(config.Bootstrap.Layout.DesktopDir, stateFileName),
			fingerprint:     fingerprintEnvMap(envMap),
			listenAddr:      listenAddress,
			port:            port,
		},
	}, nil
}

func (s *ProcessSupervisor) Start(ctx context.Context) (*StartResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.process != nil {
		return &StartResult{
			Mode:      ModeStartedCore,
			PID:       s.process.pid,
			HealthURL: s.config.healthURL,
		}, nil
	}

	ready, err := s.healthzReady(ctx)
	if err == nil && ready {
		state, stateErr := readStateFile(s.config.statePath)
		if stateErr == nil && state.Fingerprint == s.config.fingerprint && state.PID > 0 {
			process, openErr := openManagedProcess(state.PID, nil, nil)
			if openErr == nil {
				s.process = process
				return &StartResult{
					Mode:      ModeReattachCore,
					PID:       process.pid,
					HealthURL: s.config.healthURL,
				}, nil
			}
		}

		return nil, &Error{
			Code: ErrorCodeCoreRecoveryConflict,
			Err:  fmt.Errorf("healthy core already bound to %s without matching supervisor fingerprint", s.config.healthURL),
		}
	}

	portAvailable, portErr := s.portAvailable()
	if portErr != nil {
		return nil, fmt.Errorf("probe fixed core port availability: %w", portErr)
	}
	if !portAvailable {
		return nil, &Error{
			Code: ErrorCodePortInUse,
			Err:  fmt.Errorf("fixed core port %s is already occupied", s.listenAddress()),
		}
	}

	cmd := exec.Command(s.config.corePath, s.config.arguments...)
	cmd.Env = buildEnv(os.Environ(), s.config.bootstrap.EnvList(), s.config.extraEnv)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start core executable %q: %w", s.config.corePath, err)
	}

	process, err := openManagedProcess(cmd.Process.Pid, cmd, waitForCommand(cmd))
	if err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return nil, fmt.Errorf("open started core process handle: %w", err)
	}

	state := persistedState{
		Version:        stateVersion,
		PID:            process.pid,
		Fingerprint:    s.config.fingerprint,
		ExecutablePath: s.config.corePath,
		HealthURL:      s.config.healthURL,
	}
	if err := writeStateFile(s.config.statePath, state); err != nil {
		_, shutdownErr := shutdownManagedProcess(process, s.config.shutdownTimeout)
		return nil, errors.Join(fmt.Errorf("persist supervisor state: %w", err), shutdownErr)
	}

	result, err := s.waitUntilReady(ctx, process)
	if err != nil {
		_ = os.Remove(s.config.statePath)
		return nil, err
	}

	s.process = process
	return result, nil
}

func (s *ProcessSupervisor) Shutdown(ctx context.Context) (*ShutdownResult, error) {
	s.mu.Lock()
	process := s.process
	s.process = nil
	s.mu.Unlock()

	if process == nil {
		_ = os.Remove(s.config.statePath)
		return &ShutdownResult{}, nil
	}

	result, err := shutdownManagedProcess(process, boundedTimeout(ctx, s.config.shutdownTimeout))
	removeErr := os.Remove(s.config.statePath)
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		err = errors.Join(err, fmt.Errorf("remove supervisor state: %w", removeErr))
	}
	return result, err
}

func (s *ProcessSupervisor) waitUntilReady(ctx context.Context, process *managedProcess) (*StartResult, error) {
	deadlineCtx, cancel := context.WithTimeout(ctx, s.config.startTimeout)
	defer cancel()

	ticker := time.NewTicker(s.config.pollInterval)
	defer ticker.Stop()

	for {
		ready, err := s.healthzReady(deadlineCtx)
		if err == nil && ready {
			return &StartResult{
				Mode:      ModeStartedCore,
				PID:       process.pid,
				HealthURL: s.config.healthURL,
			}, nil
		}

		select {
		case waitErr := <-process.waitCh:
			portAvailable, portErr := s.portAvailable()
			if portErr == nil && !portAvailable {
				return nil, &Error{
					Code: ErrorCodePortInUse,
					Err:  fmt.Errorf("core exited before ready and fixed port %s remained occupied: %w", s.listenAddress(), waitErr),
				}
			}
			return nil, &Error{
				Code: ErrorCodeCoreProcessExited,
				Err:  fmt.Errorf("core exited before /healthz returned 200: %w", waitErr),
			}
		case <-deadlineCtx.Done():
			_, shutdownErr := shutdownManagedProcess(process, s.config.shutdownTimeout)
			return nil, &Error{
				Code: ErrorCodeCoreStartTimeout,
				Err:  errors.Join(fmt.Errorf("timed out waiting for %s", s.config.healthURL), shutdownErr),
			}
		case <-ticker.C:
		}
	}
}

func (s *ProcessSupervisor) healthzReady(ctx context.Context) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.config.healthURL, nil)
	if err != nil {
		return false, fmt.Errorf("build health request: %w", err)
	}

	resp, err := s.config.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK, nil
}

func (s *ProcessSupervisor) portAvailable() (bool, error) {
	listener, err := net.Listen("tcp", s.listenAddress())
	if err == nil {
		_ = listener.Close()
		return true, nil
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return false, nil
	}
	return false, err
}

func (s *ProcessSupervisor) listenAddress() string {
	return net.JoinHostPort(s.config.listenAddr, s.config.port)
}

func buildEnv(base []string, overlay []string, extra map[string]string) []string {
	merged := make(map[string]string, len(base)+len(overlay)+len(extra))
	for _, entry := range base {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		merged[key] = value
	}
	for _, entry := range overlay {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		merged[key] = value
	}
	for key, value := range extra {
		merged[key] = value
	}

	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+merged[key])
	}
	return env
}

func effectiveResinEnvMap(base map[string]string, extra map[string]string) map[string]string {
	merged := cloneMap(base)
	if merged == nil {
		merged = make(map[string]string)
	}
	for key, value := range extra {
		if strings.HasPrefix(key, "RESIN_") {
			merged[key] = value
		}
	}
	return merged
}

func fingerprintEnvMap(envMap map[string]string) string {
	keys := make([]string, 0, len(envMap))
	for key := range envMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	hash := sha256.New()
	for _, key := range keys {
		_, _ = hash.Write([]byte(key))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(envMap[key]))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func cloneMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func writeStateFile(path string, state persistedState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("ensure state directory: %w", err)
	}

	payload, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal supervisor state: %w", err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return fmt.Errorf("write supervisor state file: %w", err)
	}
	return nil
}

func readStateFile(path string) (*persistedState, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var state persistedState
	if err := json.Unmarshal(payload, &state); err != nil {
		return nil, fmt.Errorf("decode supervisor state file: %w", err)
	}
	if state.Version != stateVersion {
		return nil, fmt.Errorf("unsupported supervisor state version %d", state.Version)
	}
	return &state, nil
}

func waitForCommand(cmd *exec.Cmd) <-chan error {
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()
	return waitCh
}

func openManagedProcess(pid int, cmd *exec.Cmd, waitCh <-chan error) (*managedProcess, error) {
	handle, err := syscall.OpenProcess(syscall.SYNCHRONIZE|syscall.PROCESS_TERMINATE|syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return nil, fmt.Errorf("open process %d: %w", pid, err)
	}
	if waitCh == nil {
		closed := make(chan error)
		close(closed)
		waitCh = closed
	}
	return &managedProcess{
		pid:    pid,
		handle: handle,
		cmd:    cmd,
		waitCh: waitCh,
	}, nil
}

func shutdownManagedProcess(process *managedProcess, timeout time.Duration) (*ShutdownResult, error) {
	if process == nil {
		return &ShutdownResult{}, nil
	}
	defer process.closeHandle()

	var shutdownErr error
	if err := generateConsoleCtrlEvent(syscall.CTRL_BREAK_EVENT, uint32(process.pid)); err != nil {
		shutdownErr = errors.Join(shutdownErr, fmt.Errorf("send CTRL_BREAK_EVENT to process group %d: %w", process.pid, err))
	}

	exited, waitErr := process.waitForExit(timeout)
	if waitErr != nil {
		shutdownErr = errors.Join(shutdownErr, waitErr)
	}
	if exited {
		return &ShutdownResult{}, shutdownErr
	}

	if err := syscall.TerminateProcess(process.handle, 1); err != nil {
		return nil, errors.Join(shutdownErr, fmt.Errorf("force terminate process %d: %w", process.pid, err))
	}

	_, finalWaitErr := process.waitForExit(1 * time.Second)
	shutdownErr = errors.Join(shutdownErr, finalWaitErr)

	return &ShutdownResult{
		ForceKilled: true,
		ExposedEnv: map[string]string{
			forceKilledEnvKey: forceKilledEnvValue,
		},
	}, shutdownErr
}

func (p *managedProcess) waitForExit(timeout time.Duration) (bool, error) {
	if p == nil {
		return true, nil
	}
	if timeout <= 0 {
		timeout = defaultShutdownTimeout
	}

	waitMilliseconds := timeout / time.Millisecond
	if waitMilliseconds > time.Duration(^uint32(0)>>1) {
		waitMilliseconds = time.Duration(^uint32(0) >> 1)
	}

	status, err := syscall.WaitForSingleObject(p.handle, uint32(waitMilliseconds))
	if err != nil {
		return false, fmt.Errorf("wait for process %d exit: %w", p.pid, err)
	}
	switch status {
	case syscall.WAIT_OBJECT_0:
		return true, nil
	case syscall.WAIT_TIMEOUT:
		return false, nil
	default:
		return false, fmt.Errorf("wait for process %d returned unexpected status %d", p.pid, status)
	}
}

func (p *managedProcess) closeHandle() {
	if p == nil || p.handle == 0 {
		return
	}
	_ = syscall.CloseHandle(p.handle)
	p.handle = 0
}

func boundedTimeout(ctx context.Context, fallback time.Duration) time.Duration {
	if fallback <= 0 {
		fallback = defaultShutdownTimeout
	}
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return time.Millisecond
		}
		if remaining < fallback {
			return remaining
		}
	}
	return fallback
}

func generateConsoleCtrlEvent(event uint32, processGroupID uint32) error {
	kernel32, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return fmt.Errorf("load kernel32.dll: %w", err)
	}
	defer kernel32.Release()

	proc, err := kernel32.FindProc("GenerateConsoleCtrlEvent")
	if err != nil {
		return fmt.Errorf("find GenerateConsoleCtrlEvent: %w", err)
	}

	result, _, callErr := proc.Call(uintptr(event), uintptr(processGroupID))
	if result == 0 {
		return callErr
	}
	return nil
}
