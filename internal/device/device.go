// Package device models Android/iOS devices and discovers them across
// transports. Detectors are pluggable: register new mechanisms (adb, USB,
// emulators, TCP bridges, libimobiledevice) with a Registry.
package device

import (
	"context"
	"errors"
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
	USB      Transport = "usb"
	TCP      Transport = "tcp"
	Emulator Transport = "emulator"
)

// Device is a single Android/iOS target seen by a detector.
type Device struct {
	ID        string // stable identifier (adb serial, iOS UDID, ...)
	Name      string // human-friendly label
	Platform  Platform
	Transport Transport
	Address   string // host:port for TCP transports; empty otherwise
	State     string // reachability, e.g. "device", "offline", "unauthorized"
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
// TODO: also register libimobiledevice (iOS) and TCP-bridge detectors.
func DefaultRegistry() *Registry {
	r := &Registry{}
	r.Add(NewADBDetector())
	return r
}

// Add registers a detector.
func (r *Registry) Add(d Detector) { r.detectors = append(r.detectors, d) }

// DetectAll runs every detector and merges their results.
func (r *Registry) DetectAll(ctx context.Context) ([]Device, error) {
	var out []Device
	for _, d := range r.detectors {
		found, err := d.Detect(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, found...)
	}
	return out, nil
}

// ErrNotImplemented marks scaffolded behaviour that is not built yet.
var ErrNotImplemented = errors.New("not implemented")
