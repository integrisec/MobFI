package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// windowState is the persisted window geometry. On macOS the position is
// screen-relative (an offset from the current screen's visible frame), so it
// is validated against the current screen on launch: a position saved on a
// larger or now-disconnected display simply falls out of range and is ignored
// in favour of centring. Placed distinguishes a real saved position from the
// zero value (and migrates pre-position configs, which centre).
type windowState struct {
	Width  int  `json:"width"`
	Height int  `json:"height"`
	X      int  `json:"x"`
	Y      int  `json:"y"`
	Placed bool `json:"placed"`
}

// windowStatePath is <user-config>/MobFI/window.json.
func windowStatePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "MobFI", "window.json"), nil
}

// loadWindowState reads the saved geometry. ok is false if there is no usable
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

// saveWindowState writes the geometry, best-effort (errors are ignored — a
// failure to persist geometry must never disrupt the app).
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

// applyWindowGeometry finalises the window on launch: it shrinks the restored
// size to fit the current screen (honouring the minimums), then restores the
// saved position if it is fully on-screen, otherwise centres the window.
func applyWindowGeometry(ctx context.Context, ws windowState, haveSaved bool) {
	sw, sh := currentScreenSize(ctx)

	// Size: the restored size is already applied via the launch options; only
	// shrink it if it exceeds the current screen.
	if sw > 0 && sh > 0 {
		w, h := wailsruntime.WindowGetSize(ctx)
		nw, nh := w, h
		if nw > sw {
			nw = sw
		}
		if nh > sh {
			nh = sh
		}
		if nw < minWidth && minWidth <= sw {
			nw = minWidth
		}
		if nh < minHeight && minHeight <= sh {
			nh = minHeight
		}
		if nw != w || nh != h {
			wailsruntime.WindowSetSize(ctx, nw, nh)
		}
	}

	// Position: restore only a fully on-screen saved position.
	w, h := wailsruntime.WindowGetSize(ctx)
	if haveSaved && ws.Placed && positionOnScreen(ws.X, ws.Y, w, h, sw, sh) {
		wailsruntime.WindowSetPosition(ctx, ws.X, ws.Y)
		return
	}
	wailsruntime.WindowCenter(ctx)
}

// positionOnScreen reports whether a window of size w×h placed at the
// screen-relative offset (x,y) lies entirely within a screen of size sw×sh.
// Unknown screen size (0) is treated as off-screen so the caller centres.
func positionOnScreen(x, y, w, h, sw, sh int) bool {
	if sw <= 0 || sh <= 0 {
		return false
	}
	return x >= 0 && y >= 0 && x+w <= sw && y+h <= sh
}
