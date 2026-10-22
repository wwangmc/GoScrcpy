package main

import "phonescreen/internal/scrcpy"

// A session has one video reader and one ordered control writer regardless of device type.
type mirrorSession interface {
	SerialNumber() string
	Info() scrcpy.Metadata
	Encoding() string
	Controls() []string
	Settings() []string
	PointerIntervalMS() int
	ReadFrame() ([]byte, error)
	WriteControl([]byte) error
	Claim() bool
	Logs() string
	Close()
	DoneChannel() <-chan struct{}
}
