package adb

import "testing"

func TestParseDevices(t *testing.T) {
	output := "* daemon started successfully *\nList of devices attached\nUSB123 device product:walleye model:Pixel_2 device:walleye transport_id:1\n192.168.1.20:5555 offline transport_id:2\nWAIT unauthorized usb:1-2\nDENIED no permissions (missing udev rules?)\n\n"
	devices := ParseDevices(output)
	if len(devices) != 4 {
		t.Fatalf("got %d devices: %+v", len(devices), devices)
	}
	if devices[0].Serial != "USB123" || devices[0].Model != "Pixel 2" || devices[0].Transport != "USB" {
		t.Fatalf("unexpected USB device: %+v", devices[0])
	}
	if devices[1].Transport != "网络" || devices[1].State != "offline" {
		t.Fatalf("unexpected network device: %+v", devices[1])
	}
	if devices[2].State != "unauthorized" {
		t.Fatal("unauthorized device lost")
	}
	if got := ParseDevices("List of devices attached\n\n"); got == nil || len(got) != 0 {
		t.Fatal("empty list must serialize as []")
	}
}
