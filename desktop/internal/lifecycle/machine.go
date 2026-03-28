package lifecycle

import (
	"errors"
	"fmt"
	"slices"
	"sync"
)

const ErrorCodeTrayInitFailed = "TRAY_INIT_FAILED"

type State string

const (
	StateBooting        State = "booting"
	StateWizard         State = "wizard"
	StateDiagnostics    State = "diagnostics"
	StateStartingCore   State = "starting-core"
	StateRunningVisible State = "running-visible"
	StateRunningTray    State = "running-tray"
	StateStopping       State = "stopping"
	StateError          State = "error"
)

type Action string

const (
	ActionNone            Action = ""
	ActionStartCore       Action = "START_CORE"
	ActionShowMainWindow  Action = "SHOW_MAIN_WINDOW"
	ActionHideToTray      Action = "HIDE_TO_TRAY"
	ActionStopCoreAndExit Action = "STOP_CORE_AND_EXIT"
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
	var lifecycleErr *Error
	if errors.As(err, &lifecycleErr) {
		return lifecycleErr.Code
	}
	return ""
}

type Transition struct {
	From   State
	To     State
	Action Action
}

type Machine struct {
	mu              sync.RWMutex
	state           State
	resumeVisibleTo State
}

func NewMachine() *Machine {
	return &Machine{state: StateBooting}
}

func (m *Machine) State() State {
	if m == nil {
		return StateError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

func (m *Machine) EnterWizard() (Transition, error) {
	return m.transition(StateWizard, ActionNone, StateBooting)
}

func (m *Machine) BeginCoreStart() (Transition, error) {
	return m.transition(StateStartingCore, ActionStartCore, StateBooting, StateWizard)
}

func (m *Machine) CoreStartedVisible() (Transition, error) {
	if m == nil {
		return Transition{}, fmt.Errorf("lifecycle machine is nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != StateStartingCore {
		return Transition{}, fmt.Errorf("cannot transition from %q to %q", m.state, StateRunningVisible)
	}
	from := m.state
	m.state = StateRunningVisible
	m.resumeVisibleTo = StateRunningVisible
	return Transition{From: from, To: StateRunningVisible, Action: ActionNone}, nil
}

func (m *Machine) RequestHideToTray() (Transition, error) {
	if m == nil {
		return Transition{}, fmt.Errorf("lifecycle machine is nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	from := m.state
	switch from {
	case StateWizard, StateDiagnostics, StateRunningVisible:
		m.resumeVisibleTo = from
		m.state = StateRunningTray
		return Transition{From: from, To: StateRunningTray, Action: ActionHideToTray}, nil
	default:
		return Transition{}, fmt.Errorf("cannot hide to tray from state %q", from)
	}
}

func (m *Machine) RequestShowMainWindow() (Transition, error) {
	if m == nil {
		return Transition{}, fmt.Errorf("lifecycle machine is nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != StateRunningTray {
		return Transition{}, fmt.Errorf("cannot show main window from state %q", m.state)
	}

	target := m.resumeVisibleTo
	if target == "" {
		target = StateRunningVisible
	}
	from := m.state
	m.state = target
	return Transition{From: from, To: target, Action: ActionShowMainWindow}, nil
}

func (m *Machine) BeginExplicitExit() (Transition, error) {
	return m.transition(
		StateStopping,
		ActionStopCoreAndExit,
		StateBooting,
		StateWizard,
		StateDiagnostics,
		StateStartingCore,
		StateRunningVisible,
		StateRunningTray,
		StateError,
	)
}

func (m *Machine) RetryFromDiagnostics() (Transition, error) {
	return m.transition(StateBooting, ActionNone, StateDiagnostics)
}

func (m *Machine) Fail(err error) error {
	if m != nil {
		m.mu.Lock()
		m.state = StateError
		m.mu.Unlock()
	}
	return err
}

func (m *Machine) FailWithCode(code string, err error) error {
	if m != nil {
		m.mu.Lock()
		m.state = StateError
		m.mu.Unlock()
	}
	return &Error{Code: code, Err: err}
}

func (m *Machine) Diagnose(code string, err error) error {
	if m != nil {
		m.mu.Lock()
		m.state = StateDiagnostics
		m.resumeVisibleTo = StateDiagnostics
		m.mu.Unlock()
	}
	return &Error{Code: code, Err: err}
}

func (m *Machine) transition(next State, action Action, allowed ...State) (Transition, error) {
	if m == nil {
		return Transition{}, fmt.Errorf("lifecycle machine is nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	from := m.state
	if slices.Contains(allowed, from) {
		m.state = next
		if next == StateWizard {
			m.resumeVisibleTo = StateWizard
		}
		return Transition{From: from, To: next, Action: action}, nil
	}
	return Transition{}, fmt.Errorf("cannot transition from %q to %q", from, next)
}
