// Scope Change 必须和日常治理用同一把尺子量源文件是否漂移。
//
// 真实经历: 一位用户在 Windows/Codex 上把两个二进制评审文件登记为 observe, 随后走正式
// Scope 流程, 被 managed_scope_index_source_stale 顶住, 且升级到 rc5 也不解决。他据此
// 判断"rc3 读不到 aoci.code.txt 里已有的条目"。条目其实一条都没丢: 触发这条错误的前提
// 恰恰是该文件已经是 Baseline 里的 index 角色对象。真正的原因是 internal/scopechange
// 用裸 SHA 比较, 成为仓库里唯一绕过 baseline.EquivalentFingerprints 的消费者 ——
// 313a3ab 已经在 internal/volumegovernance 修过同一件事, 却没覆盖到这里。
//
// core.autocrlf 是 Git for Windows 的默认值, 于是同一次改写: 日常 Verify/Check/Guide
// 走 EquivalentFingerprints, 判定等价、报 aligned、维护循环里没有活可干; Scope Change
// 走裸 SHA, 硬失败。两边都不给出口, 仓库就卡死在没有任何动作能清除的阻断上。
//
// 下面钉死的是: 仅行尾改写不再阻断且被如实报告, 而真实内容改动、以及团队显式关闭宽容
// 时, 这道 fail-closed 守卫一如既往。
package scopechange

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
)

