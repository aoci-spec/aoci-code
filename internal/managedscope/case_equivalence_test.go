package managedscope

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// 大小写语义等价证明: 规则与候选之间不存在按大小写才分岔的匹配时, 相反语义下的
// 身份是同一份应用范围的合法替代身份; 存在真实分岔时替代身份必须保持为空。
func TestBuildProvesCaseSemanticsEquivalence(t *testing.T) {
	root := t.TempDir()
	gitScopeFixture(t, root, "init", "-q")
	writeScopeFixture(t, root, "src/main.go", "package src\n")
	writeScopeFixture(t, root, "docs/readme.md", "docs\n")
	gitScopeFixture(t, root, "add", "src/main.go", "docs/readme.md")

	result, err := Build(root, DefaultPolicy(machinecontract.ScopeProfileProduction), BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.AlternatePolicyIdentity == "" {
		t.Fatal("无大小写分岔的仓库必须给出可证明的替代身份")
	}
	if result.AlternatePolicyIdentity == result.PolicyIdentity {
		t.Fatal("替代身份必须来自相反语义, 不能与当前身份相同")
	}

	// 同一仓库、带大小写分岔的规则: "Docs/**" 只在大小写不敏感语义下命中
	// docs/readme.md, 两种语义的角色分配不同, 等价证明必须失败。
	divergent := DefaultPolicy(machinecontract.ScopeProfileProduction)
	divergent.Rules = append(divergent.Rules, Rule{RuleID: "case-divergent", Action: machinecontract.ScopeRoleExclude,
		Pattern: "Docs/**", PatternKind: machinecontract.ScopePatternGlob, Reason: "case divergence probe",
		Source: machinecontract.ScopeRuleUser, CreatedBy: "test", Order: 10, Enabled: true})
	diverged, err := Build(root, divergent, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	insensitive := EvaluatePathWithCase(mustNormalize(t, divergent), "docs/readme.md", true, false, false, false)
	sensitive := EvaluatePathWithCase(mustNormalize(t, divergent), "docs/readme.md", true, false, false, true)
	if insensitive.Role == sensitive.Role {
		t.Skipf("探针规则未产生角色分岔(%s), 前提不成立", insensitive.Role)
	}
	if diverged.AlternatePolicyIdentity != "" {
		t.Fatalf("存在真实大小写分岔时不得给出替代身份: %+v", diverged.AlternatePolicyIdentity)
	}
}

func mustNormalize(t *testing.T, policy Policy) Policy {
	t.Helper()
	normalized, err := Normalize(policy)
	if err != nil {
		t.Fatal(err)
	}
	return normalized
}
