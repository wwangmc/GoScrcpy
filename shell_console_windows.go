//go:build windows

package main

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func openShellConsole(title string) (*shellConsole, error) {
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	_, _, _ = kernel.NewProc("AllocConsole").Call()
	_, _, _ = kernel.NewProc("SetConsoleCP").Call(65001)
	_, _, _ = kernel.NewProc("SetConsoleOutputCP").Call(65001)
	caption, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return nil, err
	}
	_, _, _ = kernel.NewProc("SetConsoleTitleW").Call(uintptr(unsafe.Pointer(caption)))
	input, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	output, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		_ = input.Close()
		return nil, err
	}
	var mode uint32
	if windows.GetConsoleMode(windows.Handle(output.Fd()), &mode) == nil {
		_ = windows.SetConsoleMode(windows.Handle(output.Fd()), mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
	}
	return &shellConsole{in: input, out: output}, nil
}
