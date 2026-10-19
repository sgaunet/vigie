//go:build windows

package render

import (
	"syscall"
	"unsafe"
)

const enableVirtualTerminalProcessing = 0x0004

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode = kernel32.NewProc("SetConsoleMode")
)

// EnableVT turns on ANSI escape processing for the console behind fd. It returns false
// when that fails, and the caller then disables colors and cursor control.
func EnableVT(fd uintptr) bool {
	var mode uint32
	if r, _, _ := procGetConsoleMode.Call(fd, uintptr(unsafe.Pointer(&mode))); r == 0 {
		return false
	}
	if mode&enableVirtualTerminalProcessing != 0 {
		return true
	}
	r, _, _ := procSetConsoleMode.Call(fd, uintptr(mode|enableVirtualTerminalProcessing))
	return r != 0
}
