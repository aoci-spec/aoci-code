// agent 环境探测 + 配置与 hook 安装编排
// 索引条目: installer.go[HIS8JM]
//
// 纪律:
//   - 探测只查本地文件/目录存在,不联网;探测失败不阻止手动指定;
//   - 安装前备份目标文件(file.backup.时间戳.内容摘要,R7 纪律进产品行为);
//   - 全部模板渲染产物先过 safety.CheckForbiddenClaims 再落盘;
//   - 幂等: 目标文件内容为准(已含 aoci 配置即跳过),config.InstalledAgents 仅作记录;
//   - claude 全量(MCP 配置 + 可选 hook);codex 写项目级 MCP 配置,
//     --hooks 时额外安装 compact_prompt + SessionStart(compact) hook;
//     opencode 严格合并项目级 V1 opencode.json;
//     cursor 只输出参考片段(诚实占位)。
//
// 路径形态(Windows 真机教训): TplData 的 BinPath/RepoRoot 统一转正斜杠 ——
// 反斜杠路径进 TOML/JSON 双引号字符串会被当转义序列(\t=制表符 \a=响铃),
// Codex 解析 "C:\tools\aoci.exe" 得到损坏命令必然拉不起 server;
// 正斜杠形态 "C:/tools/aoci.exe" 双格式均无转义歧义,且 Windows API 完全接受。
package hooks

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/safety"
	"github.com/aoci-spec/aoci-code/textassets"
)

func hookMessage(key string, args ...any) string {
	value, err := textassets.Message(textassets.ActiveLocale(), key, args...)
	if err != nil {
		return fmt.Sprintf("[text asset %q failed: %v]", key, err)
	}
	return value
}

// TplData 模板渲染数据(路径字段恒为正斜杠形态)
type TplData struct {
	BinPath                    string // aoci 二进制绝对路径(正斜杠)
	RepoRoot                   string // 仓库根绝对路径(正斜杠)
	ProjectName                string // 仓库目录名
	RepoRootSlash              string // 仓库根 + 尾斜杠(索引目录段头用)
	SQuotaDefaultCompact       string
	CodexCompactCommand        string // POSIX shell command, arguments safely quoted
	CodexCompactCommandWindows string // PowerShell 5 command, arguments safely quoted
}

// toSlash 反斜杠统一转正斜杠(TOML/JSON 转义安全 + 协议内正斜杠约定)
func toSlash(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}

func quotePOSIXShellArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func quotePowerShellArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// NewTplData 构造模板数据(BinPath 取当前进程可执行文件;全路径转正斜杠)
func NewTplData(root string) TplData {
	bin, err := os.Executable()
	if err != nil {
		bin = "aoci" // 兜底: 依赖 PATH
	}
	binSlash := toSlash(bin)
	rootSlash := toSlash(root)
	posixCommand := quotePOSIXShellArgument(binSlash) +
		" --repo " + quotePOSIXShellArgument(rootSlash) +
		" hook codex-compact"
	windowsCommand := "powershell.exe -NoProfile -NonInteractive -Command \"& " +
		quotePowerShellArgument(binSlash) + " --repo " +
		quotePowerShellArgument(rootSlash) + " hook codex-compact\""
	return TplData{
		BinPath:                    binSlash,
		RepoRoot:                   rootSlash,
		ProjectName:                filepath.Base(root),
		RepoRootSlash:              strings.TrimRight(rootSlash, "/") + "/",
		SQuotaDefaultCompact:       machinecontract.NumericText().SQuotaDefaultCompact,
		CodexCompactCommand:        posixCommand,
		CodexCompactCommandWindows: windowsCommand,
	}
}

// RenderTemplate 渲染模板并过 safety 闸门(命中禁区词即拒绝落盘)
func RenderTemplate(name, tpl string, data TplData) (string, error) {
	t, err := template.New(name).Parse(tpl)
	if err != nil {
		return "", errors.New(hookMessage("hook.template_parse_error", name, err))
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", errors.New(hookMessage("hook.template_render_error", name, err))
	}
	out := buf.String()
	if hits := safety.CheckForbiddenClaims(out); len(hits) > 0 {
		return "", errors.New(hookMessage("hook.template_forbidden", name, safety.FormatHits(name, hits)))
	}
	return out, nil
}

