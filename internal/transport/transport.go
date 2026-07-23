// Package transport abstracts the connection to a device's file system.
// Concrete transports (adb, USB, SSH, TCP bridge) implement Conn and are
// registered so the core can pick one per device.
package transport

import (
	"context"
	"errors"
	"io"
	"io/fs"

	"github.com/integrisec/MobFI/internal/device"
)

// Conn is an open session to a single device's file system.
type Conn interface {
	// Exec runs a shell command on the device.
	Exec(ctx context.Context, cmd string, args ...string) ([]byte, error)
	// Open opens a file on the device for reading.
	Open(ctx context.Context, path string) (io.ReadCloser, error)
	// Walk visits every entry under root on the device.
	Walk(ctx context.Context, root string, fn fs.WalkDirFunc) error
	// Close releases the session.
	Close() error
}

// Connector opens a Conn for devices it supports.
type Connector interface {
	// Supports reports whether this connector can reach d.
	Supports(d device.Device) bool
	// Connect opens a session to d.
	Connect(ctx context.Context, d device.Device) (Conn, error)
}

// Registry selects a Connector for a given device.
type Registry struct {
	connectors []Connector
}

// DefaultRegistry returns the registry with all built-in connectors.
// TODO: register adb, ssh and TCP-bridge connectors.
func DefaultRegistry() *Registry { return &Registry{} }

// Add registers a connector.
func (r *Registry) Add(c Connector) { r.connectors = append(r.connectors, c) }

// Connect finds a connector that supports d and opens a session.
func (r *Registry) Connect(ctx context.Context, d device.Device) (Conn, error) {
	for _, c := range r.connectors {
		if c.Supports(d) {
			return c.Connect(ctx, d)
		}
	}
	return nil, ErrNoConnector
}

// ErrNoConnector means no registered connector can reach the device.
var ErrNoConnector = errors.New("no transport connector for device")

// ErrNotImplemented marks scaffolded behaviour that is not built yet.
var ErrNotImplemented = errors.New("not implemented")
