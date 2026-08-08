// 并发防线(阶段 D)的 CAS 冲突路径直测
// 索引条目: tools_cas_test.go(待补录,随本批入册)
//
// 覆盖面: update 与 remove 两管线的 CAS 冲突判决(plan 后 commit 前索引被
// 外道篡改 → write_conflict 且索引零改动)、无冲突提交成功、锁在 commit 后
// 释放(锁文件消失)。
// 夹具纪律: 自建带 cas 前缀的最小仓,不引用未查看的 tools_test.go 既有符号
// (R42,tools_remove_test.go 同款先例);篡改动作使被测分支(哈希不符)的
// 前置状态真实存在(R43)。
// 分段可测性: planUpdateEntry/commitPlan 与 planRemoveEntry/commitRemove
// 为包内符号,测试直调以获得 plan 与 commit 之间的篡改窗口 —— 这正是删除
// 管线分段重构的动机之一。
package mcptools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// casBuildRepo 造最小仓: 根目录 aoci.txt(含头部行+一个目录段+一个条目)+目标源文件。
// 返回仓库根。段头用仓库根绝对路径保证 ResolveRelPaths 填充 RelPath。
func casBuildRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	indexText := fmt.Sprintf(`#测试仓头部说明
===代码%s/===
a.go[XA5T]: F:旧功能 | R:- | A:- | S:-
`, filepath.ToSlash(root))
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(indexText), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return root
}

// casTamperIndex 外道篡改索引(模拟编辑器手改/git pull 等不走锁的通道)
func casTamperIndex(t *testing.T, root string) string {
	t.Helper()
	p := filepath.Join(root, "aoci.txt")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	tampered := string(data) + "#外道追加的一行\n"
	if werr := os.WriteFile(p, []byte(tampered), 0644); werr != nil {
		t.Fatal(werr)
	}
	return tampered
}

// TestUpdateCASConflict update 管线: plan 后索引被篡改 → commit 判 write_conflict 且索引零改动
func TestUpdateCASConflict(t *testing.T) {
	root := casBuildRepo(t)
	p, fail := planUpdateEntry(root, "a.go", "a.go[XA5T]: F:新功能 | R:- | A:- | S:-")
	if fail != nil {
		t.Fatalf("plan 应成功: %+v", fail)
	}

	tampered := casTamperIndex(t, root)

	cfail := commitPlan(root, "human", p)
	if cfail == nil {
		t.Fatalf("篡改后 commit 应被 CAS 拒绝")
	}
	if cfail.Code != errWriteConflict {
		t.Fatalf("冲突分类应为 write_conflict, got: %s (%s)", cfail.Code, cfail.Msg)
	}
	// 索引必须保持篡改后原样(commit 未写入)
	cur, _ := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if string(cur) != tampered {
		t.Fatalf("CAS 拒绝后索引被改动:\n%s", cur)
	}
	// 锁必须已释放(defer 释锁在拒绝路径同样生效)
	if _, serr := os.Stat(filepath.Join(root, ".aoci", "lock")); !os.IsNotExist(serr) {
		t.Fatalf("CAS 拒绝路径锁未释放, stat err=%v", serr)
	}
}

// TestUpdateCASSuccess update 管线: 无篡改时 commit 成功落盘且锁释放
func TestUpdateCASSuccess(t *testing.T) {
	root := casBuildRepo(t)
	p, fail := planUpdateEntry(root, "a.go", "a.go[XA5T]: F:新功能 | R:- | A:- | S:-")
	if fail != nil {
		t.Fatalf("plan 应成功: %+v", fail)
	}
	if cfail := commitPlan(root, "human", p); cfail != nil {
		t.Fatalf("无冲突 commit 应成功: %+v", cfail)
	}
	cur, _ := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if !strings.Contains(string(cur), "F:新功能") {
		t.Fatalf("成功提交后索引应含新条目:\n%s", cur)
	}
	if _, serr := os.Stat(filepath.Join(root, ".aoci", "lock")); !os.IsNotExist(serr) {
		t.Fatalf("成功提交后锁未释放, stat err=%v", serr)
	}
}

// TestRemoveCASConflict remove 管线: plan 后索引被篡改 → commit 判 write_conflict 且索引零改动
func TestRemoveCASConflict(t *testing.T) {
	root := casBuildRepo(t)
	// orphanOnly=false(人工全权): 目标文件在盘也允许 plan,隔离护栏变量只测 CAS
	p, fail := planRemoveEntry(root, "a.go", false)
	if fail != nil {
		t.Fatalf("plan 应成功: %+v", fail)
	}

	tampered := casTamperIndex(t, root)

	cfail := commitRemove(root, "human", p)
	if cfail == nil {
		t.Fatalf("篡改后 commit 应被 CAS 拒绝")
	}
	if cfail.Code != "write_conflict" {
		t.Fatalf("冲突分类应为 write_conflict, got: %s (%s)", cfail.Code, cfail.Msg)
	}
	cur, _ := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if string(cur) != tampered {
		t.Fatalf("CAS 拒绝后索引被改动:\n%s", cur)
	}
	if _, serr := os.Stat(filepath.Join(root, ".aoci", "lock")); !os.IsNotExist(serr) {
		t.Fatalf("CAS 拒绝路径锁未释放, stat err=%v", serr)
	}
}

// TestRemoveCASSuccess remove 管线: 无篡改时删除成功且条目消失、锁释放
func TestRemoveCASSuccess(t *testing.T) {
	root := casBuildRepo(t)
	p, fail := planRemoveEntry(root, "a.go", false)
	if fail != nil {
		t.Fatalf("plan 应成功: %+v", fail)
	}
	if cfail := commitRemove(root, "human", p); cfail != nil {
		t.Fatalf("无冲突 commit 应成功: %+v", cfail)
	}
	cur, _ := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if strings.Contains(string(cur), "a.go[XA5T]") {
		t.Fatalf("删除后条目仍在:\n%s", cur)
	}
	if _, serr := os.Stat(filepath.Join(root, ".aoci", "lock")); !os.IsNotExist(serr) {
		t.Fatalf("成功删除后锁未释放, stat err=%v", serr)
	}
}
