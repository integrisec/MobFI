package main

import (
	"errors"
	"os/exec"
	"strings"
)

// ConnectTCP connects to an Android device over adb TCP (`adb connect
// host:port`). The device then shows up in the auto-refreshing list. It
// returns adb's message on success.
func (g *GUI) ConnectTCP(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", errors.New("enter a host:port")
	}
	out, err := exec.CommandContext(g.ctx, "adb", "connect", addr).CombinedOutput()
	msg := strings.TrimSpace(string(out))
	low := strings.ToLower(msg)
	// `adb connect` often exits 0 even on failure, so inspect the message.
	for _, bad := range []string{"fail", "cannot", "unable", "refused", "missing"} {
		if strings.Contains(low, bad) {
			return "", errors.New(msg)
		}
	}
	if err != nil && msg == "" {
		return "", err
	}
	if msg == "" {
		msg = "connected to " + addr
	}
	return msg, nil
}
