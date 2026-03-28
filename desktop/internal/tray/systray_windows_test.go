//go:build windows

package tray

import "testing"

func TestWindowsTrayBackend_LeftClickShowsMainWindow(t *testing.T) {
	t.Parallel()

	backend := &windowsTrayBackend{}
	actions := make([]ActionID, 0, 1)
	backend.dispatchAction = func(action ActionID) {
		actions = append(actions, action)
	}
	showMenuCalls := 0
	backend.showPopupMenu = func() error {
		showMenuCalls++
		return nil
	}

	if err := backend.handleTrayNotification(wmLButtonUp); err != nil {
		t.Fatalf("handleTrayNotification(WM_LBUTTONUP) error = %v", err)
	}
	if got, want := len(actions), 1; got != want {
		t.Fatalf("dispatched actions = %d, want %d", got, want)
	}
	if got, want := actions[0], ActionShowMainWindow; got != want {
		t.Fatalf("dispatched action = %q, want %q", got, want)
	}
	if showMenuCalls != 0 {
		t.Fatalf("showPopupMenu calls = %d, want 0", showMenuCalls)
	}
}

func TestWindowsTrayBackend_RightClickShowsMenu(t *testing.T) {
	t.Parallel()

	backend := &windowsTrayBackend{}
	actions := make([]ActionID, 0, 1)
	backend.dispatchAction = func(action ActionID) {
		actions = append(actions, action)
	}
	showMenuCalls := 0
	backend.showPopupMenu = func() error {
		showMenuCalls++
		return nil
	}

	if err := backend.handleTrayNotification(wmRButtonUp); err != nil {
		t.Fatalf("handleTrayNotification(WM_RBUTTONUP) error = %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("dispatched actions = %#v, want none", actions)
	}
	if showMenuCalls != 1 {
		t.Fatalf("showPopupMenu calls = %d, want 1", showMenuCalls)
	}
}

func TestWindowsTrayBackend_MenuCommandDispatchesMappedAction(t *testing.T) {
	t.Parallel()

	backend := &windowsTrayBackend{menuActionByID: map[uint32]ActionID{2: ActionExit}}
	actions := make([]ActionID, 0, 1)
	backend.dispatchAction = func(action ActionID) {
		actions = append(actions, action)
	}

	backend.handleMenuCommand(2)

	if got, want := len(actions), 1; got != want {
		t.Fatalf("dispatched actions = %d, want %d", got, want)
	}
	if got, want := actions[0], ActionExit; got != want {
		t.Fatalf("dispatched action = %q, want %q", got, want)
	}
}

func TestShouldExitOnEndSession(t *testing.T) {
	t.Parallel()

	if shouldExitOnEndSession(0) {
		t.Fatal("shouldExitOnEndSession(0) = true, want false")
	}
	if !shouldExitOnEndSession(1) {
		t.Fatal("shouldExitOnEndSession(1) = false, want true")
	}
}
