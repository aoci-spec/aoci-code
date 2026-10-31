// Codex adapter: project-level MCP configuration writing (.codex/config.toml)
// Index entry: codex.go[HCX8JS]
//
// Design constraints:
//   - only the project-level .codex/config.toml is written (Codex's codex mcp add command writes only the user level,
//     and the project-level configuration officially requires editing the TOML by hand; this function automates that manual step);
//   - no TOML parsing dependency: a "table append" strategy, since under TOML syntax appending a non-duplicate [table]
//     at the end of the file is always safe; an existing [mcp_servers.aoci] is skipped (idempotence),
//     any other existing content keeps every byte, and no guess is made in complex merge scenarios;
//   - the idempotence criterion = the complete aoci MCP table header, including the TOML-equivalent quoted-key spelling;
//     a comment line #[mcp_servers.aoci] does not count as installed;
//   - BackupThenWrite backs up before writing; the rendered output first passes the safety gate (built into RenderTemplate);
//   - Codex loads the project-level configuration only for "trusted projects"; the trust action is the user's on the Codex side,
//     and this function says so in its returned wording;
//   - the optional context-compaction integration consists of the top-level compact_prompt and the
//     SessionStart(source=compact) hook; compact_prompt must sit before the first TOML
//     table, so installation prepends the AOCI-managed fragment, while the hook array-of-tables appends safely;
//   - when the user already has a compact_prompt or experimental_compact_prompt_file not managed by AOCI
//     it fails closed and never overwrites the user's compaction policy.
package hooks

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/aoci-spec/aoci-code/textassets"
)

const (
	// The markers below are the stable ownership boundary of the AOCI-managed Codex compaction configuration.
	// A user-defined compact_prompt has no prompt marker, and the installer must fail closed.
	codexCompactPromptMarker = "# aoci:codex-compact-prompt:v1"
	codexCompactHookMarker   = "# aoci:codex-compact-hook:v1"
	codexUTF8BOM             = "\ufeff"
)

var codexMCPTableHeader = regexp.MustCompile(`^\[[ \t]*("mcp_servers"|'mcp_servers'|mcp_servers)[ \t]*\.[ \t]*("aoci"|'aoci'|aoci)[ \t]*\][ \t]*(#.*)?$`)

type codexCompactPromptState struct {
	Managed          bool
	Custom           bool
	ExperimentalFile bool
}

// hasCodexTable reports whether the TOML text already holds the aoci table, including the equivalent quoted-key spelling.
// Only a complete table header with an optional trailing comment is recognized; a comment line is never mistaken for it.
func hasCodexTable(text string) bool {
	text = strings.TrimPrefix(text, codexUTF8BOM)
	for _, line := range strings.Split(text, "\n") {
		if codexMCPTableHeader.MatchString(strings.TrimSpace(line)) {
			return true
		}
	}
	return false
}

// codexTopLevelKey returns the key of a simple TOML assignment line. Codex's two compaction keys
// are both top-level scalars; once the first table header is met, later assignments belong to that table and no longer count as top level.
func codexTopLevelKey(line string) string {
	index := strings.IndexByte(line, '=')
	if index < 0 {
		return ""
	}
	key := strings.TrimSpace(line[:index])
	if len(key) >= 2 && ((key[0] == '"' && key[len(key)-1] == '"') ||
		(key[0] == '\'' && key[len(key)-1] == '\'')) {
		key = key[1 : len(key)-1]
	}
	return key
}

// inspectCodexCompactPrompt checks only the top-level keys before the first TOML table.
// The AOCI marker must sit right next to its managed compact_prompt (only blank lines or comments in between), so a
// stray marker cannot make a user-defined prompt pass for an AOCI asset.
func inspectCodexCompactPrompt(text string) codexCompactPromptState {
	text = strings.TrimPrefix(text, codexUTF8BOM)
	state := codexCompactPromptState{}
	atRoot := true
	managedMarkerPending := false
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if line == codexCompactPromptMarker {
				managedMarkerPending = true
			}
			continue
		}
		if strings.HasPrefix(line, "[") {
			atRoot = false
			managedMarkerPending = false
			continue
		}
		if !atRoot {
			continue
		}

		switch codexTopLevelKey(line) {
		case "compact_prompt":
			if managedMarkerPending {
				state.Managed = true
			} else {
				state.Custom = true
			}
		case "experimental_compact_prompt_file":
			state.ExperimentalFile = true
		}
		managedMarkerPending = false
	}
	return state
}

func containsCodexManagedSnippet(text, snippet string) bool {
	text = strings.TrimPrefix(text, codexUTF8BOM)
	snippet = strings.TrimRight(snippet, "\n")
	return snippet != "" && strings.Contains(text, snippet)
}

func hasCodexCompactCommand(text string) bool {
	text = strings.TrimPrefix(text, codexUTF8BOM)
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key := codexTopLevelKey(line)
		if (key == "command" || key == "command_windows") &&
			strings.Contains(line, "hook codex-compact") {
			return true
		}
	}
	return false
}

func validateCodexCompactionText(text string) error {
	state := inspectCodexCompactPrompt(text)
	if state.ExperimentalFile {
		return errors.New(hookMessage("hook.codex_compact_file_conflict"))
	}
	if state.Custom {
		return errors.New(hookMessage("hook.codex_compact_prompt_conflict"))
	}
	return nil
}

