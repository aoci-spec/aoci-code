package mcptools

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The self-check is a purely advisory signal: without a record it is always false, a byte replacement, a timestamp change, and a vanished file all count as drift,
// and a probe failure never affects the service.
func TestServiceBinaryReplacedOnDisk(t *testing.T) {
	t.Cleanup(resetServiceBinaryIdentityForTest)

	resetServiceBinaryIdentityForTest()
	if serviceBinaryReplacedOnDisk() {
		t.Fatal("未记录身份时不应报告漂移")
	}

	path := filepath.Join(t.TempDir(), "aoci-binary")
	if err := os.WriteFile(path, []byte("version-one"), 0o755); err != nil {
		t.Fatal(err)
	}
	RecordServiceBinaryIdentity(path)
	if serviceBinaryReplacedOnDisk() {
		t.Fatal("未变化的二进制不应报告漂移")
	}

	// Same size, different modification time: the common shape of make build rebuilding in place.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	if !serviceBinaryReplacedOnDisk() {
		t.Fatal("修改时间变化应报告漂移")
	}

	RecordServiceBinaryIdentity(path)
	if serviceBinaryReplacedOnDisk() {
		t.Fatal("重新记录后应回到未漂移")
	}
	if err := os.WriteFile(path, []byte("version-two-longer"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !serviceBinaryReplacedOnDisk() {
		t.Fatal("尺寸变化应报告漂移")
	}

	RecordServiceBinaryIdentity(path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if !serviceBinaryReplacedOnDisk() {
		t.Fatal("磁盘上不再存在这份二进制同样是漂移")
	}

	// An unprobeable path: the record shuts off silently and the self-check stays false.
	resetServiceBinaryIdentityForTest()
	RecordServiceBinaryIdentity(filepath.Join(t.TempDir(), "missing"))
	if serviceBinaryReplacedOnDisk() {
		t.Fatal("记录失败时自检必须保持关闭")
	}
}

// The drift fact must really reach the governance surface: the Volumes maintain response carries the field, absent when there is no drift.
func TestVolumeMaintainCarriesServiceBinaryDrift(t *testing.T) {
	t.Cleanup(resetServiceBinaryIdentityForTest)
	root := buildVolumeRepo(t, true, false)

	path := filepath.Join(t.TempDir(), "aoci-binary")
	if err := os.WriteFile(path, []byte("version-one"), 0o755); err != nil {
		t.Fatal(err)
	}
	RecordServiceBinaryIdentity(path)
	session := connectMCPClient(t, root)
	if got := maintainVolumeBatch(t, session); got.ServiceBinaryReplacedOnDisk {
		t.Fatalf("未漂移时不应出现该字段: %+v", got)
	}

	if err := os.WriteFile(path, []byte("version-two-longer"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := maintainVolumeBatch(t, session); !got.ServiceBinaryReplacedOnDisk {
		t.Fatalf("漂移后 maintain 必须携带咨询事实: %+v", got)
	}
}
