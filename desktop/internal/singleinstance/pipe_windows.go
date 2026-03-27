//go:build windows

package singleinstance

import (
	"fmt"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

const (
	pipeAccessDuplex        = 0x00000003
	pipeTypeMessage         = 0x00000004
	pipeReadModeMessage     = 0x00000002
	pipeWait                = 0x00000000
	pipeRejectRemoteClients = 0x00000008
	openExisting            = 3
	genericRead             = 0x80000000
	genericWrite            = 0x40000000
	pipeBufferSize          = 512
	pipeConnectTimeoutMs    = 1000

	errorFileNotFound     syscall.Errno = 2
	errorBrokenPipe       syscall.Errno = 109
	errorMoreData         syscall.Errno = 234
	errorPipeBusy         syscall.Errno = 231
	errorPipeConnected    syscall.Errno = 535
	errorPipeNotConnected syscall.Errno = 233
	errorNoData           syscall.Errno = 232
	errorSemTimeout       syscall.Errno = 121
)

var (
	kernel32PipeProc            = syscall.NewLazyDLL("kernel32.dll")
	procCreateNamedPipeW        = kernel32PipeProc.NewProc("CreateNamedPipeW")
	procConnectNamedPipe        = kernel32PipeProc.NewProc("ConnectNamedPipe")
	procDisconnectNamedPipe     = kernel32PipeProc.NewProc("DisconnectNamedPipe")
	procCreateFileW             = kernel32PipeProc.NewProc("CreateFileW")
	procReadFile                = kernel32PipeProc.NewProc("ReadFile")
	procWriteFile               = kernel32PipeProc.NewProc("WriteFile")
	procFlushFileBuffers        = kernel32PipeProc.NewProc("FlushFileBuffers")
	procWaitNamedPipeW          = kernel32PipeProc.NewProc("WaitNamedPipeW")
	procSetNamedPipeHandleState = kernel32PipeProc.NewProc("SetNamedPipeHandleState")
)

type pipeServer struct {
	path    string
	signals chan Signal

	closed    atomic.Bool
	closeOnce sync.Once
	done      chan struct{}
}

func startPipeServer(path string, signals chan Signal) (*pipeServer, error) {
	server := &pipeServer{
		path:    path,
		signals: signals,
		done:    make(chan struct{}),
	}

	ready := make(chan error, 1)
	go server.run(ready)

	if err := <-ready; err != nil {
		return nil, err
	}
	return server, nil
}

func (s *pipeServer) Close() error {
	if s == nil {
		return nil
	}

	s.closeOnce.Do(func() {
		s.closed.Store(true)
		_, _ = dialPipeHandle(s.path)
	})

	<-s.done
	return nil
}

func (s *pipeServer) run(ready chan<- error) {
	defer close(s.done)
	defer close(s.signals)

	firstAccept := true
	for {
		handle, err := createPipeInstance(s.path)
		if err != nil {
			if firstAccept {
				ready <- err
			}
			return
		}

		if firstAccept {
			ready <- nil
			firstAccept = false
		}

		if err := connectPipeInstance(handle); err != nil {
			_ = closeWinHandle(handle)
			if s.closed.Load() {
				return
			}
			continue
		}

		if s.closed.Load() {
			_ = disconnectPipe(handle)
			_ = closeWinHandle(handle)
			return
		}

		_ = handlePipeConnection(handle, s.signals)
		_ = disconnectPipe(handle)
		_ = closeWinHandle(handle)
	}
}

func handlePipeConnection(handle syscall.Handle, signals chan<- Signal) error {
	message, err := readPipeMessage(handle)
	if err != nil {
		return fmt.Errorf("read control message: %w", err)
	}
	if message != showMainWindowCommand {
		return fmt.Errorf("unexpected control message %q", message)
	}

	signals <- SignalShowMainWindow

	if err := writePipeMessage(handle, secondInstanceReattachReply); err != nil {
		return fmt.Errorf("write control response: %w", err)
	}
	if err := flushPipe(handle); err != nil {
		return fmt.Errorf("flush control response: %w", err)
	}
	return nil
}

func exchangeControlMessage(path string, message string) (string, error) {
	handle, err := dialPipeHandle(path)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = closeWinHandle(handle)
	}()

	if err := setPipeMessageReadMode(handle); err != nil {
		return "", err
	}
	if err := writePipeMessage(handle, message); err != nil {
		return "", err
	}
	response, err := readPipeMessage(handle)
	if err != nil {
		return "", err
	}
	return response, nil
}

