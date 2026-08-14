package mcptools

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 自检是纯咨询信号: 未记录恒 false, 字节替换/时间戳变化/文件消失都算漂移,
// 探测失败绝不影响服务。
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

	// 同尺寸不同修改时间 —— make build 原地重建的常见形态。
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

	// 不可探测的路径: 记录静默关闭, 自检保持 false。
	resetServiceBinaryIdentityForTest()
	RecordServiceBinaryIdentity(filepath.Join(t.TempDir(), "missing"))
	if serviceBinaryReplacedOnDisk() {
		t.Fatal("记录失败时自检必须保持关闭")
	}
}

// 漂移事实要真正抵达治理面: Volumes maintain 响应携带该字段, 未漂移时不出现。
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
