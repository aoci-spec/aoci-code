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
//   - 注入文本取自当前Locale的UI消息目录(hook.pretool.*)，目录故障同样放行;
//   - 命中后记录ledger op=hook_trigger。
//
// 本内核只返回文本和阻断决策；退出码映射由internal/cli/hook.go及
// 各Agent适配层负责。
package hooks

import (
	"os"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/textassets"
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
		text, ok := pretoolMessage(
			"hook.pretool.path_unresolved",
			pretoolRelPathReason(err),
		)
		if !ok {
			return PretoolResult{}
		}
		return PretoolResult{Text: text}
	}

	// 任意环境加载失败均静默放行。
	configValue, err := config.LoadReadOnly(root)
	if err != nil {
		return PretoolResult{}
	}

	paths := config.AOCIPaths(
		root,
		configValue.IndexPath,
	)

	document := hookDocument(root, configValue.IndexPath, paths.IndexPath)
	if document == nil || len(document.Sections) == 0 {
		return PretoolResult{}
	}

	baselineValue, _, _ := baseline.Load(root)

	result := PretoolResult{}

	entry := index.FindEntry(
		document,
		relativePath,
	)

	stale := false

	if entry == nil {
		text, ok := pretoolMessage(
			"hook.pretool.not_indexed",
			relativePath,
		)
		if !ok {
			return PretoolResult{}
		}
		result.Text = text
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

		headerKey := "hook.pretool.entry_header"

		if stale {
			headerKey = "hook.pretool.stale_header"
		} else if unbaselined {
			headerKey = "hook.pretool.unbaselined_header"
		}

		header, ok := pretoolMessage(headerKey)
		if !ok {
			return PretoolResult{}
		}

		result.Text = header +
			entry.FullLine

		if stale &&
			configValue.HookStrict {
			suffix, ok := pretoolMessage(
				"hook.pretool.strict_blocked",
			)
			if !ok {
				return PretoolResult{}
			}
			result.Block = true
			result.Text += suffix
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

// hookDocument resolves the Entry document the hook reads. Under Volumes v1
// aoci.txt is a Root manifest with no Entry sections; the Entries live in the
// Code Volume, so the hook loads the layout read-only and takes that Volume's
// document, already resolved against the root. A Legacy index keeps the direct
// parse. Any load failure returns nil and the hook stays fail-open (#85).
func hookDocument(root, configuredIndexPath, resolvedIndexPath string) *index.Document {
	if set, err := cognition.Load(root, configuredIndexPath); err == nil && set != nil {
		if set.LayoutMode == cognition.LayoutVolumesV1 {
			code := set.Volumes[cognition.ScopeCode]
			if code == nil || code.Document == nil {
				return nil
			}
			return code.Document
		}
		if set.Root.Document != nil {
			return set.Root.Document
		}
	}
	indexData, err := os.ReadFile(resolvedIndexPath)
	if err != nil {
		return nil
	}
	document, _ := index.Parse(string(indexData))
	if len(document.Sections) == 0 {
		return nil
	}
	index.ResolveRelPaths(document, root)
	return document
}

// pretoolMessage renders one hook text from the UI message catalog of the
// active locale, so an en-US repository is not handed Chinese prose. The hook
// is fail-open by contract: a catalog failure (unknown key, argument mismatch,
// damaged bundle) reports ok=false, and HandlePreTool then injects nothing and
// blocks nothing, exactly as it treats its other environment failures.
// pretoolRelPathReason localizes a NormalizeRelPath refusal; an unknown
// error keeps its own text.
func pretoolRelPathReason(err error) string {
	var text string
	var ok bool
	// Literal keys: the locale consumer contract scans them statically.
	switch key, _ := afs.RelPathMessageKey(err); key {
	case "path.rejected.empty":
		text, ok = pretoolMessage("path.rejected.empty")
	case "path.rejected.absolute":
		text, ok = pretoolMessage("path.rejected.absolute")
	case "path.rejected.drive":
		text, ok = pretoolMessage("path.rejected.drive")
	case "path.rejected.unc":
		text, ok = pretoolMessage("path.rejected.unc")
	case "path.rejected.no_target":
		text, ok = pretoolMessage("path.rejected.no_target")
	case "path.rejected.escape":
		text, ok = pretoolMessage("path.rejected.escape")
	}
	if ok {
		return text
	}
	return err.Error()
}

func pretoolMessage(key string, args ...any) (string, bool) {
	text, err := textassets.Message(textassets.ActiveLocale(), key, args...)
	if err != nil {
		return "", false
	}
	return text, true
}
