//go:build windows

package portowner

import (
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iphlpapi            = windows.NewLazySystemDLL("iphlpapi.dll")
	getExtendedTCPTable = iphlpapi.NewProc("GetExtendedTcpTable")
)

const tcpTableOwnerPIDListener = 3 // TCP_TABLE_OWNER_PID_LISTENER

// rowLayout locates the fields listener needs inside one table row.
type rowLayout struct {
	family  uint32
	size    int // bytes per row
	portOff int // dwLocalPort
	pidOff  int // dwOwningPid
}

var layouts = []rowLayout{
	{windows.AF_INET, 24, 8, 20},   // MIB_TCPROW_OWNER_PID
	{windows.AF_INET6, 56, 20, 52}, // MIB_TCP6ROW_OWNER_PID
}

// listener walks the IPv4 then IPv6 listener tables. The API (rather than
// parsing netstat) avoids localized "LISTENING" text.
func listener(port int) (Process, bool, error) {
	for _, l := range layouts {
		table, err := tcpTable(l.family)
		if err != nil {
			return Process{}, false, err
		}
		if len(table) < 4 {
			continue
		}
		n := int(binary.LittleEndian.Uint32(table))
		for i := range n {
			start := 4 + i*l.size
			if start+l.size > len(table) {
				break
			}
			row := table[start : start+l.size]
			// dwLocalPort keeps the port in network byte order in its low bytes.
			if int(row[l.portOff])<<8|int(row[l.portOff+1]) != port {
				continue
			}
			return lookup(int(binary.LittleEndian.Uint32(row[l.pidOff:]))), true, nil
		}
	}
	return Process{}, false, nil
}

func tcpTable(family uint32) ([]byte, error) {
	size := uint32(16 << 10)
	for range 5 {
		buf := make([]byte, size)
		r, _, _ := getExtendedTCPTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)),
			0, uintptr(family), tcpTableOwnerPIDListener, 0)
		switch errno := windows.Errno(r); errno {
		case 0:
			return buf[:size], nil
		case windows.ERROR_INSUFFICIENT_BUFFER:
			continue // size now holds the required length
		default:
			return nil, fmt.Errorf("GetExtendedTcpTable: %w", errno)
		}
	}
	return nil, errors.New("GetExtendedTcpTable: table kept growing")
}

// lookup resolves a PID to its image. PID 4 (http.sys) and elevated
// processes may be unreadable; the Process then carries the PID and
// whatever name the process snapshot gives.
func lookup(pid int) Process {
	if path := imagePath(uint32(pid)); path != "" {
		return Process{PID: pid, Name: filepath.Base(path), Path: path}
	}
	if pid != 4 {
		if list, err := processes(); err == nil {
			for _, q := range list {
				if q.PID == pid {
					return q
				}
			}
		}
	}
	return Process{PID: pid}
}

func imagePath(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}

func processes() ([]Process, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("listing processes: %w", err)
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	var out []Process
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		out = append(out, Process{PID: int(e.ProcessID), Name: windows.UTF16ToString(e.ExeFile[:]), Path: imagePath(e.ProcessID)})
	}
	return out, nil
}
