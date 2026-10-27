//go:build !windows

package main

import (
	"errors"
	"golang.org/x/term"
	"os"
)

func openShellConsole(_ string) (*shellConsole, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return nil, errors.New("请从系统终端运行 --shell；独立命令行窗口目前支持 Windows")
	}
	return &shellConsole{in: os.Stdin, out: os.Stdout}, nil
}