func mustMarshalPlan(t *testing.T, plan Plan) []byte {
	t.Helper()
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// rewriteFixtureLineEndings 把 LF 换成 CRLF, 复现 Windows 检出的效果。
func rewriteFixtureLineEndings(t *testing.T, root, rel string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Flip to whichever form differs from what is on disk. A one-way LF to CRLF
	// rewrite over content that is already CRLF yields \r\r\n, which is a real
	// content change and would test the opposite of what these tests claim.
	normalized := bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	rewritten := normalized
	if bytes.Equal(raw, normalized) {
		rewritten = bytes.ReplaceAll(normalized, []byte("\n"), []byte("\r\n"))
	}
	if bytes.Equal(rewritten, raw) ||
		!bytes.Equal(bytes.ReplaceAll(rewritten, []byte("\r\n"), []byte("\n")), normalized) {
		t.Fatalf("fixture precondition broken: rewriting %s is not line-ending-only", rel)
	}
	if err := os.WriteFile(path, rewritten, 0o644); err != nil {
		t.Fatal(err)
	}
}

func setFixtureLineEndingTolerance(t *testing.T, root string, tolerate bool) {
	t.Helper()
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.LineEndingTolerance = tolerate
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
}

func TestLineEndingOnlyIndexSourceNoLongerBlocksScopeChange(t *testing.T) {
	root, candidates := buildChangeFixture(t)
	rewriteFixtureLineEndings(t, root, "main.go")

	preview, err := Build(root, authorizationTestTime, candidates)
	if err != nil {
		t.Fatalf("a line-ending-only rewrite is the difference team policy already calls "+
			"equivalent, so it must not block Scope Change: %v", err)
	}

	// 容忍但不沉默: 后像 Baseline 会记录新字节, 审批人必须看得见这件事。
	reported := false
	for _, item := range preview.Plan.SourceLineEndingOnly {
		if item.Path == "main.go" {
			reported = true
		}
	}
	if !reported {
		t.Fatalf("a tolerated source must still be reported, not silently accepted: %+v",
			preview.Plan.SourceLineEndingOnly)
	}
}

func TestLineEndingOnlyIndexSourceStillBlocksWhenToleranceIsOff(t *testing.T) {
	root, candidates := buildChangeFixture(t)
	setFixtureLineEndingTolerance(t, root, false)
	rewriteFixtureLineEndings(t, root, "main.go")

	_, err := Build(root, authorizationTestTime, candidates)
	if err == nil || !strings.Contains(err.Error(), "managed_scope_index_source_stale") {
		t.Fatalf("a team that switched tolerance off must keep the strict judgement: %v", err)
	}
}

func TestGenuineIndexSourceChangeStillBlocksScopeChange(t *testing.T) {
	root, candidates := buildChangeFixture(t)
	// 真实内容改动: 发布后像 Baseline 会把新字节的 SHA 盖到描述旧字节的条目上。
	if err := os.WriteFile(filepath.Join(root, "main.go"),
		[]byte("package sample\n\nfunc Added() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Build(root, authorizationTestTime, candidates)
	if err == nil || !strings.Contains(err.Error(), "managed_scope_index_source_stale") {
		t.Fatalf("a genuinely changed source must still fail closed: %v", err)
	}
}

func TestLineEndingOnlyReductionIsNotStaleCoverageDebt(t *testing.T) {
	root, candidates := buildChangeFixture(t)
	// main_test.go 正是本夹具要从 index 降为 observe 的那条, 给它一次行尾改写。
	rewriteFixtureLineEndings(t, root, "main_test.go")

	preview, err := Build(root, authorizationTestTime, candidates)
	if err != nil {
		t.Fatal(err)
	}
	for _, reduction := range preview.Plan.CoverageReductions {
		if reduction.Path == "main_test.go" && reduction.AuthoringState == "stale" {
			t.Fatalf("a line-ending rewrite is not authoring debt, so it must not grade the "+
				"reduction stale: %+v", reduction)
		}
	}
}

func TestPlanIdentityUnchangedWithoutLineEndingDrift(t *testing.T) {
	// SourceLineEndingOnly 用 omitempty: 没有漂移的计划序列化与新增字段之前完全一致,
	// plan_id 不变, 升级不会作废在途审批。带漂移的计划在修复前根本无法构建, 因此不存在
	// 被作废的对象。
	root, candidates := buildChangeFixture(t)
	preview, err := Build(root, authorizationTestTime, candidates)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Plan.SourceLineEndingOnly) != 0 {
		t.Fatalf("clean fixture must carry no line-ending report: %+v", preview.Plan.SourceLineEndingOnly)
	}
	recomputed, err := planIdentity(preview.Plan)
	if err != nil {
		t.Fatal(err)
	}
	if recomputed != preview.Plan.PlanID {
		t.Fatalf("plan identity is not reproducible: %s vs %s", recomputed, preview.Plan.PlanID)
	}
	if bytes.Contains(mustMarshalPlan(t, preview.Plan), []byte("source_line_ending_only")) {
		t.Fatal("an empty additive field must not enter the serialized plan, or every existing " +
			"plan_id changes and in-flight approvals are stranded")
	}
}

// 形式 Volume 走的是同一把尺子: Windows 检出重写 aoci.code.txt 时,
// volumegovernance 判等价而 Scope Change 判漂移, 是 313a3ab 修过的同一个洞。
func TestFormalVolumeLineEndingRewriteIsNotBaselineDrift(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("aoci.txt", cognition.RootManifestMarker+"\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n"+
		"#Project: line ending fixture\n#Global-Invariants: -\n"+
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled\n"+
		"#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta state=enabled\n")
	write("aoci.meta.txt", cognition.MetaVolumeMarker+"\n#Object-Protocol: repository-cognition-object/v2\n#FRAS-Discipline: 2\n"+
		"#FRAS-v2-Limits-Authority: machine-contract\n#S-Admission: non-inferable-and-error-preventing\n"+
		"#Object-Kinds: code=file database=table\n#[Tag dictionary: code]\n#A Layer: C Code\n#B Module: D Domain\n"+
		"#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n#[Tag dictionary: database]\n#A Layer: D Database\n"+
		"#B Module: B Business\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n")
	write("main.go", "package main\n")
	write("aoci.code.txt", cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\n"+
		"main.go[CD7T]: F:run the deterministic fixture | R:- | A:main | S:Execution remains deterministic\n")

	active := baseline.NewBaseline(map[string]baseline.Fingerprint{})
	for _, rel := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt"} {
		fingerprint, err := baseline.HashFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		active.Files[rel] = fingerprint
	}

	if _, err := FormalCognitionBaselineGuards(root, "aoci.txt", active, true); err != nil {
		t.Fatalf("aligned fixture must produce guards: %v", err)
	}

	rewriteFixtureLineEndings(t, root, "aoci.code.txt")
	if _, err := FormalCognitionBaselineGuards(root, "aoci.txt", active, true); err != nil {
		t.Fatalf("a line-ending rewrite of a Volume must not read as Baseline drift: %v", err)
	}
	if _, err := FormalCognitionBaselineGuards(root, "aoci.txt", active, false); err == nil ||
		!strings.Contains(err.Error(), "managed_scope_formal_volume_baseline_drift") {
		t.Fatalf("tolerance off must keep the strict Volume judgement: %v", err)
	}

	// 真实内容改动仍然是漂移, 与宽容与否无关。
	write("aoci.code.txt", cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\n"+
		"main.go[CD7T]: F:a genuinely different responsibility | R:- | A:main | S:-\n")
	if _, err := FormalCognitionBaselineGuards(root, "aoci.txt", active, true); err == nil ||
		!strings.Contains(err.Error(), "managed_scope_formal_volume_baseline_drift") {
		t.Fatalf("a genuinely rewritten Volume must still fail closed: %v", err)
	}
}
