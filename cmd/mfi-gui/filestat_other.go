//go:build !darwin

package main

import (
	"os"
	"time"
)

// birthTime has no portable implementation off darwin; report unavailable.
func birthTime(os.FileInfo) (time.Time, bool) { return time.Time{}, false }
