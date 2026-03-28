//go:build windows

package tray

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const trayReadyTimeout = 5 * time.Second

const (
	wmNull             = 0x0000
	wmCommand          = 0x0111
	wmDestroy          = 0x0002
	wmClose            = 0x0010
	wmEndSession       = 0x0016
	wmUser             = 0x0400
	wmLButtonUp        = 0x0202
	wmRButtonUp        = 0x0205
	swHide             = 0
	cwUseDefault       = 0x80000000
	wsOverlappedWindow = 0x00CF0000
)

var (
	kernel32                  = windows.NewLazySystemDLL("Kernel32.dll")
	shell32                   = windows.NewLazySystemDLL("Shell32.dll")
	user32                    = windows.NewLazySystemDLL("User32.dll")
	procGetModuleHandle       = kernel32.NewProc("GetModuleHandleW")
	procShellNotifyIcon       = shell32.NewProc("Shell_NotifyIconW")
	procCreatePopupMenu       = user32.NewProc("CreatePopupMenu")
	procCreateWindowEx        = user32.NewProc("CreateWindowExW")
	procDefWindowProc         = user32.NewProc("DefWindowProcW")
	procDestroyIcon           = user32.NewProc("DestroyIcon")
	procDestroyMenu           = user32.NewProc("DestroyMenu")
	procDestroyWindow         = user32.NewProc("DestroyWindow")
	procDispatchMessage       = user32.NewProc("DispatchMessageW")
	procGetCursorPos          = user32.NewProc("GetCursorPos")
	procGetMessage            = user32.NewProc("GetMessageW")
	procInsertMenuItem        = user32.NewProc("InsertMenuItemW")
	procLoadCursor            = user32.NewProc("LoadCursorW")
	procLoadIcon              = user32.NewProc("LoadIconW")
	procLoadImage             = user32.NewProc("LoadImageW")
	procPostMessage           = user32.NewProc("PostMessageW")
	procPostQuitMessage       = user32.NewProc("PostQuitMessage")
	procRegisterClassEx       = user32.NewProc("RegisterClassExW")
	procRegisterWindowMessage = user32.NewProc("RegisterWindowMessageW")
	procSetForegroundWindow   = user32.NewProc("SetForegroundWindow")
	procShowWindow            = user32.NewProc("ShowWindow")
	procTrackPopupMenu        = user32.NewProc("TrackPopupMenu")
	procTranslateMessage      = user32.NewProc("TranslateMessage")
	procUnregisterClass       = user32.NewProc("UnregisterClassW")
	procUpdateWindow          = user32.NewProc("UpdateWindow")
)

type trayWndClassEx struct {
	Size, Style                        uint32
	WndProc                            uintptr
	ClsExtra, WndExtra                 int32
	Instance, Icon, Cursor, Background windows.Handle
	MenuName, ClassName                *uint16
	IconSm                             windows.Handle
}

func (w *trayWndClassEx) register() error {
	w.Size = uint32(unsafe.Sizeof(*w))
	res, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(w)))
	if res == 0 {
		return err
	}
	return nil
}

func (w *trayWndClassEx) unregister() error {
	if w == nil || w.ClassName == nil || w.Instance == 0 {
		return nil
	}
	res, _, err := procUnregisterClass.Call(uintptr(unsafe.Pointer(w.ClassName)), uintptr(w.Instance))
	if res == 0 {
		return err
	}
	return nil
}

type trayNotifyIconData struct {
	Size                       uint32
	Wnd                        windows.Handle
	ID, Flags, CallbackMessage uint32
	Icon                       windows.Handle
	Tip                        [128]uint16
	State, StateMask           uint32
	Info                       [256]uint16
	Timeout, Version           uint32
	InfoTitle                  [64]uint16
	InfoFlags                  uint32
	GuidItem                   windows.GUID
	BalloonIcon                windows.Handle
}

func (nid *trayNotifyIconData) add() error {
	const nimAdd = 0x00000000
	res, _, err := procShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(nid)))
	if res == 0 {
		return err
	}
	return nil
}

