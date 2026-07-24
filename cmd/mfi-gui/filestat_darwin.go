//go:build darwin

package main

import (
	"os"
	"syscall"
	"time"
)

// birthTime returns a file's creation (birth) time on darwin/macOS.
func birthTime(fi os.FileInfo) (time.Time, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return time.Time{}, false
	}
	b := st.Birthtimespec
	return time.Unix(b.Sec, b.Nsec), true
}
