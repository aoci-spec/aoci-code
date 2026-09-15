//go:build linux

package ui

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// discoverRunningServers lists the `aoci mcp` processes of the current user
// by reading /proc, so one status page can show every repository a host has
// a live server for. The scan is best effort and purely observational: an
// unreadable process is skipped, nothing is signalled, and a process that is
// not an aoci MCP server is ignored. The executable link answers a question
// the server itself can only guess at from inside — whether the binary it
// runs from has since been replaced on disk — because the kernel appends
// " (deleted)" to the link target of an unlinked file.
func discoverRunningServers() []Instance {
	return discoverRunningServersFrom("/proc", uint32(os.Geteuid()))
}

func discoverRunningServersFrom(procRoot string, effectiveUID uint32) []Instance {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil
	}
	self := os.Getpid()
	instances := []Instance{}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == self {
			continue
		}
		processDir := filepath.Join(procRoot, entry.Name())
		info, err := os.Stat(processDir)
		if err != nil {
			continue
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != effectiveUID {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(processDir, "cmdline"))
		if err != nil || len(raw) == 0 {
			continue
		}
		args := strings.Split(strings.TrimRight(string(bytes.ReplaceAll(raw, []byte{0}, []byte{'\n'})), "\n"), "\n")
		instance, ok := parseServerCommandLine(pid, args)
		if !ok {
			continue
		}
		if executable, err := os.Readlink(filepath.Join(processDir, "exe")); err == nil {
			instance.Executable = strings.TrimSuffix(executable, " (deleted)")
			instance.ExecutableDeleted = strings.HasSuffix(executable, " (deleted)")
		}
		if !filepath.IsAbs(instance.Root) {
			cwd, err := os.Readlink(filepath.Join(processDir, "cwd"))
			// A removed directory reads back as "<path> (deleted)", the same
			// marker the exe link uses; joining it would present a repository
			// that no longer exists, so the instance is skipped like an
			// unreadable one.
			if err != nil || strings.HasSuffix(cwd, " (deleted)") {
				continue
			}
			// Relative --repo paths belong to the server, not this status page.
			instance.Root = filepath.Join(cwd, instance.Root)
		}
		instance.Root = filepath.Clean(instance.Root)
		instance.StartedAt = info.ModTime().UTC().Format(time.RFC3339)
		instances = append(instances, instance)
	}
	return instances
}