func (nid *trayNotifyIconData) delete() error {
	const nimDelete = 0x00000002
	res, _, err := procShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(nid)))
	if res == 0 {
		return err
	}
	return nil
}

type trayMenuItemInfo struct {
	Size, Mask, Type, State     uint32
	ID                          uint32
	SubMenu, Checked, Unchecked windows.Handle
	ItemData                    uintptr
	TypeData                    *uint16
	Cch                         uint32
	BMPItem                     windows.Handle
}

type trayPoint struct {
	X, Y int32
}

type windowsTrayBackend struct {
	once           sync.Once
	handler        Handler
	menu           Menu
	menuActionByID map[uint32]ActionID

	instance       windows.Handle
	icon           windows.Handle
	cursor         windows.Handle
	window         windows.Handle
	popupMenu      windows.Handle
	notifyIcon     *trayNotifyIconData
	windowClass    *trayWndClassEx
	callbackMsg    uint32
	taskbarCreated uint32
	customIcon     bool

	dispatchAction func(ActionID)
	showPopupMenu  func() error
	cleanupOnce    sync.Once
}

func newSystrayBackend() Backend {
	return &windowsTrayBackend{}
}

func (b *windowsTrayBackend) Start(menu Menu, handler Handler) error {
	if handler == nil {
		return fmt.Errorf("tray handler is required")
	}
	readyCh := make(chan struct{})
	startErrCh := make(chan error, 1)

	b.once.Do(func() {
		go b.run(menu, handler, readyCh, startErrCh)
	})

	select {
	case <-readyCh:
		return nil
	case err := <-startErrCh:
		return err
	case <-time.After(trayReadyTimeout):
		return fmt.Errorf("tray backend did not become ready within %s", trayReadyTimeout)
	}
}

func (b *windowsTrayBackend) run(menu Menu, handler Handler, readyCh chan struct{}, startErrCh chan error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer func() {
		if recoverValue := recover(); recoverValue != nil {
			b.cleanup(true)
			select {
			case startErrCh <- fmt.Errorf("tray backend panicked: %v", recoverValue):
			default:
			}
		}
	}()

	b.handler = handler
	b.menu = menu
	b.dispatchAction = func(action ActionID) {
		go func() {
			_ = b.handler(action)
		}()
	}
	b.menuActionByID = make(map[uint32]ActionID, len(menu.Items))

	if err := b.initInstance(); err != nil {
		b.cleanup(true)
		startErrCh <- err
		return
	}
	if err := b.createMenu(menu); err != nil {
		b.cleanup(true)
		startErrCh <- err
		return
	}
	b.showPopupMenu = b.showMenu

	close(readyCh)
	b.nativeLoop()
}

