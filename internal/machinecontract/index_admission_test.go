package machinecontract_test

import (
	"regexp"
	"strings"
	"testing"
)

// 索引膨胀是单向的: 每加一条就永久摊到之后每一次 Whole-Index 交付上, 而收回要走
// 覆盖缩减的 Scope Change 审批。仓库里有 600 多个 *_test.go, 按实测约 110
// tokens/条, 全放进来会让 Whole-Index 翻倍并冲破 120k 目标线 —— 换来的却是读者
// 从被测包就能拿到的东西。
//
// 准入线写在 AGENTS.md「Cognition index admission」里: 只有需要被点名执行、带自己
// 前置条件的 harness/套件才配拥有条目; 普通包测试留在 observe, 测试锁住的事实记进
// 被锁对象的 S。这里把那条线钉成机器判定, 并防止它被悄悄删掉。
func TestOrdinaryPackageTestsNeverEnterTheWholeIndex(t *testing.T) {
	volume := readRepositoryFile(t, "aoci.code.txt")

	// Entry 行形如: name[TAG]: F:... | R:... | A:... | S:...
	entryLine := regexp.MustCompile(`^(\S[^\[]*)\[[A-Za-z0-9]+\]: F:`)
	offenders := []string{}
	for _, line := range strings.Split(volume, "\n") {
		match := entryLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		name := match[1]
		if strings.HasSuffix(name, "_test.go") {
			offenders = append(offenders, name)
		}
	}
	if len(offenders) > 0 {
		t.Fatalf(
			"ordinary package tests must stay observe, not become Whole-Index Entries: %s\n"+
				"See the Cognition index admission rule in AGENTS.md. A test earns an Entry only when it is an "+
				"executable contract run by name with its own preconditions; a test that locks a fact belongs in "+
				"the locked object's S field instead.",
			strings.Join(offenders, ", "),
		)
	}
}

// 准入线本身也要防删: 规则消失了, 上面那条机器判定就失去了它的解释与授权来源。
func TestCognitionIndexAdmissionRuleIsPublished(t *testing.T) {
	agents := readRepositoryFile(t, "AGENTS.md")
	for _, anchor := range []string{
		"## Cognition index admission",
		"executable contract",
		"stay `observe`",
		"locked object's `S`",
	} {
		if !strings.Contains(agents, anchor) {
			t.Errorf("AGENTS.md no longer states the index admission rule: missing %q", anchor)
		}
	}
}

// 验证义务同样怕被删: Tier-1 门禁只在 make full 里跑, 这条事实不在任何门禁的输出里,
// 只能靠文档携带 —— 它正是本仓库真实踩过的坑。
func TestVerificationObligationsArePublished(t *testing.T) {
	agents := readRepositoryFile(t, "AGENTS.md")
	for _, anchor := range []string{
		"## Verification obligations",
		"make fast",
		"make full",
		"clean-room-smoke",
		"scripts/blackbox/mcp_conformance.py",
		"scripts/blackbox/mcp_scenarios.py",
		"scripts/blackbox/mcp_lifecycle.py",
	} {
		if !strings.Contains(agents, anchor) {
			t.Errorf("AGENTS.md no longer states the verification obligations: missing %q", anchor)
		}
	}
}
