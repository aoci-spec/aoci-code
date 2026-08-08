// R字段关系目标轻量事实检查。
//
// AOCI条目的R字段是认知元数据，不是CLI执行路径，也不会直接控制文件写入。
// 因此，本检查器只把可确定的异常转换为Warning，不因R质量问题阻断
// Check、人工Apply、MCP回写或Auto Apply。
//
// 检查边界：
//   - R:-表示没有明确的跨文件强依赖；
//   - 有关系时建议使用半角逗号分隔精确路径或模块名；
//   - 裸文件名先按当前条目同目录解析，再回退仓库根；
//   - 仓库内文件和模块目录执行轻量状态查询，外部模块名不要求本地存在；
//   - 不遍历仓库、不读取目标正文、不解析AST、不建立全局关系图；
//   - 单条最多检查64个目标，避免异常候选制造无界文件系统调用。
//
// 真正的写入目标path仍由fs.NormalizeRelPath执行安全硬闸。R字段即使包含
// 绝对路径、逃逸路径或不存在实体，也只形成可见Warning并跳过该目标查询，
// 不将元数据质量问题错误升级为写入安全问题。
package index

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
)

const maxRelationTargetsToCheck = 64

// ValidateEntryRelations对候选条目的R字段执行轻量事实检查。
//
// repoRoot是仓库根；relPath是当前条目对应的仓库相对路径；line必须是已经
// StripFences处理后的完整单行条目。
//
// 条目结构、R段缺失或重复仍由ValidateEntryLineWith负责。本函数在条目结构
// 可解析时检查R值，并且只返回LevelWarning。
func ValidateEntryRelations(
	repoRoot,
	relPath,
	line string,
) []Violation {
	if strings.TrimSpace(repoRoot) == "" {
		return []Violation{relationWarning(
			"R关系检查已跳过: 仓库根为空",
		)}
	}

	match := consistencyEntryRe.FindStringSubmatch(line)
	if match == nil {
		// 条目结构错误由格式闸负责，避免同一问题重复报告。
		return nil
	}

	_, relationText, _, _ := splitFRAS(match[3])
	relationText = strings.TrimSpace(relationText)

	if relationText == "" {
		return []Violation{relationWarning(
			"R字段为空: 无明确跨文件强依赖时建议使用 R:- 占位",
		)}
	}
	if relationText == "-" {
		return nil
	}

	violations := []Violation{}

	if strings.Contains(relationText, "，") {
		violations = append(
			violations,
			relationWarning(
				"R字段使用了全角逗号，建议改用半角逗号分隔仓库相对路径",
			),
		)
		// 为继续执行轻量存在性检查，只在内存中把全角逗号视为分隔符。
		relationText = strings.ReplaceAll(
			relationText,
			"，",
			",",
		)
	}

	sourceRel := strings.ReplaceAll(
		strings.TrimSpace(relPath),
		"\\",
		"/",
	)
	if normalizedSource, err := afs.NormalizeRelPath(
		sourceRel,
	); err == nil {
		sourceRel = normalizedSource
	} else {
		violations = append(
			violations,
			relationWarning(
				"当前条目路径无法用于R自引用检查: "+err.Error(),
			),
		)
		sourceRel = ""
	}

	rawTargets := strings.Split(
		relationText,
		",",
	)
	checkTargets := rawTargets

	if len(rawTargets) > maxRelationTargetsToCheck {
		violations = append(
			violations,
			relationWarning(
				fmt.Sprintf(
					"R目标共%d项，超过轻量检查上限%d；仅检查前%d项，"+
						"Auto流程不会因此停止",
					len(rawTargets),
					maxRelationTargetsToCheck,
					maxRelationTargetsToCheck,
				),
			),
		)
		checkTargets = rawTargets[:maxRelationTargetsToCheck]
	}

	// seen按实际解析后的仓库相对目标去重。这样同一文件的不同合法表达
	// （例如src/b.go与当前目录下的b.go）不会绕过重复关系提示。
	seen := map[string]bool{}

	for position, rawTarget := range checkTargets {
		target := strings.TrimSpace(rawTarget)

		if target == "" {
			violations = append(
				violations,
				relationWarning(
					fmt.Sprintf(
						"R目标第%d项为空，建议删除重复分隔符",
						position+1,
					),
				),
			)
			continue
		}

		if target == "-" {
			violations = append(
				violations,
				relationWarning(
					"R占位符与真实路径混用；无关系时写 R:-，"+
						"有关系时只列路径",
				),
			)
			continue
		}

		// Explicit URI identities are cognition or external-object references,
		// not repository-relative filesystem paths. Their existence and type are
		// validated by the owning protocol layer (for example, CognitionSet).
		if isExplicitRelationURI(target) {
			continue
		}

		targetRel, normalizeErr := afs.NormalizeRelPath(
			target,
		)
		if normalizeErr != nil {
			violations = append(
				violations,
				relationWarning(
					fmt.Sprintf(
						"R目标路径不规范 %q: %v",
						target,
						normalizeErr,
					),
				),
			)
			continue
		}

		if sourceRel != "" &&
			targetRel == sourceRel {
			violations = append(
				violations,
				relationWarning(
					"R目标指向当前条目自身: "+targetRel,
				),
			)
			continue
		}

		resolvedRel, stat, statErr := lstatRelationTarget(
			repoRoot,
			sourceRel,
			targetRel,
			strings.HasPrefix(
				strings.ReplaceAll(target, "\\", "/"),
				"./",
			),
		)
		// 逐段Lstat会把末尾空段折叠到同一文件系统实体；身份键也必须
		// 同步折叠，否则file与file/可绕过自引用或等价重复检查。
		canonicalRel := strings.TrimRight(resolvedRel, "/")

		if seen[canonicalRel] {
			violations = append(
				violations,
				relationWarning(
					"R目标重复: "+targetRel,
				),
			)
			continue
		}
		seen[canonicalRel] = true

		if statErr != nil {
			if os.IsNotExist(statErr) {
				if shouldWarnMissingRelationTarget(
					repoRoot,
					target,
					targetRel,
				) {
					violations = append(
						violations,
						relationWarning(
							"R目标不存在: "+targetRel,
						),
					)
				}
			} else {
				violations = append(
					violations,
					relationWarning(
						fmt.Sprintf(
							"R目标无法检查 %s: %v",
							targetRel,
							statErr,
						),
					),
				)
			}
			continue
		}

		if sourceRel != "" &&
			canonicalRel == sourceRel {
			violations = append(
				violations,
				relationWarning(
					"R目标指向当前条目自身: "+targetRel,
				),
			)
			continue
		}

		if stat.Mode()&os.ModeSymlink != 0 {
			violations = append(
				violations,
				relationWarning(
					"R目标是符号链接，轻量检查不会继续跟随: "+
						targetRel,
				),
			)
			continue
		}

		if stat.IsDir() {
			// 仓库内模块目录是R字段允许的精确模块名。
			continue
		}

		if !stat.Mode().IsRegular() {
			violations = append(
				violations,
				relationWarning(
					"R目标不是普通文件: "+targetRel,
				),
			)
		}
	}

	return violations
}

