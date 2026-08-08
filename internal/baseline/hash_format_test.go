// gofmt规范指纹与format-only安全边界测试。
package baseline

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashFileRecognizesOnlyGofmtEquivalentGoSource(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sample.go")
	write := func(content string) Fingerprint {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		fingerprint, err := HashFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return fingerprint
	}

	unformatted := write("package sample\n\nfunc Value( )int{return 1}\n")
	formatted := write("package sample\n\nfunc Value() int { return 1 }\n")
	semantic := write("package sample\n\nfunc Value() int { return 2 }\n")
	stringChange := write("package sample\n\nconst Text = \"a b\"\n")
	otherString := write("package sample\n\nconst Text = \"ab\"\n")

	if !IsFormatOnlyChange(unformatted, formatted) {
		t.Fatalf("gofmt等价源码应进入快速路径: before=%+v after=%+v", unformatted, formatted)
	}
	if IsFormatOnlyChange(formatted, semantic) {
		t.Fatal("token语义变化不得进入format-only")
	}
	if IsFormatOnlyChange(stringChange, otherString) {
		t.Fatal("字符串内部空白变化不得被宽松吞掉")
	}
}

func TestHashFileOmitsFormatFingerprintForInvalidOrNonGoFiles(t *testing.T) {
	for name, content := range map[string]string{
		"invalid.go": "package {",
		"notes.txt":  "package sample\nfunc Value( ) int { return 1 }\n",
	} {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		fingerprint, err := HashFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if fingerprint.FormatSHA256 != "" || fingerprint.FormatKind != "" {
			t.Fatalf("%s不得产生格式规范指纹: %+v", name, fingerprint)
		}
	}
}
