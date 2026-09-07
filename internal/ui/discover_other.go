//go:build !linux

package ui

// discoverRunningServers has no portable, permission-safe answer outside
// Linux; the page then shows only the repositories named explicitly.
func discoverRunningServers() []Instance { return nil }
