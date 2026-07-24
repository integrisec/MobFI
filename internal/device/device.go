// Package device models Android/iOS devices and discovers them across
// transports. Detectors are pluggable: register new mechanisms (adb, USB,
// emulators, TCP bridges, libimobiledevice) with a Registry.
package device

import (
	"context"
	"errors"
	"fmt"
)

// Platform identifies the mobile OS family of a device.
type Platform string

const (
	Android Platform = "android"
	IOS     Platform = "ios"
)

// Transport names how MFI reached a device.
type Transport string

const (
	USB       Transport = "usb"
	TCP       Transport = "tcp"
	Emulator  Transport = "emulator"
	Simulator Transport = "simulator" // iOS Simulator managed by `xcrun simctl`
)

// Device is a single Android/iOS target seen by a detector.
type Device struct {
	ID        string    `json:"id"`   // stable identifier (adb serial, iOS UDID, ...)
	Name      string    `json:"name"` // human-friendly label
	Platform  Platform  `json:"platform"`
	Transport Transport `json:"transport"`
	Address   string    `json:"address,omitempty"` // host:port for TCP transports
	State     string    `json:"state"`             // "device", "offline", "unauthorized", ...
}

// Detector discovers devices reachable via one mechanism. Implementations
// must be safe to call repeatedly and must not block indefinitely.
type Detector interface {
	// Name identifies the detector (e.g. "adb", "libimobiledevice").
	Name() string
	// Detect returns the devices currently reachable via this mechanism.
	Detect(ctx context.Context) ([]Device, error)
}

// Registry holds the active detectors.
type Registry struct {
	detectors []Detector
}

// DefaultRegistry returns the registry with all built-in detectors.
// TODO: also register a TCP-bridge detector.
func DefaultRegistry() *Registry {
	r := &Registry{}
	r.Add(NewADBDetector())
	r.Add(NewIOSDetector())
	r.Add(NewSimctlDetector())
	return r
}

// Add registers a detector.
func (r *Registry) Add(d Detector) { r.detectors = append(r.detectors, d) }

// DetectAll runs every detector and merges their results. A detector that
// fails does not suppress the others: its error is collected and the
// devices found by the remaining detectors are still returned, alongside a
// joined error naming the detectors that failed.
func (r *Registry) DetectAll(ctx context.Context) ([]Device, error) {
	var (
		out  []Device
		errs []error
	)
	for _, d := range r.detectors {
		found, err := d.Detect(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", d.Name(), err))
			continue
		}
		out = append(out, found...)
	}
	return out, errors.Join(errs...)
}

// ErrNotImplemented marks scaffolded behaviour that is not built yet.
var ErrNotImplemented = errors.New("not implemented")
