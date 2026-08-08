// PreToolUse运行时判断内核，逻辑与具体Agent无关。
// 索引条目: pretool.go[Hook.Pretool.9.S]
//
// 语义:
//   - 默认模式恒不阻断，只注入当前条目或补录提醒;
//   - 条目真实Stale时附短警告;
//   - strict模式下，条目真实Stale才阻断写入;
//   - 漂移速查消费团队line_ending_tolerance，纯CRLF/LF表示差异
//     不产生STALE警告，更不得触发strict阻断;
//   - 路径解析失败、索引缺失或配置异常均放行，hook自身故障不能卡死工作流;
//   - 命中后记录ledger op=hook_trigger。
//
// 本内核只返回文本和阻断决策；退出码映射由internal/cli/hook.go及
// 各Agent适配层负责。
package hooks

import (
	"os"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

// PretoolResult是hook内核输出。
type PretoolResult struct {
	// Text是向Agent注入的条目、警告或说明，可为空。
	Text string

	// Block=true表示建议阻断本次写操作，仅strict+真实Stale时产生。
	Block bool
}

// HandlePreTool处理一次写前hook。
//
// tool是Agent侧工具名，例如Edit、Write或MultiEdit；
// rawPath是Agent即将写入的目标路径。
func HandlePreTool(
	root string,
	tool string,
	rawPath string,
) PretoolResult {
	start := time.Now()

	// 路径非法时只说明并放行。
	// hook不是安全门禁，不能因自身参数解析失败卡死用户工作流。
	relativePath, err := afs.NormalizeRelPath(
		rawPath,
	)
	if err != nil {
		return PretoolResult{
			Text: "aoci hook: 路径未解析(" +
				err.Error() +
				"),本次不注入",
		}
	}

	// 任意环境加载失败均静默放行。
	configValue, err := config.Load(root)
	if err != nil {
		return PretoolResult{}
	}

	paths := config.AOCIPaths(
		root,
		configValue.IndexPath,
	)

	indexData, err := os.ReadFile(
		paths.IndexPath,
	)
	if err != nil {
		return PretoolResult{}
	}

	document, _ := index.Parse(
		string(indexData),
	)

	if len(document.Sections) == 0 {
		return PretoolResult{}
	}

	index.ResolveRelPaths(
		document,
		root,
	)

	baselineValue, _, _ := baseline.Load(root)

	result := PretoolResult{}

	entry := index.FindEntry(
		document,
		relativePath,
	)

	stale := false

	if entry == nil {
		result.Text =
			"aoci: 该文件未收录索引(" +
				relativePath +
				")。改动完成后请用 aoci_update_entry 补录条目。"
	} else {
		// 判定宽容、写入安全不变：
		// 本处只决定是否提示或阻断，不参与CAS与Stage源码摘要绑定。
		currentStale, unbaselined, _ :=
			baseline.IsStaleFileWith(
				root,
				relativePath,
				baselineValue,
				configValue.LineEndingTolerance,
			)

		stale = currentStale

		header :=
			"aoci: 该文件的索引条目(改动前请遵循其中约束):\n"

		if stale {
			header =
				"aoci ⚠ STALE: 该文件在条目生成后已变更," +
					"以下条目可能过期,先核对源码:\n"
		} else if unbaselined {
			header =
				"aoci: 该文件未入基线(漂移状态未知),条目如下:\n"
		}

		result.Text = header +
			entry.FullLine

		if stale &&
			configValue.HookStrict {
			result.Block = true
			result.Text +=
				"\naoci strict: 条目已过期,本次写入被阻断 —— " +
					"请先核对源码并 aoci_update_entry 更新条目" +
					"(或 aoci scan 前移基线)后重试。"
		}
	}

	ledger.Append(
		root,
		configValue.LedgerEnabled,
		ledger.Event{
			Op:          "hook_trigger",
			PathsCount:  1,
			TagFilter:   tool,
			DurationMs:  time.Since(start).Milliseconds(),
			DriftWarned: stale,
			Source:      ledger.SourceAgent,
		},
	)

	return result
}
