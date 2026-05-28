//go:build windows

package termcolor

import (
	"os"
	"syscall"
	"unsafe"
)

func enableVirtualTerminal() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setConsoleMode := kernel32.NewProc("SetConsoleMode")
	getConsoleMode := kernel32.NewProc("GetConsoleMode")

	h := syscall.Handle(os.Stdout.Fd())

	var mode uint32
	getConsoleMode.Call(uintptr(h), uintptr(unsafe.Pointer(&mode)))

	// ENABLE_VIRTUAL_TERMINAL_PROCESSING = 0x0004
	setConsoleMode.Call(uintptr(h), uintptr(mode|0x0004))
}