func renderLocaleTemplate(name string, id textassets.ID, data TplData) (string, error) {
	source, err := textassets.Load(textassets.ActiveLocale(), id)
	if err != nil {
		return "", errors.New(hookMessage("hook.template_asset_error", id, err))
	}
	return RenderTemplate(name, source, data)
}

// BackupThenWrite 备份(存在时,file.backup.时间戳.内容摘要)后原子写入。
// 内容摘要确保同秒重试不会用新preimage覆盖唯一的原始备份。
func BackupThenWrite(path string, data []byte) error {
	if err := backupCurrent(path); err != nil {
		return err
	}
	return afs.AtomicWrite(path, data)
}

// BackupThenWriteCAS保留即时备份，并在最终替换紧邻边界验证计划preimage。
func BackupThenWriteCAS(path string, data []byte, expectedSHA256 string) error {
	if err := backupCurrent(path); err != nil {
		return err
	}
	return afs.AtomicWriteCAS(path, data, expectedSHA256)
}

func backupCurrent(path string) error {
	if old, err := os.ReadFile(path); err == nil {
		digest := sha256.Sum256(old)
		bak := path + ".backup." + time.Now().Format("20060102_150405") +
			"." + hex.EncodeToString(digest[:])
		if err := afs.AtomicWrite(bak, old); err != nil {
			return errors.New(hookMessage("hook.backup_error", path, err))
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return nil
}

// agentsBlockMarkers AGENTS.md 标记区块边界
const (
	agentsBegin = "<!-- aoci:begin -->"
	agentsEnd   = "<!-- aoci:end -->"
)

// loadAgentsTemplate从textassets读取AGENTS托管区块唯一事实源。
//
// 读取失败返回普通错误，由安装命令完整报告；不得静默回退到旧副本或空模板。
func loadAgentsTemplate() (string, error) {
	value, err := textassets.Load(
		textassets.ActiveLocale(),
		textassets.TemplateAgentsMD,
	)
	if err != nil {
		return "", errors.New(hookMessage("hook.agents_asset_error", err))
	}
	return value, nil
}

// EnsureAgentsBlock 在仓库根 AGENTS.md 写入/替换 aoci 标记区块。
// 已有区块整块替换,区块外内容一个字节不动;无区块则文末追加;文件不存在则新建。
// 返回动作说明。
func EnsureAgentsBlock(root string) (string, error) {
	agentsTemplate, err := loadAgentsTemplate()
	if err != nil {
		return "", err
	}

	block, err := RenderTemplate(
		"AGENTS.md.tmpl",
		agentsTemplate,
		NewTplData(root),
	)
	if err != nil {
		return "", err
	}
	block = strings.TrimRight(block, "\n")
	path := filepath.Join(root, "AGENTS.md")

	data, rerr := os.ReadFile(path)
	if rerr != nil {
		if !os.IsNotExist(rerr) {
			return "", rerr
		}
		// 新建
		if err := BackupThenWrite(path, []byte(block+"\n")); err != nil {
			return "", err
		}
		return hookMessage("hook.agents_created"), nil
	}

	text := string(data)
	bi := strings.Index(text, agentsBegin)
	ei := strings.Index(text, agentsEnd)
	if bi >= 0 && ei > bi {
		// 整块替换(区块外原样)
		newText := text[:bi] + block + text[ei+len(agentsEnd):]
		if newText == text {
			return hookMessage("hook.agents_current"), nil
		}
		if err := BackupThenWrite(path, []byte(newText)); err != nil {
			return "", err
		}
		return hookMessage("hook.agents_updated"), nil
	}
	// 文末追加
	sep := "\n"
	if !strings.HasSuffix(text, "\n") {
		sep = "\n\n"
	} else if !strings.HasSuffix(text, "\n\n") {
		sep = "\n"
	}
	if err := BackupThenWrite(path, []byte(text+sep+block+"\n")); err != nil {
		return "", err
	}
	return hookMessage("hook.agents_appended"), nil
}

// Detect 探测本机/本仓已存在的 agent 环境(仅文件存在性,不联网)
func Detect(root string) []string {
	var found []string
	home, _ := os.UserHomeDir()
	// claude: 项目 .mcp.json / .claude/ / 用户级 ~/.claude/
	if fileExists(filepath.Join(root, ".mcp.json")) ||
		dirExists(filepath.Join(root, ".claude")) ||
		(home != "" && dirExists(filepath.Join(home, ".claude"))) {
		found = append(found, "claude")
	}
	// codex: 项目 .codex/ 或用户级 ~/.codex/config.toml
	if dirExists(filepath.Join(root, ".codex")) ||
		(home != "" && fileExists(filepath.Join(home, ".codex", "config.toml"))) {
		found = append(found, "codex")
	}
	// cursor: 项目 .cursor/
	if dirExists(filepath.Join(root, ".cursor")) {
		found = append(found, "cursor")
	}
	// opencode: 项目级根配置或嵌套配置实物；单独的 .opencode 目录可能只含
	// skills/commands，不足以证明 MCP 配置环境，不能据此误报。
	if fileExists(filepath.Join(root, "opencode.json")) ||
		fileExists(filepath.Join(root, "opencode.jsonc")) ||
		fileExists(filepath.Join(root, ".opencode", "opencode.json")) ||
		fileExists(filepath.Join(root, ".opencode", "opencode.jsonc")) {
		found = append(found, "opencode")
	}
	return found
}

// Install 安装指定 agent 的接入。
// claude: 全量(MCP 配置合并写入 + 可选 PreToolUse hook);
// codex: 写项目级 .codex/config.toml 的 [mcp_servers.aoci],--hooks 时同时安装
// compact_prompt 与 SessionStart(compact) hook;
// opencode: 严格创建/合并项目级 OpenCode V1 opencode.json;
// cursor: 输出参考配置片段(诚实占位,不写文件)。
// 返回面向用户的多行结果说明。
func Install(root, agent string, withHooks bool) (string, error) {
	switch agent {
	case "claude":
		var b strings.Builder
		msg, err := InstallClaudeMCP(root)
		if err != nil {
			return "", err
		}
		b.WriteString(msg + "\n")
		if withHooks {
			hmsg, herr := InstallClaudeHook(root)
			if herr != nil {
				return "", herr
			}
			b.WriteString(hmsg + "\n")
		}
		return strings.TrimRight(b.String(), "\n"), nil
	case "codex":
		var b strings.Builder
		// 冲突预检必须在 MCP 写入前完成,保证用户自定义压缩策略
		// 与 --hooks 冲突时整个 Codex 安装零改动。
		if withHooks {
			if err := ValidateCodexCompactionInstall(root); err != nil {
				return "", err
			}
		}
		msg, err := InstallCodexMCP(root)
		if err != nil {
			return "", err
		}
		b.WriteString(msg + "\n")
		b.WriteString(hookMessage("hook.codex_native"))
		if withHooks {
			hmsg, herr := InstallCodexCompaction(root)
			if herr != nil {
				return "", herr
			}
			b.WriteString(hmsg + "\n")
		}
		return strings.TrimRight(b.String(), "\n"), nil
	case "opencode":
		return InstallOpenCodeMCP(root)
	case "cursor":
		out, err := renderLocaleTemplate(
			"codex-cursor-stubs.txt.tmpl",
			textassets.TemplateCodexCursorStubs,
			NewTplData(root),
		)
		if err != nil {
			return "", err
		}
		return hookMessage("hook.cursor_placeholder", out), nil
	default:
		return "", errors.New(hookMessage("hook.bad_agent", agent))
	}
}

// fileExists / dirExists 存在性探测
func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
