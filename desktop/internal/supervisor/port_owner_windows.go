//go:build windows

package supervisor

import (
	"bytes"
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	afINET                   = 2
	tcpTableOwnerPIDListener = 3
)

type PortConflict struct {
	ListenAddress string
	Port          int
	Occupant      *PortOccupant
}

type PortOccupant struct {
	PID            uint32
	ImageName      string
	ExecutablePath string
}

type mibTCPRowOwnerPID struct {
	State      uint32
	LocalAddr  uint32
	LocalPort  [4]byte
	RemoteAddr uint32
	RemotePort [4]byte
	OwningPID  uint32
}

var procGetExtendedTCPTable = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")

func detectPortOccupant(listenAddress, port string) (*PortOccupant, error) {
	portNumber, err := strconv.Atoi(strings.TrimSpace(port))
	if err != nil {
		return nil, fmt.Errorf("parse port %q: %w", port, err)
	}

	rows, err := queryTCPListenerRows()
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		if tcpRowPort(row) != portNumber {
			continue
		}
		if !tcpRowMatchesListenAddress(row, listenAddress) {
			continue
		}

		occupant := &PortOccupant{PID: row.OwningPID}
		occupant.ExecutablePath = queryProcessExecutablePath(row.OwningPID)
		occupant.ImageName = occupant.ExecutablePath
		if occupant.ImageName != "" {
			occupant.ImageName = filepath.Base(occupant.ImageName)
		} else {
			occupant.ImageName = queryProcessImageName(row.OwningPID)
		}
		return occupant, nil
	}

	return nil, nil
}

func queryTCPListenerRows() ([]mibTCPRowOwnerPID, error) {
	var size uint32
	r0, _, callErr := procGetExtendedTCPTable.Call(
		0,
		uintptr(unsafe.Pointer(&size)),
		0,
		afINET,
		tcpTableOwnerPIDListener,
		0,
	)
	if errno := windows.Errno(r0); errno != windows.ERROR_INSUFFICIENT_BUFFER && errno != 0 {
		return nil, fmt.Errorf("probe GetExtendedTcpTable size: %w", callErr)
	}

	buffer := make([]byte, size)
	r0, _, callErr = procGetExtendedTCPTable.Call(
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&size)),
		0,
		afINET,
		tcpTableOwnerPIDListener,
		0,
	)
	if r0 != 0 {
		return nil, fmt.Errorf("query GetExtendedTcpTable: %w", callErr)
	}

	count := binary.LittleEndian.Uint32(buffer[:4])
	rowSize := binary.Size(mibTCPRowOwnerPID{})
	base := 4
	rows := make([]mibTCPRowOwnerPID, 0, count)
	for index := 0; index < int(count); index++ {
		start := base + index*rowSize
		end := start + rowSize
		if end > len(buffer) {
			return nil, fmt.Errorf("tcp listener table truncated at row %d", index)
		}
		var row mibTCPRowOwnerPID
		if err := binary.Read(bytes.NewReader(buffer[start:end]), binary.LittleEndian, &row); err != nil {
			return nil, fmt.Errorf("decode tcp listener row %d: %w", index, err)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func tcpRowPort(row mibTCPRowOwnerPID) int {
	return int(binary.BigEndian.Uint16(row.LocalPort[:2]))
}

func tcpRowMatchesListenAddress(row mibTCPRowOwnerPID, listenAddress string) bool {
	trimmed := strings.TrimSpace(listenAddress)
	if trimmed == "" || trimmed == "0.0.0.0" {
		return true
	}
	localIP := windowsIPv4String(row.LocalAddr)
	return localIP == trimmed || localIP == "0.0.0.0"
}

func windowsIPv4String(value uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d", byte(value), byte(value>>8), byte(value>>16), byte(value>>24))
}

func queryProcessExecutablePath(pid uint32) string {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(handle)

	buffer := make([]uint16, windows.MAX_PATH)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err == nil && size > 0 {
		return windows.UTF16ToString(buffer[:size])
	}

	buffer = make([]uint16, 4096)
	size = uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err == nil && size > 0 {
		return windows.UTF16ToString(buffer[:size])
	}

	return ""
}

func queryProcessImageName(pid uint32) string {
	cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	records, err := csv.NewReader(strings.NewReader(string(output))).ReadAll()
	if err != nil || len(records) == 0 || len(records[0]) == 0 {
		return ""
	}
	imageName := strings.TrimSpace(records[0][0])
	if strings.EqualFold(imageName, "INFO: No tasks are running which match the specified criteria.") {
		return ""
	}
	return imageName
}
