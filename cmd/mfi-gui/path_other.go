//go:build !windows && !darwin

package main

// refreshPath is a no-op on Linux: a GUI launched from the app menu inherits
// the session PATH (and our .desktop entry bakes one in), so there is no
// launcher PATH quirk to work around here.
func refreshPath() {}
