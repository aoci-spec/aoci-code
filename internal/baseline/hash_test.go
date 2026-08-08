// 双指纹计算测试。
package baseline

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func writeFingerprintTestFile(
	t *testing.T,
	root string,
	name string,
	content []byte,
) string {
	t.Helper()

	path := filepath.Join(
		root,
		name,
	)

	if err := os.WriteFile(
		path,
		content,
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestHashFileLineEndingNormalization(t *testing.T) {
	root := t.TempDir()

	lfPath := writeFingerprintTestFile(
		t,
		root,
		"lf.txt",
		[]byte("alpha\nbeta\n"),
	)

	crlfPath := writeFingerprintTestFile(
		t,
		root,
		"crlf.txt",
		[]byte("alpha\r\nbeta\r\n"),
	)

	lfFingerprint, err := HashFile(lfPath)
	if err != nil {
		t.Fatal(err)
	}

	crlfFingerprint, err := HashFile(crlfPath)
	if err != nil {
		t.Fatal(err)
	}

	if lfFingerprint.SHA256 ==
		crlfFingerprint.SHA256 {
		t.Fatal("LF与CRLF原始字节不同，原始SHA不得相同")
	}

	if lfFingerprint.NormalizedSHA256 == "" ||
		crlfFingerprint.NormalizedSHA256 == "" {
		t.Fatal("普通小文本必须生成规范化指纹")
	}

	if lfFingerprint.NormalizedSHA256 !=
		crlfFingerprint.NormalizedSHA256 {
		t.Fatalf(
			"LF与CRLF规范化后应等价: %s vs %s",
			lfFingerprint.NormalizedSHA256,
			crlfFingerprint.NormalizedSHA256,
		)
	}

	if lfFingerprint.NormalizedSHA256 !=
		lfFingerprint.SHA256 {
		t.Fatal("纯LF文本的规范化指纹应等于原始指纹")
	}
}

func TestHashFileKeepsIsolatedCRDistinct(t *testing.T) {
	root := t.TempDir()

	isolatedCR := writeFingerprintTestFile(
		t,
		root,
		"isolated.txt",
		[]byte("alpha\rbeta\n"),
	)

	lf := writeFingerprintTestFile(
		t,
		root,
		"lf.txt",
		[]byte("alpha\nbeta\n"),
	)

	left, err := HashFile(isolatedCR)
	if err != nil {
		t.Fatal(err)
	}

	right, err := HashFile(lf)
	if err != nil {
		t.Fatal(err)
	}

	if left.NormalizedSHA256 ==
		right.NormalizedSHA256 {
		t.Fatal("孤立CR是真实内容差异，不得被换行宽容吞掉")
	}
}

func TestHashFileBinaryAndOversizeStayStrict(t *testing.T) {
	root := t.TempDir()

	binaryPath := writeFingerprintTestFile(
		t,
		root,
		"binary.bin",
		[]byte{'a', 0, 'b', '\r', '\n'},
	)

	binaryFingerprint, err := HashFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}

	if binaryFingerprint.NormalizedSHA256 != "" {
		t.Fatal("嗅探窗口内含NUL的文件不得生成规范化指纹")
	}

	oversizePath := writeFingerprintTestFile(
		t,
		root,
		"oversize.txt",
		bytes.Repeat(
			[]byte{'x'},
			int(normalizedFingerprintMaxBytes)+1,
		),
	)

	oversizeFingerprint, err := HashFile(oversizePath)
	if err != nil {
		t.Fatal(err)
	}

	if oversizeFingerprint.NormalizedSHA256 != "" {
		t.Fatal("超限文件必须退回严格原始指纹")
	}
}

func TestHashFileEmptyTextHasNormalizedFingerprint(t *testing.T) {
	root := t.TempDir()

	path := writeFingerprintTestFile(
		t,
		root,
		"empty.txt",
		[]byte{},
	)

	fingerprint, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if fingerprint.NormalizedSHA256 == "" {
		t.Fatal("空文本应生成规范化指纹")
	}

	if fingerprint.NormalizedSHA256 !=
		fingerprint.SHA256 {
		t.Fatal("空文本的原始与规范化指纹应相同")
	}
}

func TestFoldCRLFAcrossChunkBoundary(t *testing.T) {
	hashValue := sha256.New()

	pending := foldCRLFInto(
		hashValue,
		[]byte("alpha\r"),
		false,
	)
	if !pending {
		t.Fatal("块末CR必须保留为待决状态")
	}

	pending = foldCRLFInto(
		hashValue,
		[]byte("\nbeta"),
		pending,
	)
	if pending {
		t.Fatal("跨块CRLF已经完成，不应继续待决")
	}

	got := hex.EncodeToString(
		hashValue.Sum(nil),
	)

	expectedBytes := sha256.Sum256(
		[]byte("alpha\nbeta"),
	)
	expected := hex.EncodeToString(
		expectedBytes[:],
	)

	if got != expected {
		t.Fatalf(
			"跨块CRLF折叠错误: got=%s want=%s",
			got,
			expected,
		)
	}
}
