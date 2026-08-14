package mcptools

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
)

// 关系闭包装箱的跨层验收。三组夹具对应调查阶段实测到的三种关系图:
// 无关系(多批本就正常)、链式(有解但旧实现永不收敛)、单环(无解但旧实现从不报错)。
//
// 计划阶段拿不到关系图,只能逐批观察;因此这里断言的是"有限轮内给出正确结论",
// 而不是"一轮命中"。
func relationFixtureEntry(path string, count int, mode string) string {
	base := path[strings.LastIndexByte(path, '/')+1:]
	if !strings.HasPrefix(base, "f") || len(base) < 4 {
		return base + "[CD5T]: F:carry repository integration guidance | R:- | A:- | S:-"
	}
	var index int
	if _, err := fmt.Sscanf(base[1:4], "%d", &index); err != nil {
		return base + "[CD5T]: F:carry repository integration guidance | R:- | A:- | S:-"
	}
	next := index + 1
	switch mode {
	case "none":
		return fmt.Sprintf("%s[CD5T]: F:provide fixture unit %d | R:- | A:- | S:-", base, index)
	case "chain":
		if index >= count {
			return fmt.Sprintf("%s[CD5T]: F:provide fixture chain tail %d | R:- | A:- | S:-", base, index)
		}
	default: // ring
		if index >= count {
			next = 1
		}
	}
	return fmt.Sprintf("%s[CD5T]: F:provide fixture unit %d | R:code:pkg/f%03d.go | A:- | S:-", base, index, next)
}

// driveRelationFixture 反复取批、按 mode 创作、提交,直到写入成功或机器给出结论。
// 返回 (轮数, 是否写入成功, 最后一次响应原文)。
func driveRelationFixture(t *testing.T, root string, count int, mode string, maxRounds int) (int, bool, string) {
	t.Helper()
	var follow map[string]any
	last := ""
	for round := 1; round <= maxRounds; round++ {
		session := connectMCPClient(t, root)
		var batchID string
		var candidates []map[string]any
		if follow == nil {
			raw := callVolumeTool(t, session, "aoci_maintain", map[string]any{})
			var maintain map[string]any
			if err := json.Unmarshal([]byte(raw), &maintain); err != nil {
				t.Fatal(err)
			}
			plan, _ := maintain["code_plan"].(map[string]any)
			if plan == nil {
				return round, false, raw
			}
			batchID, _ = plan["batch_id"].(string)
			for _, item := range plan["candidates"].([]any) {
				candidates = append(candidates, item.(map[string]any))
			}
		} else {
			batchID, _ = follow["batch_id"].(string)
			for _, item := range follow["candidates"].([]any) {
				candidates = append(candidates, item.(map[string]any))
			}
		}
		if len(candidates) == 0 {
			return round, false, last
		}
		entries := make([]map[string]any, 0, len(candidates))
		for _, candidate := range candidates {
			path, _ := candidate["path"].(string)
			entries = append(entries, map[string]any{"path": path,
				"source_sha256": candidate["source_sha256"], "candidate_id": candidate["candidate_id"],
				"new_entry": relationFixtureEntry(path, count, mode)})
		}
		last = callVolumeTool(t, session, "aoci_update_entry",
			map[string]any{"code_batch_id": batchID, "entries": entries})
		var result map[string]any
		if err := json.Unmarshal([]byte(last), &result); err != nil {
			t.Fatal(err)
		}
		if status, _ := result["status"].(string); status == autoStatusApplied {
			return round, true, last
		}
		if strings.Contains(last, "closure_exceeds_batch_limit") || strings.Contains(last, "not_converging") {
			return round, false, last
		}
		follow, _ = result["code_plan"].(map[string]any)
		if follow == nil {
			return round, false, last
		}
	}
	return maxRounds, false, last
}

// 无关系: 多批机制本就正常,首批直接写入(防回归)。
func TestRelationClosureWithoutRelationsAppliesImmediately(t *testing.T) {
	root := buildRelationFixtureRepo(t, 12)
	restoreBatchLimit(t, 5)
	rounds, applied, last := driveRelationFixture(t, root, 12, "none", 4)
	if !applied || rounds != 1 {
		t.Fatalf("无关系图应首批写入: rounds=%d applied=%v last=%s", rounds, applied, last[:min(len(last), 300)])
	}
}

// 链式: 每个成分只有一个节点,存在自闭合批次。旧实现永不收敛,修复后必须在
// 有限轮内写入。
func TestRelationClosureConvergesOnSolvableGraph(t *testing.T) {
	root := buildRelationFixtureRepo(t, 12)
	restoreBatchLimit(t, 5)
	rounds, applied, last := driveRelationFixture(t, root, 12, "chain", 8)
	if !applied {
		t.Fatalf("可解关系图必须收敛并写入: rounds=%d last=%s", rounds, last[:min(len(last), 400)])
	}
	if strings.Contains(last, "not_converging") {
		t.Fatalf("可解关系图不应触发不收敛保护: %s", last[:min(len(last), 300)])
	}
}

// 单环: 强连通分量等于全图。可独立写的对象先写掉,随后必须显式报出成分规模,
// 而不是无限重排。
func TestRelationClosureReportsOversizedComponent(t *testing.T) {
	root := buildRelationFixtureRepo(t, 12)
	restoreBatchLimit(t, 5)
	reported := ""
	for attempt := 0; attempt < 3 && reported == ""; attempt++ {
		_, applied, last := driveRelationFixture(t, root, 12, "ring", 8)
		if strings.Contains(last, "closure_exceeds_batch_limit") {
			reported = last
			break
		}
		if !applied {
			t.Fatalf("单环夹具既未写入也未报出成分规模: %s", last[:min(len(last), 400)])
		}
	}
	if reported == "" {
		t.Fatal("单环夹具必须显式报出闭包超限")
	}
	if !strings.Contains(reported, "largest_component=") || !strings.Contains(reported, "batch_limit=") {
		t.Fatalf("超限诊断缺少机器事实: %s", reported[:min(len(reported), 400)])
	}
}

// buildRelationFixtureRepo 造一个 count 个源文件的 Volumes 仓库,批次上限被
// 测试内联降到远小于 count 以便快速覆盖多批路径。
func buildRelationFixtureRepo(t *testing.T, count int) string {
	t.Helper()
	root := buildVolumeRepo(t, true, false)
	writeVolumeTestFile(t, root, "aoci.code.txt", cognition.CodeVolumeMarker+"\n")
	for index := 1; index <= count; index++ {
		writeVolumeTestFile(t, root, fmt.Sprintf("pkg/f%03d.go", index),
			fmt.Sprintf("package pkg\n\nfunc F%03d() int { return %d }\n", index, index))
	}
	rebaselineVolumeFixture(t, root)
	return root
}

// restoreBatchLimit 把单批上限压到夹具规模,用后还原。
func restoreBatchLimit(t *testing.T, limit int) {
	t.Helper()
	original := entriesBatchLimit
	entriesBatchLimit = limit
	t.Cleanup(func() { entriesBatchLimit = original })
}
