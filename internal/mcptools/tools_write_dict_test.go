// 单条与批量回写标签字典硬闸测试。
//
// 独立成文缘由: 既有 tools_test.go 的 buildRepo 夹具索引头部无字典行,
// 全部既有用例天然处于"无字典→闸跳过"态;字典闸正例需要带规范字典头部
// 的独立夹具,自建 dict 前缀夹具不扰动既有五测试(R42: 不引用未查看符号
// 之外,也不改造共享夹具引入耦合)。
//
// 三分支判决:
//  1. 字典外符号 → bad_args硬拒且正式索引零写入;
//  2. 跨维错位(A 维符号用在 B 位)→ 同样硬拒并保留错位说明;
//  3. 无字典仓(复用 buildRepo)→ 零字典告警(不误报,宽进语义)。
package mcptools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
)

// buildDictRepo 造带规范标签字典头部的最小仓库。
// 字典: A层级仅 X(测试层),B模块仅 Y(样例模块);E规模无数字声明(E闸不参与,
// 隔离变量只测字典闸)。索引沿用 .aoci/index.txt 旧路径与 buildRepo 同口径。
func buildDictRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "src"), 0755))
	must(os.MkdirAll(filepath.Join(root, ".aoci"), 0755))
	must(os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("A1"), 0644))
	rootSlash := filepath.ToSlash(root)
	idx := "#====字典测试仓索引====\n" +
		"#A层级: X-测试层\n" +
		"#B模块: Y-样例模块\n" +
		"===段 " + rootSlash + "/src/===\n" +
		"a.go[XY5T]: F:甲 | R:- | A:- | S:改前必读\n"
	must(os.WriteFile(filepath.Join(root, ".aoci", "index.txt"), []byte(idx), 0644))
	cfg := legacyTestConfig()
	cfg.IndexPath = ".aoci/index.txt"
	must(config.Save(root, cfg))
	snap, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	must(err)
	must(baseline.Save(root, baseline.NewBaseline(snap)))
	return root
}

// TestUpdateEntryDictGateWarning 字典闸三分支判决。
func TestUpdateEntryDictGate(t *testing.T) {
	// 分支1: 字典外符号(A 位用 Z,字典仅 X)→ 硬拒且零写入
	root := buildDictRepo(t)
	before, _ := os.ReadFile(filepath.Join(root, ".aoci", "index.txt"))
	out, fail := ApplyUpdateEntry(root, "src/b.go",
		"b.go[ZY5T]: F:乙 | R:- | A:- | S:臆造A符号", "agent", false)
	if out != nil || fail == nil || fail.Code != errBadArgs ||
		!strings.Contains(fail.Msg, "字典") {
		t.Fatalf("字典外符号必须进入可修复硬拒: out=%+v fail=%+v", out, fail)
	}
	data, _ := os.ReadFile(filepath.Join(root, ".aoci", "index.txt"))
	if string(data) != string(before) {
		t.Fatal("字典硬拒必须保持正式索引零写入")
	}

	// 分支2: 跨维错位(B 位用 A 维符号 X)→ 文案含疑似维度错位(P-14)
	root2 := buildDictRepo(t)
	out2, fail2 := ApplyUpdateEntry(root2, "src/c.go",
		"c.go[XX5T]: F:丙 | R:- | A:- | S:B位错用A符号", "agent", false)
	if out2 != nil || fail2 == nil || !strings.Contains(fail2.Msg, "错位") {
		t.Fatalf("跨维错位必须硬拒并保留说明: out=%+v fail=%+v", out2, fail2)
	}

	// 分支3: 无字典仓(buildRepo 头部无字典行)→ 零字典告警不误报
	root3 := buildRepo(t)
	out3, fail3 := ApplyUpdateEntry(root3, "src/d.go",
		"d.go[QQ5T]: F:丁 | R:- | A:- | S:无字典仓任意标签", "agent", false)
	if fail3 != nil {
		t.Fatalf("无字典仓应正常放行: %+v", fail3)
	}
	for _, w := range out3.Warnings {
		if strings.Contains(w, "字典") || strings.Contains(w, "错位") {
			t.Fatalf("无字典仓不得产生字典告警(误报): %q", w)
		}
	}
}