func (b *windowsTrayBackend) initInstance() error {
	const (
		idiApplication = 32512
		idcArrow       = 32512
		csHRedraw      = 0x0002
		csVRedraw      = 0x0001
		nifMessage     = 0x00000001
		nifIcon        = 0x00000002
		nifTip         = 0x00000004
		imageIcon      = 1
		lrLoadFromFile = 0x00000010
		lrDefaultSize  = 0x00000040
	)

	b.callbackMsg = wmUser + 1
	taskbarEventNamePtr, _ := windows.UTF16PtrFromString("TaskbarCreated")
	res, _, err := procRegisterWindowMessage.Call(uintptr(unsafe.Pointer(taskbarEventNamePtr)))
	b.taskbarCreated = uint32(res)

	instanceHandle, _, err := procGetModuleHandle.Call(0)
	if instanceHandle == 0 {
		return err
	}
	b.instance = windows.Handle(instanceHandle)

	iconHandle, _, err := procLoadIcon.Call(0, idiApplication)
	if iconHandle == 0 {
		return err
	}
	b.icon = windows.Handle(iconHandle)

	cursorHandle, _, err := procLoadCursor.Call(0, idcArrow)
	if cursorHandle == 0 {
		return err
	}
	b.cursor = windows.Handle(cursorHandle)

	classNamePtr, err := windows.UTF16PtrFromString("ResinDesktopTrayWindow")
	if err != nil {
		return err
	}
	windowNamePtr, err := windows.UTF16PtrFromString("")
	if err != nil {
		return err
	}

	b.windowClass = &trayWndClassEx{
		Style:      csHRedraw | csVRedraw,
		WndProc:    windows.NewCallback(b.wndProc),
		Instance:   b.instance,
		Icon:       b.icon,
		Cursor:     b.cursor,
		Background: windows.Handle(6),
		ClassName:  classNamePtr,
		IconSm:     b.icon,
	}
	if err := b.windowClass.register(); err != nil {
		return err
	}

	windowHandle, _, err := procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(classNamePtr)),
		uintptr(unsafe.Pointer(windowNamePtr)),
		wsOverlappedWindow,
		cwUseDefault,
		cwUseDefault,
		cwUseDefault,
		cwUseDefault,
		0,
		0,
		uintptr(b.instance),
		0,
	)
	if windowHandle == 0 {
		_ = b.windowClass.unregister()
		return err
	}
	b.window = windows.Handle(windowHandle)

	procShowWindow.Call(uintptr(b.window), swHide)
	procUpdateWindow.Call(uintptr(b.window))

	trayIconPath, err := iconBytesToFilePath(resinTrayIcon)
	if err != nil {
		return err
	}
	trayIconPathPtr, err := windows.UTF16PtrFromString(trayIconPath)
	if err != nil {
		return err
	}
	trayIconHandle, _, err := procLoadImage.Call(
		0,
		uintptr(unsafe.Pointer(trayIconPathPtr)),
		imageIcon,
		0,
		0,
		lrLoadFromFile|lrDefaultSize,
	)
	if trayIconHandle == 0 {
		return err
	}
	b.icon = windows.Handle(trayIconHandle)
	b.customIcon = true

	b.notifyIcon = &trayNotifyIconData{
		Wnd:             b.window,
		ID:              100,
		Flags:           nifMessage | nifIcon | nifTip,
		CallbackMessage: b.callbackMsg,
		Icon:            b.icon,
	}
	b.notifyIcon.Size = uint32(unsafe.Sizeof(*b.notifyIcon))
	tip, err := windows.UTF16FromString("Resin Desktop")
	if err != nil {
		return err
	}
	copy(b.notifyIcon.Tip[:], tip)
	if err := b.notifyIcon.add(); err != nil {
		return err
	}

	return nil
}

func (b *windowsTrayBackend) createMenu(menu Menu) error {
	const (
		miimFType  = 0x00000100
		miimString = 0x00000040
		miimID     = 0x00000002
		miimState  = 0x00000001
		mftString  = 0x00000000
	)

	menuHandle, _, err := procCreatePopupMenu.Call()
	if menuHandle == 0 {
		return err
	}
	b.popupMenu = windows.Handle(menuHandle)

	for index, item := range menu.Items {
		menuID := uint32(index + 1)
		b.menuActionByID[menuID] = item.ID
		titlePtr, err := windows.UTF16PtrFromString(item.Label)
		if err != nil {
			return err
		}
		menuItem := trayMenuItemInfo{
			Mask:     miimFType | miimString | miimID | miimState,
			Type:     mftString,
			ID:       menuID,
			TypeData: titlePtr,
			Cch:      uint32(len(item.Label)),
		}
		menuItem.Size = uint32(unsafe.Sizeof(menuItem))
		res, _, err := procInsertMenuItem.Call(
			uintptr(b.popupMenu),
			uintptr(index),
			1,
			uintptr(unsafe.Pointer(&menuItem)),
		)
		if res == 0 {
			return err
		}
	}

	return nil
}

func (b *windowsTrayBackend) wndProc(hWnd windows.Handle, message uint32, wParam, lParam uintptr) (lResult uintptr) {
	switch message {
	case wmCommand:
		b.handleMenuCommand(uint32(wParam))
	case wmClose:
		if b.window != 0 {
			window := b.window
			b.window = 0
			procDestroyWindow.Call(uintptr(window))
		}
	case wmDestroy:
		b.cleanup(false)
		procPostQuitMessage.Call(0)
	case wmEndSession:
		if shouldExitOnEndSession(wParam) {
			b.cleanup(false)
			procPostQuitMessage.Call(0)
		}
	case b.callbackMsg:
		if err := b.handleTrayNotification(uint32(lParam)); err != nil {
			return 0
		}
	case b.taskbarCreated:
		if b.notifyIcon != nil {
			_ = b.notifyIcon.add()
		}
	default:
		lResult, _, _ = procDefWindowProc.Call(uintptr(hWnd), uintptr(message), wParam, lParam)
	}
	return
}

