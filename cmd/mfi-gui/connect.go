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

	// A non-zero exit, or any error-ish output, means pairing failed. adb pair
	// reports failures as "error: ..." / "... fault" (not the words `adb connect`
	// uses), so match on those too rather than only fail/cannot/refused.
	failed := err != nil
	for _, bad := range []string{"fail", "fault", "error", "cannot", "unable", "refused", "wrong", "timeout", "timed out"} {
		if strings.Contains(low, bad) {
			failed = true
			break
		}
	}
	if failed {
		if msg == "" {
			if err != nil {
				return "", err
			}
			msg = "pairing failed"
		}
		// The pairing host:port and code from "Pair device with pairing code"
		// are short-lived and change every time the dialog is opened; a
		// protocol/handshake fault almost always means they went stale.
		if strings.Contains(low, "fault") || strings.Contains(low, "protocol") || strings.Contains(low, "timeout") || strings.Contains(low, "timed out") {
			msg += "\n(the pairing host:port and code expire fast and change each time the dialog is reopened -- reopen \"Pair device with pairing code\" and enter the fresh values promptly. Make sure this computer and the phone are on the same Wi-Fi with no client/AP isolation. Cloud devices (e.g. Corellium) connect via adb-over-TCP instead of pairing.)"
		}
		return "", errors.New(msg)
	}
	if msg == "" {
		msg = "paired with " + addr
	}
	return msg, nil
}
