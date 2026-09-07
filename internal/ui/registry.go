package ui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Registration records one running page so a later `aoci ui --detach` can
// reuse it and `aoci ui --stop` can end it. It lives in the user's cache
// directory, never in the repository: the page changes no byte of what it
// watches, and a per-user location also keeps two users' pages apart.
type Registration struct {
	Version   int       `json:"version"`
	PID       int       `json:"pid"`
	URL       string    `json:"url"`
	Root      string    `json:"root"`
	StartedAt time.Time `json:"started_at"`
}

const registryVersion = 1

// RegistryDir resolves where registrations live: an explicit override, then
// AOCI_UI_REGISTRY_DIR, then the user's cache directory. Tests and the
// black-box suite point it at a scratch directory so a developer's own
// panels are never touched.
func RegistryDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if env := os.Getenv("AOCI_UI_REGISTRY_DIR"); env != "" {
		return env, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "aoci", "ui"), nil
}

func registrationPath(dir, root string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(root)))
	return filepath.Join(dir, hex.EncodeToString(sum[:8])+".json")
}

// LogPath is where a detached page's stderr goes, beside its registration.
func LogPath(dir, root string) string {
	return strings.TrimSuffix(registrationPath(dir, root), ".json") + ".log"
}

// Register writes the registration atomically (temp file, then rename).
func Register(dir string, reg Registration) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	reg.Version = registryVersion
	data, err := json.Marshal(reg)
	if err != nil {
		return err
	}
	path := registrationPath(dir, reg.Root)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Unregister removes the registration for root when it belongs to pid, or
// unconditionally when pid is zero. A page shutting down must not remove a
// newer page's record.
func Unregister(dir, root string, pid int) error {
	path := registrationPath(dir, root)
	if pid != 0 {
		if reg, ok := Registered(dir, root); ok && reg.PID != pid {
			return nil
		}
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Registered reads the registration for root, if any.
func Registered(dir, root string) (Registration, bool) {
	data, err := os.ReadFile(registrationPath(dir, root))
	if err != nil {
		return Registration{}, false
	}
	var reg Registration
	if json.Unmarshal(data, &reg) != nil || reg.URL == "" {
		return Registration{}, false
	}
	return reg, true
}

// Live proves a registration by asking the page itself: it must answer and
// list this root. A live PID is not enough — the number may have been reused
// by an unrelated process, and that process must never be signalled.
func Live(reg Registration, timeout time.Duration) bool {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(strings.TrimSuffix(reg.URL, "/") + "/api/repos")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var repos []struct {
		Root string `json:"root"`
	}
	if json.NewDecoder(resp.Body).Decode(&repos) != nil {
		return false
	}
	want := filepath.Clean(reg.Root)
	for _, r := range repos {
		if filepath.Clean(r.Root) == want {
			return true
		}
	}
	return false
}

// Stop ends the page registered for root. Liveness is proved through the
// page before any signal is sent; a stale record is simply removed. The
// page gets a graceful signal first so it unregisters itself, a kill if it
// lingers, and the registration is removed either way.
func Stop(dir, root string, wait time.Duration) (Registration, bool, error) {
	reg, ok := Registered(dir, root)
	if !ok {
		return Registration{}, false, nil
	}
	if !Live(reg, time.Second) {
		return reg, false, Unregister(dir, root, 0)
	}
	process, err := os.FindProcess(reg.PID)
	if err != nil {
		return reg, true, err
	}
	_ = terminate(process)
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) && Live(reg, 500*time.Millisecond) {
		time.Sleep(100 * time.Millisecond)
	}
	if Live(reg, 500*time.Millisecond) {
		_ = process.Kill()
	}
	return reg, true, Unregister(dir, root, 0)
}
