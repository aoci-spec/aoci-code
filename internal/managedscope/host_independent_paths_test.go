// Managed Scope 路径语义必须与宿主无关。
//
// 真实经历: Build 曾用 filesystemCaseSensitive() 探测当前机器的文件系统, 并把
// 那个布尔量算进应用范围身份。于是同一个仓库在 Linux 与 Windows 上 scan 会得到
// 不同的治理身份 —— 一个仓库的治理状态取决于谁的笔记本跑的扫描。8cc35eb 建了
// 一座等价桥(相反语义下逐路径重评, 全部角色一致才发布替代身份)把这个差异遮住,
// 但只在"可证等价"时有效: 规则与路径之间存在真实的大小写分岔时, Linux 同事与
// Windows 同事不可能同时对齐, 其中一方拿到一个没有真实成因的 scope_change_required。
//
// Git 的路径语义在每台主机上都是精确且区分大小写的, 范围策略现在也是。桥连同
// 那个探测一起移除。
package managedscope

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// 身份保留 v2 原始前像, 大小写位固定为 "true"。这条断言是迁移面的边界:
// 只要它成立, 在大小写敏感文件系统上建立的每一份收据都原样有效, 一个都不迁移。
func TestAppliedIdentityKeepsTheCaseSensitivePreimage(t *testing.T) {
	root := t.TempDir()
	gitScopeFixture(t, root, "init", "-q")
	writeScopeFixture(t, root, "src/main.go", "package src\n")
	gitScopeFixture(t, root, "add", "src/main.go")

	policy := DefaultPolicy(machinecontract.ScopeProfileProduction)
	result, err := Build(root, policy, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := Normalize(policy)
	if err != nil {
		t.Fatal(err)
	}
	base, err := Identity(normalized)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	for _, value := range []string{"managed-scope-applied-identity/v2", base, result.SafeInventory.RulesIdentity, "true"} {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	if want := hex.EncodeToString(hash.Sum(nil)); result.PolicyIdentity != want {
		t.Fatalf("applied identity no longer reproduces the case-sensitive v2 preimage:\n got  %s\n want %s\n"+
			"every existing receipt on a case-sensitive filesystem depends on this staying exact",
			result.PolicyIdentity, want)
	}
}

// 匹配按 Git 语义精确进行。规则与路径只差大小写时不得命中 —— 命中与否曾经取决于
// 宿主文件系统, 那正是身份随机器漂移的来源。
func TestRuleMatchingIsExactRegardlessOfHost(t *testing.T) {
	rule := Rule{RuleID: "case-exact", Action: machinecontract.ScopeRoleExclude,
		Pattern: "Docs/**", PatternKind: machinecontract.ScopePatternGlob,
		Reason: "exact match discipline", Source: machinecontract.ScopeRuleUser, Enabled: true}
	if Match(rule, "docs/readme.md") {
		t.Fatal("a pattern differing only in case must not match; matching it makes the verdict " +
			"depend on the host filesystem")
	}
	if !Match(rule, "Docs/readme.md") {
		t.Fatal("the exact pattern must still match its exact path")
	}
}

// 桥已移除: Evaluation 不再携带替代身份或宿主探测结果。
func TestEvaluationCarriesNoHostDerivedCaseFacts(t *testing.T) {
	root := t.TempDir()
	gitScopeFixture(t, root, "init", "-q")
	writeScopeFixture(t, root, "src/main.go", "package src\n")
	gitScopeFixture(t, root, "add", "src/main.go")

	result, err := Build(root, DefaultPolicy(machinecontract.ScopeProfileProduction), BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	raw := mustMarshalEvaluation(t, result)
	for _, forbidden := range []string{"case_sensitive", "alternate_policy_identity"} {
		if containsField(raw, forbidden) {
			t.Fatalf("%q survives in the evaluation; a host-derived fact in the governed shape is "+
				"what let identity drift between machines", forbidden)
		}
	}
}

func mustMarshalEvaluation(t *testing.T, value *Evaluation) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func containsField(raw, name string) bool { return strings.Contains(raw, "\""+name+"\"") }
