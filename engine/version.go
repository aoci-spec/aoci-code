package engine

import (
	"runtime/debug"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/buildinfo"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// Version reports the engine version. An aoci binary reports the version its
// build injected; a program that imports this module reports the module
// version the Go toolchain recorded for it; anything else reports "dev".
func Version() string {
	if injected := strings.TrimSpace(buildinfo.Version); injected != "" && injected != "dev" {
		return injected
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Path == machinecontract.GoModulePath && usableModuleVersion(info.Main.Version) {
			return info.Main.Version
		}
		for _, dep := range info.Deps {
			if dep == nil || dep.Path != machinecontract.GoModulePath {
				continue
			}
			if dep.Replace != nil && usableModuleVersion(dep.Replace.Version) {
				return dep.Replace.Version
			}
			if dep.Replace == nil && usableModuleVersion(dep.Version) {
				return dep.Version
			}
		}
	}
	return "dev"
}

// Commit reports the full commit an aoci build was made from, or "none".
func Commit() string {
	return buildinfo.Commit
}

func usableModuleVersion(version string) bool {
	return version != "" && version != "(devel)" && version != "v0.0.0"
}
