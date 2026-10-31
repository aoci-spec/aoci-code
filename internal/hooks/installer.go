// Agent environment detection plus configuration and hook installation orchestration
// Index entry: installer.go[HIS8JM]
//
// Discipline:
//   - detection only checks that local files/directories exist, never the network; a failed detection does not prevent manual selection;
//   - the target file is backed up before installation (file.backup.timestamp.content digest, the R7 discipline as product behaviour);
//   - every template rendering passes safety.CheckForbiddenClaims before it is written;
//   - idempotence: the target file's content decides (a file already holding the aoci configuration is skipped), and config.InstalledAgents is only a record;
//   - claude gets the full set (MCP configuration + optional hook); codex gets the project-level MCP configuration,
//     plus compact_prompt + the SessionStart(compact) hook under --hooks;
//     opencode strictly merges the project-level V1 opencode.json;
//     cursor only prints a reference fragment (an honest placeholder).
//
// Path form (a Windows real-machine lesson): TplData's BinPath/RepoRoot are uniformly converted to forward slashes;
// a backslash path inside a TOML/JSON double-quoted string is read as escape sequences (\t = tab, \a = bell),
// Codex parses "C:\tools\aoci.exe" into a broken command, and the server can never start;
// the forward-slash form "C:/tools/aoci.exe" has no escape ambiguity in either format, and the Windows API accepts it fully.
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

// TplData is the template rendering data (path fields are always in forward-slash form)
type TplData struct {
	BinPath                    string // absolute path of the aoci binary (forward slashes)
	RepoRoot                   string // absolute path of the repository root (forward slashes)
	ProjectName                string // the repository directory name
	RepoRootSlash              string // repository root plus trailing slash (for index directory section headers)
	SQuotaDefaultCompact       string
	CodexCompactCommand        string // POSIX shell command, arguments safely quoted
	CodexCompactCommandWindows string // PowerShell 5 command, arguments safely quoted
}

// toSlash converts backslashes to forward slashes uniformly (TOML/JSON escape safety plus the protocol's forward-slash convention)
func toSlash(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}

func quotePOSIXShellArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func quotePowerShellArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// NewTplData builds the template data (BinPath is the current process executable; every path is converted to forward slashes)
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

// RenderTemplate renders a template and passes it through the safety gate (a forbidden word refuses the write)
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

// BackupThenWrite backs up (when the file exists, as file.backup.timestamp.content digest) and then writes atomically.
// The content digest makes sure a retry in the same second does not overwrite the only original backup with a newer preimage.
func BackupThenWrite(path string, data []byte) error {
	if err := backupCurrent(path); err != nil {
		return err
	}
	return afs.AtomicWrite(path, data)
}

// BackupThenWriteCAS keeps the immediate backup and verifies the planned preimage right at the final replacement boundary.
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
		// Host configurations may include credentials for unrelated servers.
		// Backups are private even when the original configuration is public.
		if err := afs.AtomicWriteMode(bak, old, 0o600); err != nil {
			return errors.New(hookMessage("hook.backup_error", path, err))
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return nil
}

// agentsBlockMarkers are the boundaries of the AGENTS.md marker block
const (
	agentsBegin = "<!-- aoci:begin -->"
	agentsEnd   = "<!-- aoci:end -->"
)

// loadAgentsTemplate reads the single source of truth of the AGENTS managed block from textassets.
//
// A read failure returns an ordinary error that the install command reports in full; it must never fall back silently to an old copy or an empty template.
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

// EnsureAgentsBlock writes or replaces the aoci marker block in the repository root AGENTS.md.
// An existing block is replaced as a whole and the content outside the block keeps every byte; without a block it is appended at the end; a missing file is created.
// It returns an explanation of the action.
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
		// Create
		if err := BackupThenWrite(path, []byte(block+"\n")); err != nil {
			return "", err
		}
		return hookMessage("hook.agents_created"), nil
	}

	text := string(data)
	bi := strings.Index(text, agentsBegin)
	ei := strings.Index(text, agentsEnd)
	if bi >= 0 && ei > bi {
		// Replace the whole block (the outside is kept as it is)
		newText := text[:bi] + block + text[ei+len(agentsEnd):]
		if newText == text {
			return hookMessage("hook.agents_current"), nil
		}
		if err := BackupThenWrite(path, []byte(newText)); err != nil {
			return "", err
		}
		return hookMessage("hook.agents_updated"), nil
	}
	// Append at the end
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

// Detect detects the agent environments already present on this machine/repository (file existence only, no network)
func Detect(root string) []string {
	var found []string
	home, _ := os.UserHomeDir()
	// claude: project .mcp.json / .claude/ / user-level ~/.claude/
	if fileExists(filepath.Join(root, ".mcp.json")) ||
		dirExists(filepath.Join(root, ".claude")) ||
		(home != "" && dirExists(filepath.Join(home, ".claude"))) {
		found = append(found, "claude")
	}
	// codex: project .codex/ or user-level ~/.codex/config.toml
	if dirExists(filepath.Join(root, ".codex")) ||
		(home != "" && fileExists(filepath.Join(home, ".codex", "config.toml"))) {
		found = append(found, "codex")
	}
	// cursor: project .cursor/
	if dirExists(filepath.Join(root, ".cursor")) {
		found = append(found, "cursor")
	}
	// qoder: project .qoder/ (rules, settings.local.json) or user-level ~/.qoder/
	if dirExists(filepath.Join(root, ".qoder")) ||
		(home != "" && dirExists(filepath.Join(home, ".qoder"))) {
		found = append(found, "qoder")
	}
	// opencode: a project-level root configuration or a nested configuration in the flesh; a lone .opencode directory may hold only
	// skills/commands, which does not prove an MCP configuration environment and must not be reported as one.
	if fileExists(filepath.Join(root, "opencode.json")) ||
		fileExists(filepath.Join(root, "opencode.jsonc")) ||
		fileExists(filepath.Join(root, ".opencode", "opencode.json")) ||
		fileExists(filepath.Join(root, ".opencode", "opencode.jsonc")) {
		found = append(found, "opencode")
	}
	return found
}

// Install installs the integration of the given agent.
// claude: the full set (MCP configuration merge-written + optional PreToolUse hook);
// codex: writes [mcp_servers.aoci] into the project-level .codex/config.toml, and under --hooks also installs
// compact_prompt and the SessionStart(compact) hook;
// opencode: strictly creates/merges the project-level OpenCode V1 opencode.json;
// cursor: prints a reference configuration fragment (an honest placeholder, no file written).
// It returns a multi-line user-facing result explanation.
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
	case "qoder":
		// Qoder CLI reads the project-level .mcp.json (verified: `qoder mcp list`
		// reports the server Connected); the IDE takes the same mcpServers block
		// through its settings. No hook surface exists, so --hooks is inert here.
		msg, err := InstallClaudeMCP(root)
		if err != nil {
			return "", err
		}
		return msg + "\n" + hookMessage("hook.qoder_shares_mcp_json"), nil
	case "codex":
		var b strings.Builder
		// The conflict precheck must finish before the MCP write, so a conflict between the user's own compaction policy
		// and --hooks leaves the whole Codex installation unchanged.
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

// fileExists / dirExists are existence probes
func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
