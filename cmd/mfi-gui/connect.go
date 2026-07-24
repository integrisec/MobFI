package main

import (
	"errors"
	"strings"

	"github.com/integrisec/MobFI/internal/sysproc"
)

// ConnectTCP connects to an Android device over adb TCP (`adb connect
// host:port`). The device then shows up in the auto-refreshing list. It
// returns adb's message on success.
func (g *GUI) ConnectTCP(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", errors.New("enter a host:port")
	}
	out, err := sysproc.CommandContext(g.ctx, "adb", "connect", addr).CombinedOutput()
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

// PairTCP pairs with an Android 11+ device for wireless debugging
// (`adb pair host:port code`). The pairing host:port and 6-digit code come
// from the device's "Pair device with pairing code" screen — note these
// differ from the connect address, so ConnectTCP is still needed afterward.
func (g *GUI) PairTCP(addr, code string) (string, error) {
	addr = strings.TrimSpace(addr)
	code = strings.TrimSpace(code)
	if addr == "" {
		return "", errors.New("enter the pairing host:port")
	}
	if code == "" {
		return "", errors.New("enter the 6-digit pairing code")
	}
	cmd := sysproc.CommandContext(g.ctx, "adb", "pair", addr, code)
	cmd.Stdin = strings.NewReader(code + "\n") // for adb builds that prompt for the code
	out, err := cmd.CombinedOutput()
	msg := strings.TrimSpace(string(out))
	low := strings.ToLower(msg)
	for _, bad := range []string{"fail", "cannot", "unable", "refused", "wrong"} {
		if strings.Contains(low, bad) {
			return "", errors.New(msg)
		}
	}
	if err != nil && msg == "" {
		return "", err
	}
	if msg == "" {
		msg = "paired with " + addr
	}
	return msg, nil
}
