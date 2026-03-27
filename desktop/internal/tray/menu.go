package tray

import "fmt"

type ActionID string

const (
	ActionShowMainWindow   ActionID = "SHOW_MAIN_WINDOW"
	ActionOpenLogDirectory ActionID = "OPEN_LOG_DIRECTORY"
	ActionExit             ActionID = "EXIT"
)

type MenuItem struct {
	ID    ActionID
	Label string
}

type Menu struct {
	Items []MenuItem
}

type Handler func(ActionID) error

type Backend interface {
	Start(Menu, Handler) error
}

type Manager struct {
	backend Backend
}

func NewManager() *Manager {
	return &Manager{backend: newSystrayBackend()}
}

func NewManagerWithBackend(backend Backend) (*Manager, error) {
	if backend == nil {
		return nil, fmt.Errorf("tray backend is required")
	}
	return &Manager{backend: backend}, nil
}

func (m *Manager) Init(menu Menu, handler func(ActionID) error) error {
	if m == nil {
		return fmt.Errorf("tray manager is nil")
	}
	if m.backend == nil {
		return fmt.Errorf("tray backend is nil")
	}
	return m.backend.Start(menu, Handler(handler))
}

func DefaultMenu() Menu {
	return Menu{
		Items: []MenuItem{
			{ID: ActionShowMainWindow, Label: "显示主窗口"},
			{ID: ActionOpenLogDirectory, Label: "查看日志目录"},
			{ID: ActionExit, Label: "退出"},
		},
	}
}