func isExplicitRelationURI(target string) bool {
	schemeEnd := strings.Index(target, "://")
	if schemeEnd <= 0 {
		return false
	}
	for position, char := range target[:schemeEnd] {
		if (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(position > 0 && ((char >= '0' && char <= '9') || char == '+' || char == '-' || char == '.')) {
			continue
		}
		return false
	}
	return true
}

// lstatRelationTarget只查询候选明确表达的两个位置，不遍历仓库。
// 裸文件名（含尾斜杠别名）优先解释为当前条目同目录实体；同目录不存在时
// 仍兼容仓库根实体。explicitRepoRoot保留NormalizeRelPath会丢失的./语义，
// 强制只查仓库根；尾斜杠只影响原表达的缺失归属判断，不改变实体查询位置。
func lstatRelationTarget(
	repoRoot,
	sourceRel,
	targetRel string,
	explicitRepoRoot bool,
) (string, os.FileInfo, error) {
	candidates := []string{}
	lookupTarget := strings.TrimRight(targetRel, "/")

	if !explicitRepoRoot &&
		!strings.Contains(lookupTarget, "/") &&
		sourceRel != "" {
		sourceDir := path.Dir(sourceRel)
		if sourceDir != "." {
			candidates = append(
				candidates,
				path.Join(sourceDir, lookupTarget),
			)
		}
	}

	candidates = append(candidates, lookupTarget)
	missingResolvedRel := candidates[0]
	seen := map[string]bool{}

	for _, candidate := range candidates {
		if seen[candidate] {
			continue
		}
		seen[candidate] = true

		stat, err := lstatRelationCandidate(repoRoot, candidate)
		if err == nil {
			return candidate, stat, nil
		}
		if !os.IsNotExist(err) {
			return candidate, nil, err
		}
	}

	return missingResolvedRel, nil, os.ErrNotExist
}

// lstatRelationCandidate逐段Lstat仓库根下的目标，不会因为只检查
// 最后一段而跟随中间目录符号链接到仓外。
func lstatRelationCandidate(repoRoot, candidate string) (os.FileInfo, error) {
	current := repoRoot
	parts := strings.Split(candidate, "/")
	for position, part := range parts {
		current = filepath.Join(current, filepath.FromSlash(part))
		stat, err := os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if stat.Mode()&os.ModeSymlink != 0 || position == len(parts)-1 {
			return stat, nil
		}
	}
	return nil, os.ErrNotExist
}

// shouldWarnMissingRelationTarget保守识别明确指向仓内实体的表达。
// 无法由本地事实证明属于仓库的其余表达按外部模块名处理，避免把认知关系
// 强行降格为文件路径；明确的仓内路径仍保留“目标不存在”提示。
func shouldWarnMissingRelationTarget(
	repoRoot,
	rawTarget,
	targetRel string,
) bool {
	slashTarget := strings.ReplaceAll(
		strings.TrimSpace(rawTarget),
		"\\",
		"/",
	)
	if strings.HasPrefix(slashTarget, "./") {
		return true
	}

	trimmedTarget := strings.TrimSuffix(targetRel, "/")
	if strings.Contains(trimmedTarget, "/") {
		firstSegment := strings.SplitN(
			trimmedTarget,
			"/",
			2,
		)[0]
		_, err := os.Lstat(filepath.Join(
			repoRoot,
			filepath.FromSlash(firstSegment),
		))
		if err == nil || !os.IsNotExist(err) {
			return true
		}
		// 首段没有仓库实体时，本地路径与外部模块表达无法可靠区分。
		// 即使末段碰巧与当前源码扩展名相同，也不得凭该弱信号误报。
		return false
	}

	// 裸名称即使带有与源码相同的扩展名，也可能是外部模块表达。
	// 本地实体不存在时没有事实能证明其归属，不得用扩展名相似度误报。
	return false
}

// relationWarning集中保证R关系问题始终是Warning。
func relationWarning(message string) Violation {
	return Violation{
		Level: LevelWarning,
		Msg:   message,
	}
}
