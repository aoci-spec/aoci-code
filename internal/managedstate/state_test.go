package managedstate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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
// 历史上以大小写不敏感语义记录的收据必须停在 Scope Change, 而不是被静默采纳。
//
// 曾经存在的等价桥会在"逐路径可证等价"时把这种收据认成当前身份。它遮住的是一个
// 缺陷 —— 应用范围身份里带着宿主文件系统的探测结果 —— 而不是解决它: 规则与路径
// 之间存在真实的大小写分岔时, 两个平台不可能同时对齐。身份现在与宿主无关, 这类
// 历史收据走一次治理迁移, 期间不写任何正式资产。
func TestHistoricalCaseInsensitiveReceiptIsHeldAtScopeChange(t *testing.T) {
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
	normalized, err := managedscope.Normalize(cfg.EffectiveManagedScope())
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
	if legacy == evaluation.PolicyIdentity {
		t.Fatal("fixture precondition: the legacy preimage must differ from the canonical identity")
	}

	historical := baseline.NewBaseline(nil)
	historical.ManagedScope = &baseline.ManagedScopeState{
		Version: machinecontract.ManagedScopeBaselineV1, PolicyIdentity: legacy,
		ObserveChangePolicy: machinecontract.ObserveChangeReviewRequired,
	}
	if err := baseline.Save(root, historical); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	if err != nil {
		t.Fatal(err)
	}

	state, err := Load(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if state.PolicyAligned || !state.ScopeChangeRequired {
		t.Fatalf("a historical case-insensitive receipt must not be adopted silently: %+v", state)
	}
	if state.ActivePolicyIdentity != legacy || state.DesiredPolicyIdentity != evaluation.PolicyIdentity {
		t.Fatalf("the migration must be visible as active vs desired: active=%s desired=%s",
			state.ActivePolicyIdentity, state.DesiredPolicyIdentity)
	}
	after, err := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("detecting the migration must not write the Baseline")
	}
}