func (b *windowsTrayBackend) handleTrayNotification(code uint32) error {
	switch code {
	case wmLButtonUp:
		b.dispatch(ActionShowMainWindow)
		return nil
	case wmRButtonUp:
		if b.showPopupMenu == nil {
			return fmt.Errorf("tray popup menu handler is not configured")
		}
		return b.showPopupMenu()
	default:
		return nil
	}
}

func (b *windowsTrayBackend) handleMenuCommand(menuID uint32) {
	action, ok := b.menuActionByID[menuID]
	if !ok {
		return
	}
	b.dispatch(action)
}

func (b *windowsTrayBackend) dispatch(action ActionID) {
	if b.dispatchAction != nil {
		b.dispatchAction(action)
	}
}

func (b *windowsTrayBackend) showMenu() error {
	const (
		tpmBottomAlign = 0x0020
		tpmLeftAlign   = 0x0000
	)
	if b.popupMenu == 0 {
		return fmt.Errorf("tray popup menu is not initialized")
	}
	point := trayPoint{}
	res, _, err := procGetCursorPos.Call(uintptr(unsafe.Pointer(&point)))
	if res == 0 {
		return err
	}
	procSetForegroundWindow.Call(uintptr(b.window))
	res, _, err = procTrackPopupMenu.Call(
		uintptr(b.popupMenu),
		tpmBottomAlign|tpmLeftAlign,
		uintptr(point.X),
		uintptr(point.Y),
		0,
		uintptr(b.window),
		0,
	)
	procPostMessage.Call(uintptr(b.window), wmNull, 0, 0)
	if res == 0 {
		return err
	}
	return nil
}

func (b *windowsTrayBackend) nativeLoop() {
	message := &struct {
		WindowHandle windows.Handle
		Message      uint32
		Wparam       uintptr
		Lparam       uintptr
		Time         uint32
		Pt           trayPoint
	}{}
	for {
		ret, _, err := procGetMessage.Call(uintptr(unsafe.Pointer(message)), 0, 0, 0)
		switch int32(ret) {
		case -1:
			_ = err
			b.cleanup(true)
			procPostQuitMessage.Call(0)
			return
		case 0:
			return
		default:
			_ = err
			procTranslateMessage.Call(uintptr(unsafe.Pointer(message)))
			procDispatchMessage.Call(uintptr(unsafe.Pointer(message)))
		}
	}
}

func (b *windowsTrayBackend) cleanup(destroyWindow bool) {
	b.cleanupOnce.Do(func() {
		if destroyWindow && b.window != 0 {
			window := b.window
			b.window = 0
			procDestroyWindow.Call(uintptr(window))
		}
		if b.notifyIcon != nil {
			_ = b.notifyIcon.delete()
			b.notifyIcon = nil
		}
		if b.popupMenu != 0 {
			procDestroyMenu.Call(uintptr(b.popupMenu))
			b.popupMenu = 0
		}
		if b.customIcon && b.icon != 0 {
			procDestroyIcon.Call(uintptr(b.icon))
			b.icon = 0
			b.customIcon = false
		}
		if b.windowClass != nil {
			_ = b.windowClass.unregister()
			b.windowClass = nil
		}
	})
}

func shouldExitOnEndSession(wParam uintptr) bool {
	return wParam != 0
}

func iconBytesToFilePath(iconBytes []byte) (string, error) {
	hash := md5.Sum(iconBytes)
	iconFilePath := filepath.Join(os.TempDir(), "resin_tray_icon_"+hex.EncodeToString(hash[:])+".ico")
	if _, err := os.Stat(iconFilePath); err == nil {
		return iconFilePath, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.WriteFile(iconFilePath, iconBytes, 0o644); err != nil {
		return "", err
	}
	return iconFilePath, nil
}
