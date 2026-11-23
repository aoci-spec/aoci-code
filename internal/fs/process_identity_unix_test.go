//go:build (aix || darwin || dragonfly || freebsd || netbsd || openbsd || solaris) && !linux

package fs

import (
	"os"
	"testing"
)

func TestProcessIdentityIsStableAndNonEmpty(t *testing.T) {
	first, alive, known := processIdentity(os.Getpid())
	if !alive || !known || first == "" {
		t.Fatalf("当前进程必须具有可复核启动身份: identity=%q alive=%v known=%v", first, alive, known)
	}
	second, alive, known := processIdentity(os.Getpid())
	if !alive || !known || second != first {
		t.Fatalf("同一进程启动身份必须稳定: first=%q second=%q alive=%v known=%v", first, second, alive, known)
	}
}

// The identity does not follow the caller's time zone or locale (audit
// G1-05), and one recorded before the format was fixed is never compared.
func TestProcessIdentityIgnoresTimeZoneAndLegacyFormat(t *testing.T) {
	t.Setenv("TZ", "UTC")
	first, _, _ := processIdentity(os.Getpid())
	t.Setenv("TZ", "Asia/Tokyo")
	t.Setenv("LC_ALL", "de_DE.UTF-8")
	second, _, _ := processIdentity(os.Getpid())
	if first == "" || first != second {
		t.Fatalf("identity must not follow TZ or locale: %q vs %q", first, second)
	}
	if identitiesComparable("Mon Nov 23 10:00:00 2026", first) {
		t.Fatal("an identity recorded before the format was fixed must not be compared")
	}
}
