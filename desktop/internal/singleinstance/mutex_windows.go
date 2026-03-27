//go:build windows

package singleinstance

import (
	"fmt"
	"syscall"
	"unsafe"
)

const errorAlreadyExists syscall.Errno = 183

var (
	kernel32MutexProc   = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutexW    = kernel32MutexProc.NewProc("CreateMutexW")
	procReleaseMutex    = kernel32MutexProc.NewProc("ReleaseMutex")
	procCloseHandle     = kernel32MutexProc.NewProc("CloseHandle")
	procGetLastError    = kernel32MutexProc.NewProc("GetLastError")
	invalidHandleValue  = ^uintptr(0)
	zeroSecurityPointer = unsafe.Pointer(nil)
)

type namedMutex struct {
	handle syscall.Handle
	owned  bool
	closed bool
}

func acquireNamedMutex(name string) (*namedMutex, bool, error) {
	namePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, false, fmt.Errorf("encode mutex name: %w", err)
	}

	handle, _, callErr := procCreateMutexW.Call(
		uintptr(zeroSecurityPointer),
		1,
		uintptr(unsafe.Pointer(namePtr)),
	)
	if handle == 0 {
		return nil, false, fmt.Errorf("CreateMutexW failed: %w", normalizeSyscallErr(callErr))
	}

	mutex := &namedMutex{handle: syscall.Handle(handle)}
	if syscallErrno(callErr) == errorAlreadyExists {
		return mutex, false, nil
	}

	mutex.owned = true
	return mutex, true, nil
}

func (m *namedMutex) Close() error {
	if m == nil || m.closed || m.handle == 0 {
		return nil
	}

	var errs []error
	if m.owned {
		if _, _, err := procReleaseMutex.Call(uintptr(m.handle)); syscallResultFailed(err) {
			errs = append(errs, fmt.Errorf("ReleaseMutex failed: %w", normalizeSyscallErr(err)))
		}
	}
	if _, _, err := procCloseHandle.Call(uintptr(m.handle)); syscallResultFailed(err) {
		errs = append(errs, fmt.Errorf("CloseHandle failed: %w", normalizeSyscallErr(err)))
	}

	m.closed = true
	m.handle = 0
	return errorsJoin(errs)
}

func errorsJoin(errs []error) error {
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	default:
		return fmt.Errorf("%v", errs)
	}
}

func normalizeSyscallErr(err error) error {
	if syscallResultFailed(err) {
		return err
	}
	errno := lastErrno()
	if errno == 0 {
		return syscall.EINVAL
	}
	return errno
}

func lastErrno() syscall.Errno {
	r1, _, _ := procGetLastError.Call()
	return syscall.Errno(r1)
}

func syscallErrno(err error) syscall.Errno {
	if err == nil {
		return 0
	}
	errno, ok := err.(syscall.Errno)
	if !ok {
		return 0
	}
	return errno
}

func syscallResultFailed(err error) bool {
	errno := syscallErrno(err)
	return errno != 0
}
