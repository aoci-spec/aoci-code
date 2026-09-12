package ui

import "strings"

// Instance is one running `aoci mcp` server observed on this machine.
type Instance struct {
	PID               int      `json:"pid"`
	Root              string   `json:"root"`
	Executable        string   `json:"executable,omitempty"`
	ExecutableDeleted bool     `json:"executable_replaced_on_disk"`
	Args              []string `json:"args"`
	StartedAt         string   `json:"started_at,omitempty"`
}

// parseServerCommandLine recognises `<aoci> [--repo <root>|--repo=<root>] ... mcp`
// and returns the declared repository root without resolving relative paths.
// A process whose argv[0] is not an aoci binary, or that does not run the mcp
// subcommand, is not a server.
func parseServerCommandLine(pid int, args []string) (Instance, bool) {
	if len(args) < 2 {
		return Instance{}, false
	}
	base := args[0]
	if cut := strings.LastIndexAny(base, `/\\`); cut >= 0 {
		base = base[cut+1:]
	}
	name := strings.TrimSuffix(base, ".exe")
	if name != "aoci" {
		return Instance{}, false
	}
	instance := Instance{PID: pid, Args: append([]string{}, args...)}
	isServer := false
	for index := 1; index < len(args); index++ {
		switch arg := args[index]; {
		case arg == "mcp":
			isServer = true
		case arg == "--repo" && index+1 < len(args):
			instance.Root = args[index+1]
			index++
		case strings.HasPrefix(arg, "--repo="):
			instance.Root = strings.TrimPrefix(arg, "--repo=")
		}
	}
	if !isServer {
		return Instance{}, false
	}
	return instance, true
}
