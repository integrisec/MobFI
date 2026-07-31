package device

import (
	"context"
	"errors"
	"os"
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
		{ID: "00008030-AAAA", Name: "Dana's iPhone", Platform: IOS, Transport: USB, State: "ready"},
		{ID: "00008030-BBBB", Name: "00008030-BBBB", Platform: IOS, Transport: USB, State: "unpaired"},
		{ID: "00008030-CCCC", Name: "Office iPad", Platform: IOS, Transport: TCP, State: "ready"},
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

// idevice_id exits non-zero when it cannot reach usbmuxd (e.g. Windows with no
// Apple Mobile Device Service). Detect should treat that as "no iOS devices",
// not surface it as an error.
func TestIOSDetectEnumerateFailure(t *testing.T) {
	d := &IOSDetector{run: func(context.Context, string, ...string) ([]byte, error) {
		return nil, &exec.ExitError{ProcessState: &os.ProcessState{}}
	}}
	devs, err := d.Detect(context.Background())
	if err != nil {
		t.Fatalf("enumerate failure should not error, got %v", err)
	}
	if devs != nil {
		t.Fatalf("enumerate failure should yield no devices, got %+v", devs)
	}
}

// A cancelled context must still propagate.
func TestIOSDetectContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := &IOSDetector{run: func(context.Context, string, ...string) ([]byte, error) {
		return nil, context.Canceled
	}}
	if _, err := d.Detect(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context should propagate, got %v", err)
	}
}
