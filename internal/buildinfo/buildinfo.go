// Package buildinfo holds the identity the build injects into an aoci binary.
//
// The three variables are the ldflags -X targets of the Makefile, the
// GoReleaser configuration, and the clean-room smoke build. They live in
// their own package so the embeddable engine can report the same identity
// the CLI prints without importing the command tree, and so a library
// consumer that injects nothing reads the documented defaults.
package buildinfo

var (
	// Version is the release version of this build, or "dev".
	Version = "dev"
	// Commit is the full commit the binary was built from, or "none".
	Commit = "none"
	// BuildDate is the commit date the binary carries, or "unknown".
	BuildDate = "unknown"
)
