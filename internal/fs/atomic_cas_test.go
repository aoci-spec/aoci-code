package fs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteCASPreservesChangedPreimage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "asset.txt")
	original := []byte("original\n")
	external := []byte("external\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(original)
	expected := hex.EncodeToString(digest[:])
	if err := os.WriteFile(path, external, 0o644); err != nil {
		t.Fatal(err)
	}
	err := AtomicWriteCAS(path, []byte("ours\n"), expected)
	if err == nil || !errors.Is(err, ErrAtomicCASConflict) {
		t.Fatalf("preimage变化必须返回CAS冲突: %v", err)
	}
	current, readErr := os.ReadFile(path)
	if readErr != nil || string(current) != string(external) {
		t.Fatalf("CAS冲突必须保留外部内容: err=%v current=%q", readErr, current)
	}
}

func TestAtomicWriteCASSucceedsWithCapturedPreimage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "asset.txt")
	original := []byte("original\n")
	if err := os.WriteFile(path, original, 0o640); err != nil {
		t.Fatal(err)
	}
	originalInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(original)
	if err := AtomicWriteCAS(path, []byte("ours\n"), hex.EncodeToString(digest[:])); err != nil {
		t.Fatalf("CAS写入失败: %v", err)
	}
	current, err := os.ReadFile(path)
	if err != nil || string(current) != "ours\n" {
		t.Fatalf("CAS写入结果异常: err=%v current=%q", err, current)
	}
	if matches, err := filepath.Glob(path + ".aoci-cas.*"); err != nil || len(matches) != 0 {
		t.Fatalf("成功后不得残留CAS恢复资产: err=%v matches=%v", err, matches)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != originalInfo.Mode().Perm() {
		t.Fatalf("CAS必须保留平台实际目标权限: err=%v mode=%v want=%v",
			err, info.Mode().Perm(), originalInfo.Mode().Perm())
	}
}

func TestAtomicWriteCASPreservesChangeAtReplacementBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "asset.txt")
	original := []byte("original\n")
	external := []byte("external-at-boundary\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(original)
	expected := hex.EncodeToString(digest[:])
	previousHook := beforeAtomicCASExchange
	beforeAtomicCASExchange = func(target string) {
		if err := os.WriteFile(target, external, 0o644); err != nil {
			t.Fatalf("注入替换边界外部写入失败: %v", err)
		}
	}
	t.Cleanup(func() { beforeAtomicCASExchange = previousHook })

	err := AtomicWriteCAS(path, []byte("ours\n"), expected)
	if err == nil || !errors.Is(err, ErrAtomicCASConflict) {
		t.Fatalf("替换边界变化必须返回CAS冲突: %v", err)
	}
	current, readErr := os.ReadFile(path)
	if readErr != nil || string(current) != string(external) {
		t.Fatalf("CAS冲突必须恢复交换瞬间的外部内容: err=%v current=%q", readErr, current)
	}
	if matches, globErr := filepath.Glob(path + ".aoci-cas.*"); globErr != nil || len(matches) != 0 {
		t.Fatalf("已安全回滚的冲突不得残留恢复资产: err=%v matches=%v", globErr, matches)
	}
}

func TestAtomicWriteCASStopsWhenPlatformExchangeUnavailable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "asset.txt")
	original := []byte("original\n")
	if err := os.WriteFile(path, original, 0o640); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(original)
	previousExchange := exchangeAtomicPathsPlatform
	exchangeAtomicPathsPlatform = func(string, string) error {
		return errors.New("injected unsupported exchange")
	}
	t.Cleanup(func() { exchangeAtomicPathsPlatform = previousExchange })
	err := AtomicWriteCAS(path, []byte("must-not-apply\n"), hex.EncodeToString(digest[:]))
	if err == nil || !errors.Is(err, ErrAtomicCASUnavailable) {
		t.Fatalf("平台交换不可用时必须明确停止: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(original) {
		t.Fatalf("不可用停点必须保留preimage: err=%v data=%q", err, data)
	}
	if matches, globErr := filepath.Glob(path + ".aoci-cas.*"); globErr != nil || len(matches) != 0 {
		t.Fatalf("安全停止后不得残留未应用恢复资产: err=%v matches=%v", globErr, matches)
	}
}
