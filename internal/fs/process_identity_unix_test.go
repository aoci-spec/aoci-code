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
