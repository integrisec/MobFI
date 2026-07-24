package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// windowState is the persisted window geometry. Only the size is stored:
// frontend.Screen exposes no per-monitor origin, so a saved position can't be
// safely validated against a multi-monitor layout, and restoring size while
// letting Wails centre the window is both predictable and always on-screen.
type windowState struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// windowStatePath is <user-config>/MobFI/window.json.
func windowStatePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "MobFI", "window.json"), nil
}

// loadWindowState reads the saved size. ok is false if there is no usable
// saved state (missing file, unreadable, or non-positive dimensions).
func loadWindowState() (windowState, bool) {
	p, err := windowStatePath()
	if err != nil {
		return windowState{}, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return windowState{}, false
	}
	var ws windowState
	if err := json.Unmarshal(b, &ws); err != nil || ws.Width <= 0 || ws.Height <= 0 {
		return windowState{}, false
	}
	return ws, true
}

// saveWindowState writes the size, best-effort (errors are ignored — a failure
// to persist geometry must never disrupt the app).
func saveWindowState(ws windowState) {
	if ws.Width <= 0 || ws.Height <= 0 {
		return
	}
	p, err := windowStatePath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	if b, err := json.Marshal(ws); err == nil {
		_ = os.WriteFile(p, b, 0o644)
	}
}

// currentScreenSize returns the logical (Wails sizing) dimensions of the
// current screen, preferring the current screen, then the primary, then the
// first. It returns 0,0 when the size is unknown so callers skip clamping.
func currentScreenSize(ctx context.Context) (int, int) {
	screens, err := wailsruntime.ScreenGetAll(ctx)
	if err != nil || len(screens) == 0 {
		return 0, 0
	}
	pick := 0
	for i, s := range screens {
		if s.IsPrimary {
			pick = i
		}
	}
	for i, s := range screens {
		if s.IsCurrent {
			pick = i
			break
		}
	}
	return screens[pick].Size.Width, screens[pick].Size.Height
}

// clampWindowToScreen shrinks the window if it is larger than the current
// screen (e.g. a size restored from a bigger display), keeping it within the
// app's minimums, and recentres it when resized. It is a no-op when the
// window already fits or the screen size is unknown.
func clampWindowToScreen(ctx context.Context) {
	sw, sh := currentScreenSize(ctx)
	if sw <= 0 || sh <= 0 {
		return
	}
	w, h := wailsruntime.WindowGetSize(ctx)
	nw, nh := w, h
	if nw > sw {
		nw = sw
	}
	if nh > sh {
		nh = sh
	}
	// Honour the minimums, but never exceed the screen.
	if nw < minWidth && minWidth <= sw {
		nw = minWidth
	}
	if nh < minHeight && minHeight <= sh {
		nh = minHeight
	}
	if nw != w || nh != h {
		wailsruntime.WindowSetSize(ctx, nw, nh)
		wailsruntime.WindowCenter(ctx)
	}
}
