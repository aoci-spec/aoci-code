// workflow 包测试共享小辅助
// 索引条目: 测试辅助文件(随包测试,不单独立条)
package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

// writeRepoFile 向测试仓写一个文件(内容可空)
func writeRepoFile(t *testing.T, root, rel, content string) error {
	t.Helper()
	return os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(content), 0644)
}
