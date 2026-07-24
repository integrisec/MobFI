//go:build windows

package extract

// safeComponent sanitizes a device path component for a Windows filesystem.
func safeComponent(name string) string { return windowsSafeName(name) }