func appendCodexSnippet(text, content string) string {
	content = strings.TrimRight(content, "\n") + "\n"
	if text == "" {
		return content
	}
	sep := "\n"
	if !strings.HasSuffix(text, "\n") {
		sep = "\n\n"
	}
	return text + sep + content
}

func prependCodexTopLevel(text, content string) string {
	content = strings.TrimRight(content, "\n") + "\n"
	if text == "" {
		return content
	}
	if strings.HasPrefix(text, codexUTF8BOM) {
		return codexUTF8BOM + content + "\n" + strings.TrimPrefix(text, codexUTF8BOM)
	}
	return content + "\n" + text
}

func renderCodexCompactionSnippets(root string) (string, string, error) {
	data := NewTplData(root)
	prompt, err := renderLocaleTemplate(
		"codex-compact-prompt.toml.tmpl",
		textassets.TemplateCodexCompactPrompt,
		data,
	)
	if err != nil {
		return "", "", err
	}
	hook, err := renderLocaleTemplate(
		"codex-compact-hook.toml.tmpl",
		textassets.TemplateCodexCompactHook,
		data,
	)
	if err != nil {
		return "", "", err
	}
	return prompt, hook, nil
}

func validateCodexManagedSnippets(text, prompt, hook string) error {
	if err := validateCodexCompactionText(text); err != nil {
		return err
	}
	state := inspectCodexCompactPrompt(text)
	if state.Managed && !containsCodexManagedSnippet(text, prompt) {
		return errors.New(hookMessage("hook.codex_compact_managed_prompt_conflict"))
	}
	hasHook := containsCodexManagedSnippet(text, hook)
	hasHookMarker := strings.Contains(
		strings.TrimPrefix(text, codexUTF8BOM),
		codexCompactHookMarker,
	)
	if !hasHook && (hasHookMarker || hasCodexCompactCommand(text)) {
		return errors.New(hookMessage("hook.codex_compact_hook_conflict"))
	}
	return nil
}

// ValidateCodexCompactionInstall is the zero-write conflict and asset precheck of --hooks.
// The installer must call it before writing the MCP table, so a conflict with the user's own compaction policy leaves the repository unchanged.
func ValidateCodexCompactionInstall(root string) error {
	path := filepath.Join(root, ".codex", "config.toml")
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	text := string(existing)
	prompt, hook, err := renderCodexCompactionSnippets(root)
	if err != nil {
		return err
	}
	if err := validateCodexManagedSnippets(text, prompt, hook); err != nil {
		return err
	}
	return nil
}

// InstallCodexCompaction installs the AOCI-managed compact_prompt and the companion SessionStart(source=compact) hook
// in the same .codex/config.toml. The user's existing bytes are kept as they are;
// a missing top-level prompt is prepended and the hook fragment appended; with both present it is strictly zero-write.
func InstallCodexCompaction(root string) (string, error) {
	path := filepath.Join(root, ".codex", "config.toml")
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	text := string(existing)
	prompt, hook, err := renderCodexCompactionSnippets(root)
	if err != nil {
		return "", err
	}
	if err := validateCodexManagedSnippets(text, prompt, hook); err != nil {
		return "", err
	}

	state := inspectCodexCompactPrompt(text)
	hasHook := containsCodexManagedSnippet(text, hook)
	if state.Managed && hasHook {
		return hookMessage("hook.codex_compact_current"), nil
	}

	if !state.Managed {
		text = prependCodexTopLevel(text, prompt)
	}
	if !hasHook {
		text = appendCodexSnippet(text, hook)
	}

	if writeErr := BackupThenWrite(path, []byte(text)); writeErr != nil {
		return "", writeErr
	}
	return hookMessage("hook.codex_compact_installed"), nil
}

// InstallCodexMCP writes the [mcp_servers.aoci] table into the project-level .codex/config.toml.
// A missing file is created; an existing file without the table gets it appended; a file holding the table is skipped.
// It returns the user-facing result explanation.
func InstallCodexMCP(root string) (string, error) {
	data := NewTplData(root)

	// Render the TOML fragment (RenderTemplate has the safety scan built in; a forbidden word refuses the write)
	content, err := renderLocaleTemplate(
		"codex-mcp.toml.tmpl",
		textassets.TemplateCodexMCPConfig,
		data,
	)
	if err != nil {
		return "", err
	}
	content = strings.TrimRight(content, "\n") + "\n"

	path := filepath.Join(root, ".codex", "config.toml")
	existing, rerr := os.ReadFile(path)
	if rerr != nil {
		if !os.IsNotExist(rerr) {
			return "", rerr
		}
		// The target does not exist: create it (BackupThenWrite creates the .codex directory automatically)
		if werr := BackupThenWrite(path, []byte(content)); werr != nil {
			return "", werr
		}
		return hookMessage("hook.codex_created"), nil
	}

	text := string(existing)
	// Idempotence: a file already holding the table is skipped, with no speculative merge
	if hasCodexTable(text) {
		return hookMessage("hook.codex_current"), nil
	}

	// Append: under TOML syntax appending a non-duplicate table at the end is always safe; the existing content keeps every byte
	if werr := BackupThenWrite(path, []byte(appendCodexSnippet(text, content))); werr != nil {
		return "", werr
	}
	return hookMessage("hook.codex_appended"), nil
}
