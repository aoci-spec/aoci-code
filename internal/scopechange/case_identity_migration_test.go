// 历史大小写身份的迁移代价必须是可陈述的。
//
// 应用范围身份过去把宿主文件系统的探测结果算了进去, 于是同一个仓库在 Linux 与
// Windows 上得到不同的治理身份。身份改为与宿主无关后, 以 "false" 记录的历史收据
// 需要走一次治理 Scope Change —— 这个用例钉死那次迁移的实际形状: 不动任何角色,
// 不动任何条目, Whole-Index 字节不变, 且 policy-bound auto 就能授权, 不需要真人。
//
// 这不是乐观估计, 是迁移能否在 GA 之前落地的判据。真实的大小写分岔仍会变成一次
// 真实的角色变更, 那条路由给独立审批, 不在本用例范围内。
package scopechange

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

func TestHistoricalCaseIdentityMigratesIdentityOnlyUnderAuto(t *testing.T) {
	root, _ := buildChangeFixture(t)
	setFixtureAuthorization(t, root, config.AutomationModeAuto, machinecontract.ScopeApprovalModeAuto)

	// 夹具把期望策略改成 production 以制造角色变更; 这里改回 full, 让唯一的差异
	// 只剩历史大小写位, 迁移形状才看得清楚。
	full := managedscope.DefaultPolicy(machinecontract.ScopeProfileFull)
	normalized, err := managedscope.Normalize(full)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ManagedScope = &normalized
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}

	evaluation, err := managedscope.Build(root, normalized, managedscope.BuildOptions{WalkOptions: cfg.WalkOptions()})
	if err != nil {
		t.Fatal(err)
	}
	base, err := managedscope.Identity(normalized)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	for _, value := range []string{"managed-scope-applied-identity/v2", base, evaluation.SafeInventory.RulesIdentity, "false"} {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	legacy := hex.EncodeToString(hash.Sum(nil))

	state, exists, err := baseline.Load(root)
	if err != nil || !exists || state.ManagedScope == nil {
		t.Fatalf("load Baseline: exists=%t err=%v", exists, err)
	}
	state.ManagedScope.PolicyIdentity = legacy
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}

	empty := CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1,
		Entries: []EntryCandidate{}, Dispositions: []EntryDisposition{}}
	preview, err := Build(root, authorizationTestTime, empty)
	if err != nil {
		t.Fatalf("a historical case identity must still produce a plan: %v", err)
	}
	if preview.Plan.OldPolicyIdentity != legacy || preview.Plan.NewPolicyIdentity == legacy {
		t.Fatalf("the plan must migrate from the legacy identity: old=%s new=%s",
			preview.Plan.OldPolicyIdentity, preview.Plan.NewPolicyIdentity)
	}
	if len(preview.Plan.RoleChanges) != 0 || len(preview.Plan.EntryCreates) != 0 ||
		len(preview.Plan.EntryUpdates) != 0 || len(preview.Plan.EntryRemoves) != 0 ||
		len(preview.Plan.CoverageReductions) != 0 {
		t.Fatalf("the case-identity migration must be identity-only: roles=%d creates=%d updates=%d "+
			"removes=%d reductions=%d", len(preview.Plan.RoleChanges), len(preview.Plan.EntryCreates),
			len(preview.Plan.EntryUpdates), len(preview.Plan.EntryRemoves), len(preview.Plan.CoverageReductions))
	}
	if preview.IndexPostimage.PreimageSHA256 != preview.IndexPostimage.PostimageSHA256 {
		t.Fatal("an identity-only migration must leave the Whole-Index byte-identical")
	}
	if preview.Plan.InteractionRequired {
		t.Fatal("an identity-only migration carries no ratifiable blocker, so it must not demand a reviewer")
	}
	if _, err := NewPolicyBoundApproval(root, preview, authorizationTestTime); err != nil {
		t.Fatalf("policy-bound auto must authorize an identity-only migration: %v", err)
	}
}
