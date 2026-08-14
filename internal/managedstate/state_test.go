package managedstate

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
	"os"
	"os/exec"
	"path/filepath"
)

func writeStateFixture(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitStateFixture(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

// 在大小写敏感平台上建立的 Baseline 收据, 在等价可证明时被相反语义的平台原样
// 接受: PolicyAligned=true, 且采纳收据身份为当前身份, 让计划与提交跨平台共享
// 同一标识。伪造的收据身份仍然失败关闭。
func TestLoadAcceptsProvenCaseEquivalentReceipt(t *testing.T) {
	root := t.TempDir()
	gitStateFixture(t, root, "init", "-q")
	writeStateFixture(t, root, "src/main.go", "package src\n")
	gitStateFixture(t, root, "add", "src/main.go")

	policy := managedscope.DefaultPolicy(machinecontract.ScopeProfileProduction)
	cfg := &config.Config{ManagedScope: &policy}
	evaluation, err := managedscope.Build(root, cfg.EffectiveManagedScope(), managedscope.BuildOptions{WalkOptions: fs.WalkOptions{}})
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.AlternatePolicyIdentity == "" {
		t.Fatal("测试前提: 本仓库无大小写分岔, 必须有替代身份")
	}

	// 模拟另一平台建立的收据: 身份是相反语义下的那一个。
	crossPlatform := baseline.NewBaseline(nil)
	crossPlatform.ManagedScope = &baseline.ManagedScopeState{
		Version: machinecontract.ManagedScopeBaselineV1, PolicyIdentity: evaluation.AlternatePolicyIdentity,
		ObserveChangePolicy: machinecontract.ObserveChangeReviewRequired,
	}
	if err := baseline.Save(root, crossPlatform); err != nil {
		t.Fatal(err)
	}
	state, err := Load(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !state.PolicyAligned || state.ScopeChangeRequired {
		t.Fatalf("等价可证明的跨平台收据必须对齐: %+v", state)
	}
	if state.DesiredPolicyIdentity != evaluation.AlternatePolicyIdentity {
		t.Fatalf("对齐后必须采纳收据身份为当前身份: desired=%s active=%s",
			state.DesiredPolicyIdentity, state.ActivePolicyIdentity)
	}

	// 伪造身份: 既不是本语义也不是替代语义 → 仍然 scope_change_required。
	forged := baseline.NewBaseline(nil)
	forged.ManagedScope = &baseline.ManagedScopeState{
		Version: machinecontract.ManagedScopeBaselineV1, PolicyIdentity: "0000000000000000000000000000000000000000000000000000000000000000",
		ObserveChangePolicy: machinecontract.ObserveChangeReviewRequired,
	}
	if err := baseline.Save(root, forged); err != nil {
		t.Fatal(err)
	}
	state, err = Load(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if state.PolicyAligned || !state.ScopeChangeRequired {
		t.Fatalf("伪造收据身份必须失败关闭: %+v", state)
	}
}