func createPipeInstance(path string) (syscall.Handle, error) {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, fmt.Errorf("encode pipe path: %w", err)
	}

	handle, _, callErr := procCreateNamedPipeW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		pipeAccessDuplex,
		pipeTypeMessage|pipeReadModeMessage|pipeWait|pipeRejectRemoteClients,
		1,
		pipeBufferSize,
		pipeBufferSize,
		0,
		0,
	)
	if handle == invalidHandleValue {
		return 0, fmt.Errorf("CreateNamedPipeW failed: %w", normalizeSyscallErr(callErr))
	}
	return syscall.Handle(handle), nil
}

func connectPipeInstance(handle syscall.Handle) error {
	r1, _, err := procConnectNamedPipe.Call(uintptr(handle), 0)
	if r1 != 0 {
		return nil
	}
	errno := syscallErrno(err)
	if errno == errorPipeConnected {
		return nil
	}
	return fmt.Errorf("ConnectNamedPipe failed: %w", normalizeSyscallErr(err))
}

func dialPipeHandle(path string) (syscall.Handle, error) {
	if err := waitForPipe(path, pipeConnectTimeoutMs); err != nil {
		return 0, err
	}

	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, fmt.Errorf("encode pipe path: %w", err)
	}

	handle, _, callErr := procCreateFileW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		genericRead|genericWrite,
		0,
		0,
		openExisting,
		0,
		0,
	)
	if handle == invalidHandleValue {
		return 0, fmt.Errorf("CreateFileW failed: %w", normalizeSyscallErr(callErr))
	}
	return syscall.Handle(handle), nil
}

func waitForPipe(path string, timeoutMs uint32) error {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("encode pipe path: %w", err)
	}

	r1, _, callErr := procWaitNamedPipeW.Call(uintptr(unsafe.Pointer(pathPtr)), uintptr(timeoutMs))
	if r1 != 0 {
		return nil
	}

	errno := syscallErrno(callErr)
	switch errno {
	case errorFileNotFound, errorPipeBusy, errorSemTimeout:
		return fmt.Errorf("WaitNamedPipeW failed: %w", errno)
	default:
		return fmt.Errorf("WaitNamedPipeW failed: %w", normalizeSyscallErr(callErr))
	}
}

func setPipeMessageReadMode(handle syscall.Handle) error {
	mode := uint32(pipeReadModeMessage)
	r1, _, err := procSetNamedPipeHandleState.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&mode)),
		0,
		0,
	)
	if r1 == 0 {
		return fmt.Errorf("SetNamedPipeHandleState failed: %w", normalizeSyscallErr(err))
	}
	return nil
}

func readPipeMessage(handle syscall.Handle) (string, error) {
	data := make([]byte, 0, pipeBufferSize)
	buffer := make([]byte, pipeBufferSize)

	for {
		var read uint32
		r1, _, err := procReadFile.Call(
			uintptr(handle),
			uintptr(unsafe.Pointer(&buffer[0])),
			uintptr(len(buffer)),
			uintptr(unsafe.Pointer(&read)),
			0,
		)
		if read > 0 {
			data = append(data, buffer[:read]...)
		}

		if r1 != 0 {
			break
		}

		errno := syscallErrno(err)
		switch errno {
		case errorMoreData:
			continue
		case errorBrokenPipe, errorNoData:
			if len(data) > 0 {
				break
			}
			return "", fmt.Errorf("ReadFile failed: %w", errno)
		default:
			return "", fmt.Errorf("ReadFile failed: %w", normalizeSyscallErr(err))
		}
	}

	return string(data), nil
}

func writePipeMessage(handle syscall.Handle, message string) error {
	data := []byte(message)
	var written uint32
	r1, _, err := procWriteFile.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)),
		uintptr(unsafe.Pointer(&written)),
		0,
	)
	if r1 == 0 {
		return fmt.Errorf("WriteFile failed: %w", normalizeSyscallErr(err))
	}
	if int(written) != len(data) {
		return fmt.Errorf("WriteFile wrote %d bytes, want %d", written, len(data))
	}
	return nil
}

func flushPipe(handle syscall.Handle) error {
	r1, _, err := procFlushFileBuffers.Call(uintptr(handle))
	if r1 == 0 {
		return fmt.Errorf("FlushFileBuffers failed: %w", normalizeSyscallErr(err))
	}
	return nil
}

func disconnectPipe(handle syscall.Handle) error {
	r1, _, err := procDisconnectNamedPipe.Call(uintptr(handle))
	if r1 != 0 {
		return nil
	}
	if syscallErrno(err) == errorPipeNotConnected {
		return nil
	}
	return fmt.Errorf("DisconnectNamedPipe failed: %w", normalizeSyscallErr(err))
}

func closeWinHandle(handle syscall.Handle) error {
	r1, _, err := procCloseHandle.Call(uintptr(handle))
	if r1 == 0 {
		return fmt.Errorf("CloseHandle failed: %w", normalizeSyscallErr(err))
	}
	return nil
}
