package device

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"testing"
)

func TestIOSDetect(t *testing.T) {
	d := &IOSDetector{run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		switch {
		case name == "idevice_id" && args[0] == "-l":
			return []byte("00008030-AAAA\n00008030-BBBB\n"), nil
		case name == "idevice_id" && args[0] == "-n":
			return []byte("00008030-CCCC (Network)\n"), nil
		case name == "ideviceinfo":
			// paired USB device resolves a name; the other is locked;
			// the network device is addressed with a leading -n.
			udid := args[len(args)-3] // ... -u <udid> -k DeviceName
			switch udid {
			case "00008030-AAAA":
				return []byte("Dana's iPhone\n"), nil
			case "00008030-CCCC":
				return []byte("Office iPad\n"), nil
			default:
				return nil, errors.New("Could not connect to lockdownd")
			}
		}
		return nil, exec.ErrNotFound
	}}

	got, err := d.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	want := []Device{
		{ID: "00008030-AAAA", Name: "Dana's iPhone", Platform: IOS, Transport: USB, State: "device"},
		{ID: "00008030-BBBB", Name: "00008030-BBBB", Platform: IOS, Transport: USB, State: "unpaired"},
		{ID: "00008030-CCCC", Name: "Office iPad", Platform: IOS, Transport: TCP, State: "device"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Detect mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestIOSDetectMissingBinary(t *testing.T) {
	d := &IOSDetector{run: func(context.Context, string, ...string) ([]byte, error) {
		return nil, exec.ErrNotFound
	}}
	devs, err := d.Detect(context.Background())
	if err != nil {
		t.Fatalf("missing tools should not error, got %v", err)
	}
	if devs != nil {
		t.Fatalf("missing tools should yield no devices, got %+v", devs)
	}
}
