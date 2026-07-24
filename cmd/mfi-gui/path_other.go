//go:build !windows

package main

// refreshPath is a no-op off Windows: the CLI and GUI inherit PATH from the
// shell or launcher, and there is no Explorer PATH-caching quirk to work around.
func refreshPath() {}
