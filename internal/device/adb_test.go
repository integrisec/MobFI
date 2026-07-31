package device

import (
	"context"
	"os/exec"
	"reflect"
	"testing"
)

func TestParseADBDevices(t *testing.T) {
	out := `List of devices attached
* daemon not running; starting now at tcp:5037
* daemon started successfully
emulator-5554          device product:sdk_gphone64_arm64 model:sdk_gphone64_arm64 device:emu64a transport_id:1
0123456789ABCDEF       device usb:1-1 product:panther model:Pixel_7 device:panther transport_id:2
192.168.1.5:5555       device product:x model:Galaxy_S22 transport_id:3
R58NABCDEF             unauthorized transport_id:4

`
	got := parseADBDevices([]byte(out))
	want := []Device{
		{ID: "emulator-5554", Name: "sdk gphone64 arm64", Platform: Android, Transport: Emulator, State: "ready"},
		{ID: "0123456789ABCDEF", Name: "Pixel 7", Platform: Android, Transport: USB, State: "ready"},
		{ID: "192.168.1.5:5555", Name: "Galaxy S22", Platform: Android, Transport: TCP, Address: "192.168.1.5:5555", State: "ready"},
		{ID: "R58NABCDEF", Name: "R58NABCDEF", Platform: Android, Transport: USB, State: "unauthorized"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseADBDevices mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestADBDetectMissingBinary(t *testing.T) {
	d := &ADBDetector{run: func(context.Context, string, ...string) ([]byte, error) {
		return nil, exec.ErrNotFound
	}}
	devs, err := d.Detect(context.Background())
	if err != nil {
		t.Fatalf("adb missing should not error, got %v", err)
	}
	if devs != nil {
		t.Fatalf("adb missing should yield no devices, got %+v", devs)
	}
}
