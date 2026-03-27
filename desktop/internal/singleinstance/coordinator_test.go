//go:build windows

package singleinstance

import (
	"fmt"
	"testing"
	"time"
)

func TestSingleInstance_ReattachExistingShell(t *testing.T) {
	t.Parallel()

	coordinator := newTestCoordinator(t)

	primary, err := coordinator.Acquire()
	if err != nil {
		t.Fatalf("primary Acquire() error = %v", err)
	}
	defer func() {
		if err := primary.Close(); err != nil {
			t.Fatalf("primary Close() error = %v", err)
		}
	}()

	secondary, err := coordinator.Acquire()
	if err != nil {
		t.Fatalf("secondary Acquire() error = %v", err)
	}
	if secondary.Role != RoleReattach {
		t.Fatalf("secondary role = %q, want %q", secondary.Role, RoleReattach)
	}
	if secondary.Signals() != nil {
		t.Fatal("secondary Signals() should be nil")
	}
	if err := secondary.Close(); err != nil {
		t.Fatalf("secondary Close() error = %v", err)
	}

	assertSignal(t, primary.Signals(), SignalShowMainWindow)
}

func TestSingleInstance_ShowWindowSignal(t *testing.T) {
	t.Parallel()

	coordinator := newTestCoordinator(t)

	primary, err := coordinator.Acquire()
	if err != nil {
		t.Fatalf("primary Acquire() error = %v", err)
	}
	defer func() {
		if err := primary.Close(); err != nil {
			t.Fatalf("primary Close() error = %v", err)
		}
	}()

	resultCh := make(chan *Session, 1)
	errCh := make(chan error, 1)
	go func() {
		session, acquireErr := coordinator.Acquire()
		if acquireErr != nil {
			errCh <- acquireErr
			return
		}
		resultCh <- session
	}()

	assertSignal(t, primary.Signals(), SignalShowMainWindow)

	select {
	case err := <-errCh:
		t.Fatalf("secondary Acquire() error = %v", err)
	case session := <-resultCh:
		if session.Role != RoleReattach {
			t.Fatalf("secondary role = %q, want %q", session.Role, RoleReattach)
		}
		if err := session.Close(); err != nil {
			t.Fatalf("secondary Close() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for secondary Acquire() result")
	}
}

func TestSingleInstance_StaleMutexReturnsError(t *testing.T) {
	t.Parallel()

	config := newTestConfig(t)
	mutex, created, err := acquireNamedMutex(config.MutexName)
	if err != nil {
		t.Fatalf("acquireNamedMutex() error = %v", err)
	}
	if !created {
		t.Fatal("expected fresh named mutex for stale-lock test")
	}
	defer func() {
		if err := mutex.Close(); err != nil {
			t.Fatalf("mutex Close() error = %v", err)
		}
	}()

	_, err = New(config).Acquire()
	if err == nil {
		t.Fatal("Acquire() error = nil, want non-nil")
	}
	if got := ErrorCodeOf(err); got != ErrorCodeInstanceStaleLock {
		t.Fatalf("ErrorCodeOf(err) = %q, want %q (err=%v)", got, ErrorCodeInstanceStaleLock, err)
	}
}

func assertSignal(t *testing.T, signals <-chan Signal, want Signal) {
	t.Helper()

	select {
	case got, ok := <-signals:
		if !ok {
			t.Fatal("signals channel closed before wakeup signal arrived")
		}
		if got != want {
			t.Fatalf("signal = %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for signal %q", want)
	}
}

func newTestCoordinator(t *testing.T) *Coordinator {
	t.Helper()
	return New(newTestConfig(t))
}

func newTestConfig(t *testing.T) Config {
	t.Helper()

	suffix := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
	return Config{
		MutexName: `Local\ResinDesktopSingleton-` + suffix,
		PipePath:  `\\.\pipe\resin-desktop-control-` + suffix,
	}
}
