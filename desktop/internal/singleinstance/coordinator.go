//go:build windows

package singleinstance

import (
	"errors"
	"fmt"
)

const (
	DefaultMutexName = `Local\ResinDesktopSingleton`
	DefaultPipePath  = `\\.\pipe\resin-desktop-control`

	ErrorCodeInstanceStaleLock = "INSTANCE_STALE_LOCK"

	showMainWindowCommand       = "SHOW_MAIN_WINDOW"
	secondInstanceReattachReply = "SECOND_INSTANCE=REATTACH"
)

type Role string

const (
	RolePrimary  Role = "PRIMARY"
	RoleReattach Role = "REATTACH"
)

type Signal string

const (
	SignalShowMainWindow Signal = showMainWindowCommand
)

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
	var instanceErr *Error
	if errors.As(err, &instanceErr) {
		return instanceErr.Code
	}
	return ""
}

type Config struct {
	MutexName string
	PipePath  string
}

type Coordinator struct {
	mutexName string
	pipePath  string
}

func New(config Config) *Coordinator {
	mutexName := config.MutexName
	if mutexName == "" {
		mutexName = DefaultMutexName
	}

	pipePath := config.PipePath
	if pipePath == "" {
		pipePath = DefaultPipePath
	}

	return &Coordinator{
		mutexName: mutexName,
		pipePath:  pipePath,
	}
}

type Session struct {
	Role Role

	signals <-chan Signal
	mutex   *namedMutex
	pipe    *pipeServer
}

func (s *Session) Signals() <-chan Signal {
	if s == nil {
		return nil
	}
	return s.signals
}

func (s *Session) Close() error {
	if s == nil {
		return nil
	}

	var errs []error
	if s.pipe != nil {
		if err := s.pipe.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.mutex != nil {
		if err := s.mutex.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (c *Coordinator) Acquire() (*Session, error) {
	mutex, created, err := acquireNamedMutex(c.mutexName)
	if err != nil {
		return nil, fmt.Errorf("acquire named mutex %q: %w", c.mutexName, err)
	}

	if !created {
		if closeErr := mutex.Close(); closeErr != nil {
			return nil, fmt.Errorf("close duplicate mutex handle: %w", closeErr)
		}

		response, err := exchangeControlMessage(c.pipePath, showMainWindowCommand)
		if err != nil {
			return nil, &Error{
				Code: ErrorCodeInstanceStaleLock,
				Err:  fmt.Errorf("control pipe unavailable while mutex %q is already owned: %w", c.mutexName, err),
			}
		}
		if response != secondInstanceReattachReply {
			return nil, &Error{
				Code: ErrorCodeInstanceStaleLock,
				Err: fmt.Errorf(
					"unexpected control response %q from %q",
					response,
					c.pipePath,
				),
			}
		}

		return &Session{Role: RoleReattach}, nil
	}

	signals := make(chan Signal, 8)
	pipe, err := startPipeServer(c.pipePath, signals)
	if err != nil {
		_ = mutex.Close()
		return nil, fmt.Errorf("start named pipe server %q: %w", c.pipePath, err)
	}

	return &Session{
		Role:    RolePrimary,
		signals: signals,
		mutex:   mutex,
		pipe:    pipe,
	}, nil
}
