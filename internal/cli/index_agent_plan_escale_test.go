// A4 expected_e确定性预填判决测试(fillTargetEScale纯函数级)。
//
// 判决:
//  1. 有阈值+有行数 → ExpectedE含正确档位符号;
//  2. Lines缺失时现算补齐(update类目标既有不完整的修复锁定);
//  3. 无阈值表 → ExpectedE为空不猜;
//  4. 文件不在盘 → Lines与ExpectedE留空不崩。
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/index"
)

func escaleTestThresholds(t *testing.T) *index.EScaleThresholds {
	t.Helper()
	header := "#E规模: L大>400 M中200-400 S小100-200 T微<100\n"
	th := index.ExtractEScaleThresholds(header)
	if !th.HasThresholds() {
		t.Fatal("夹具阈值表提取失败")
	}
	return th
}

func TestFillTargetEScale(t *testing.T) {
	th := escaleTestThresholds(t)

	// 判决1: 有行数直接导出档位(50行→T)
	target := agentPlanTarget{Path: "a.go", Lines: 50}
	fillTargetEScale(&target, t.TempDir(), th)
	if len(target.ExpectedE) != 1 || target.ExpectedE[0] != "T" {
		t.Fatalf("50行应导出T档: %v", target.ExpectedE)
	}

	// 判决2: Lines缺失时现算补齐(造3行文件→T)
	root := t.TempDir()
	rel := "b.go"
	content := "package x\nvar a = 1\nvar b = 2\n"
	if err := os.WriteFile(
		filepath.Join(root, rel), []byte(content), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	target2 := agentPlanTarget{Path: rel}
	fillTargetEScale(&target2, root, th)
	if target2.Lines != 3 {
		t.Fatalf("Lines应现算为3: %d", target2.Lines)
	}
	if len(target2.ExpectedE) != 1 || target2.ExpectedE[0] != "T" {
		t.Fatalf("3行应导出T档: %v", target2.ExpectedE)
	}

	// 边界档位: 200行同属M与S(边界重叠,反查须双档且排序稳定)
	target3 := agentPlanTarget{Path: "c.go", Lines: 200}
	fillTargetEScale(&target3, t.TempDir(), th)
	joined := strings.Join(target3.ExpectedE, "/")
	if joined != "M/S" {
		t.Fatalf("200行应双档M/S(排序稳定): %v", target3.ExpectedE)
	}

	// 判决3: 无阈值表不猜
	empty := index.ExtractEScaleThresholds("#E规模: L大 M中 S小 T微\n")
	target4 := agentPlanTarget{Path: "d.go", Lines: 50}
	fillTargetEScale(&target4, t.TempDir(), empty)
	if len(target4.ExpectedE) != 0 {
		t.Fatalf("无阈值表不得导出档位: %v", target4.ExpectedE)
	}

	// 判决4: 文件不在盘留空不崩
	target5 := agentPlanTarget{Path: "ghost.go"}
	fillTargetEScale(&target5, t.TempDir(), th)
	if target5.Lines != 0 || len(target5.ExpectedE) != 0 {
		t.Fatalf("不在盘文件应留空: lines=%d e=%v",
			target5.Lines, target5.ExpectedE)
	}
}
