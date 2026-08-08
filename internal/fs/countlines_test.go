// CountFileLines 行数语义测试: 空文件/带尾换行/无尾换行/单行无换行/缺失文件。
// 索引条目待补: countlines_test.go
package fs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCountFileLines(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name    string
		content string
		want    int
	}{
		{"empty.txt", "", 0},
		{"trail.txt", "a\nb\n", 2},
		{"notrail.txt", "a\nb", 2},
		{"single.txt", "x", 1},
		{"onlynl.txt", "\n", 1},
	}
	for _, c := range cases {
		p := filepath.Join(dir, c.name)
		if err := os.WriteFile(p, []byte(c.content), 0644); err != nil {
			t.Fatal(err)
		}
		got, err := CountFileLines(p)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got != c.want {
			t.Fatalf("%s: 行数应为 %d,得到 %d", c.name, c.want, got)
		}
	}
	if _, err := CountFileLines(filepath.Join(dir, "不存在")); err == nil {
		t.Fatal("缺失文件应报错")
	}
}
