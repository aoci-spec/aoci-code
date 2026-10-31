// Service binary identity self-check.
//
// Replacing the bytes on disk does not change the running process: after an upgrade the old process keeps serving with the old rules until the host
// restarts it. The mcp_service_version in the receipt can expose that at the cognition layer, but the tool behaviour layer has
// no signal at all; neither host nor model can see that "a different binary is already lying on disk".
//
// This records a cheap identity of our own executable (size + modification time) at service startup, and the governance
// surface later exposes the drift fact in responses as service_binary_replaced_on_disk. It is a purely advisory
// signal: it blocks no tool, takes part in no identity derivation, and only turns "time to restart" from manual diagnosis into a machine
// fact. A probe failure is always silently treated as no drift; the self-check can never be the reason the service is unavailable.
package mcptools

import (
	"os"
	"sync"
	"time"
)

type serviceBinaryIdentity struct {
	path    string
	size    int64
	modTime time.Time
}

var serviceBinaryState struct {
	sync.Mutex
	recorded *serviceBinaryIdentity
}

// RecordServiceBinaryIdentity records the on-disk identity of our own binary at service startup.
// An empty or unprobeable path records nothing and the self-check stays off.
func RecordServiceBinaryIdentity(path string) {
	if path == "" {
		return
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	serviceBinaryState.Lock()
	defer serviceBinaryState.Unlock()
	serviceBinaryState.recorded = &serviceBinaryIdentity{path: path, size: info.Size(), modTime: info.ModTime()}
}

// serviceBinaryReplacedOnDisk reports whether the service binary on disk now differs from the identity recorded at
// startup. Without a record it is always false; a vanished file also counts as replaced, since the bytes now running are
// no longer on disk.
func serviceBinaryReplacedOnDisk() bool {
	serviceBinaryState.Lock()
	recorded := serviceBinaryState.recorded
	serviceBinaryState.Unlock()
	if recorded == nil {
		return false
	}
	info, err := os.Stat(recorded.path)
	if err != nil {
		return true
	}
	return info.Size() != recorded.size || !info.ModTime().Equal(recorded.modTime)
}

// resetServiceBinaryIdentityForTest clears the record, only for tests restoring global state.
func resetServiceBinaryIdentityForTest() {
	serviceBinaryState.Lock()
	defer serviceBinaryState.Unlock()
	serviceBinaryState.recorded = nil
}
