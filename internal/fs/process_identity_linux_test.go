//go:build linux

package fs

import (
	"os/exec"
	"testing"
	"time"
)

func TestProcessIdentityTreatsZombieAsDead(t *testing.T) {
	command := exec.Command("sh", "-c", "exit 0")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Wait() })

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		identity, alive, known := processIdentity(command.Process.Pid)
		if known && !alive {
			if identity == "" {
				t.Fatal("zombie身份应保留启动tick以支持审计")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("已退出但尚未Wait的进程应被识别为zombie dead")
}
