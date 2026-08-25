package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/textassets"
)

var hanTextPattern = regexp.MustCompile(`[\p{Han}]`)

func runLocaleInit(t *testing.T, root string, localeArgs ...string) (string, string, int) {
	t.Helper()
	previousLocale := textassets.ActiveLocale()
	if err := textassets.SetActiveLocale(textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := textassets.SetActiveLocale(previousLocale); err != nil {
			t.Errorf("restore active Locale: %v", err)
		}
	})
	args := []string{"--repo", root, "init", "--agent=", "--hooks=false"}
	if len(localeArgs) == 0 {
		args = append(args, "--locale", textassets.DefaultLocale)
	} else {
		args = append(args, localeArgs...)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := executeCLI(args, &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}

func TestRuntimeLocaleMessageConsumersAreDeclared(t *testing.T) {
	root := localeRuntimeRepositoryRoot(t)
	keys := map[string]struct{}{}
	for _, relative := range []string{"internal/cli", "internal/mcptools", "internal/ledger"} {
		walkProductionGoFiles(t, filepath.Join(root, relative), func(path string, file *ast.File) {
			if filepath.Base(path) == "command_localization.go" {
				collectStringMapValues(t, path, file, "commandShortMessages", keys)
			}
			if filepath.Base(path) == "verify_render.go" {
				collectLiteralCallArguments(t, path, file, "section", keys)
			}
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				identifier, ok := call.Fun.(*ast.Ident)
				if !ok || (identifier.Name != "cliMessage" && identifier.Name != "mcpMessage" && identifier.Name != "writeMessage") {
					return true
				}
				if len(call.Args) == 0 {
					t.Fatalf("%s: %s call has no message key", path, identifier.Name)
				}
				literal, ok := call.Args[0].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					if argument, dynamic := call.Args[0].(*ast.Ident); dynamic && filepath.Base(path) == "command_localization.go" && identifier.Name == "cliMessage" && argument.Name == "key" {
						return true
					}
					if argument, dynamic := call.Args[0].(*ast.Ident); dynamic && filepath.Base(path) == "verify_render.go" && identifier.Name == "cliMessage" && argument.Name == "messageKey" {
						return true
					}
					t.Fatalf("%s: %s message key must be a string literal", path, identifier.Name)
				}
				key, err := strconv.Unquote(literal.Value)
				if err != nil || strings.TrimSpace(key) == "" {
					t.Fatalf("%s: invalid %s message key %q", path, identifier.Name, literal.Value)
				}
				keys[key] = struct{}{}
				return true
			})
		})
	}

	list := make([]string, 0, len(keys))
	for key := range keys {
		list = append(list, key)
	}
	sort.Strings(list)
	for _, locale := range []string{textassets.DefaultLocale, textassets.LegacyLocale} {
		if err := textassets.ValidateMessageKeys(locale, list); err != nil {
			t.Fatalf("%s runtime message consumer contract: %v", locale, err)
		}
	}
}

func collectLiteralCallArguments(t *testing.T, path string, file *ast.File, function string, keys map[string]struct{}) {
	t.Helper()
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		identifier, ok := call.Fun.(*ast.Ident)
		if !ok || identifier.Name != function {
			return true
		}
		if len(call.Args) == 0 {
			t.Fatalf("%s: %s call has no message key", path, function)
		}
		literal, ok := call.Args[0].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			t.Fatalf("%s: %s message key must be a string literal", path, function)
		}
		key, err := strconv.Unquote(literal.Value)
		if err != nil || strings.TrimSpace(key) == "" {
			t.Fatalf("%s: invalid %s message key %q", path, function, literal.Value)
		}
		keys[key] = struct{}{}
		return true
	})
}

func collectStringMapValues(t *testing.T, path string, file *ast.File, variable string, keys map[string]struct{}) {
	t.Helper()
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		declaration, ok := node.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for index, name := range declaration.Names {
			if name.Name != variable || index >= len(declaration.Values) {
				continue
			}
			found = true
			literal, ok := declaration.Values[index].(*ast.CompositeLit)
			if !ok {
				t.Fatalf("%s: %s must be a map literal", path, variable)
			}
			for _, element := range literal.Elts {
				pair, ok := element.(*ast.KeyValueExpr)
				if !ok {
					t.Fatalf("%s: %s contains a non-keyed element", path, variable)
				}
				value, ok := pair.Value.(*ast.BasicLit)
				if !ok || value.Kind != token.STRING {
					t.Fatalf("%s: %s contains a non-literal message key", path, variable)
				}
				key, err := strconv.Unquote(value.Value)
				if err != nil || strings.TrimSpace(key) == "" {
					t.Fatalf("%s: invalid %s message key %q", path, variable, value.Value)
				}
				keys[key] = struct{}{}
			}
		}
		return true
	})
	if !found {
		t.Fatalf("%s: %s declaration not found", path, variable)
	}
}

func TestRuntimeLocaleShellsContainNoHardcodedHan(t *testing.T) {
	root := localeRuntimeRepositoryRoot(t)
	allowed := map[string]map[string]bool{
		"internal/cli/index_agent_plan_helpers.go": {"A层级": true, "B模块": true},
		"internal/mcptools/tools_maintain.go":      {"A层级": true, "B模块": true},
	}
	for _, relative := range []string{"internal/cli", "internal/mcptools", "internal/ledger"} {
		walkProductionGoFiles(t, filepath.Join(root, relative), func(path string, file *ast.File) {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				t.Fatal(err)
			}
			rel = filepath.ToSlash(rel)
			ast.Inspect(file, func(node ast.Node) bool {
				literal, ok := node.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(literal.Value)
				if err != nil || !containsHanRune(value) {
					return true
				}
				if allowed[rel][value] {
					return true
				}
				t.Errorf("%s contains hardcoded user-visible Han string %q", rel, value)
				return true
			})
		})
	}
}

func localeRuntimeRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func walkProductionGoFiles(t *testing.T, directory string, visit func(string, *ast.File)) {
	t.Helper()
	err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		visit(path, file)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func containsHanRune(value string) bool {
	for _, character := range value {
		if unicode.Is(unicode.Han, character) {
			return true
		}
	}
	return false
}

func TestFreshInitDefaultsToCompleteEnglishSurface(t *testing.T) {
	root := t.TempDir()
	stdout, stderr, code := runLocaleInit(t, root)
	if code != ExitOK {
		t.Fatalf("init exit=%d stderr=%s", code, stderr)
	}
	if hanTextPattern.MatchString(stdout + stderr) {
		t.Fatalf("default English init leaked Han text:\n%s%s", stdout, stderr)
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if textassets.DefaultLocale != "en-US" {
		t.Fatalf("catalog default locale = %q", textassets.DefaultLocale)
	}
	if cfg.Locale != "en-US" {
		t.Fatalf("fresh locale = %q", cfg.Locale)
	}
	for _, relativePath := range []string{"AGENTS.md", "aoci.txt", "aoci.meta.txt", "aoci.code.txt", ".aoci/.gitignore"} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relativePath)))
		if err != nil {
			t.Fatal(err)
		}
		if hanTextPattern.Match(data) {
			t.Fatalf("%s leaked Han text:\n%s", relativePath, data)
		}
	}
	indexData, err := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(indexData), "#Locale: en-US") {
		t.Fatalf("English index lacks locale marker:\n%s", indexData)
	}
}

func TestFreshInitCanSelectChinese(t *testing.T) {
	root := t.TempDir()
	stdout, stderr, code := runLocaleInit(t, root, "--locale", "zh-CN")
	if code != ExitOK {
		t.Fatalf("init exit=%d stderr=%s", code, stderr)
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Locale != textassets.LegacyLocale {
		t.Fatalf("explicit locale = %q", cfg.Locale)
	}
	indexData, err := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(indexData), "#Locale: zh-CN") {
		t.Fatalf("Chinese index lacks locale marker:\n%s", indexData)
	}
	metaData, err := os.ReadFile(filepath.Join(root, "aoci.meta.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !hanTextPattern.MatchString(stdout + string(indexData) + string(metaData)) {
		t.Fatal("explicit Chinese init did not use the Chinese product language")
	}
}

func TestEnglishCLIErrorEnvelopeDoesNotLeakChinese(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig()
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"--repo", root, "config", "set", "automation_mode", "unbounded"},
		{"--repo", root, "--json", "config", "set", "automation_mode", "unbounded"},
	} {
		var stdout, stderr bytes.Buffer
		if code := executeCLI(args, &stdout, &stderr); code == ExitOK {
			t.Fatalf("invalid automation mode succeeded: %v", args)
		}
		combined := stdout.String() + stderr.String()
		if hanTextPattern.MatchString(combined) {
			t.Fatalf("English error output contains Han text for %v:\n%s", args, combined)
		}
		if !strings.Contains(combined, "error_code") {
			t.Fatalf("English error output lacks stable error classification for %v:\n%s", args, combined)
		}
	}
}

func TestChineseCLIErrorEnvelopeDoesNotLeakEnglishShell(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Locale = textassets.LegacyLocale
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	for _, arguments := range [][]string{
		{"--repo", root, "config", "set", "locale", "fr-FR"},
		{"--repo", root, "--json", "config", "set", "locale", "fr-FR"},
	} {
		var stdout, stderr bytes.Buffer
		if code := executeCLI(arguments, &stdout, &stderr); code != ExitConfig {
			t.Fatalf("invalid Locale exit=%d, want %d: %v", code, ExitConfig, arguments)
		}
		combined := stdout.String() + stderr.String()
		localized := combined
		if strings.HasPrefix(strings.TrimSpace(combined), "{") {
			var envelope struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal([]byte(combined), &envelope); err != nil {
				t.Fatalf("decode localized JSON error envelope: %v\n%s", err, combined)
			}
			localized = envelope.Message
		}
		if !hanTextPattern.MatchString(localized) || !strings.Contains(localized, "不支持Locale") {
			t.Fatalf("zh-CN error output is not localized for %v:\n%s", arguments, combined)
		}
		for _, leaked := range []string{"unsupported locale", "available:"} {
			if strings.Contains(combined, leaked) {
				t.Fatalf("zh-CN error output leaked %q for %v:\n%s", leaked, arguments, combined)
			}
		}
	}
}

func TestChineseLocaleSuppressesUnlocalizedMigrationDiagnostics(t *testing.T) {
	previous := textassets.ActiveLocale()
	if err := textassets.SetActiveLocale(textassets.LegacyLocale); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })

	for _, current := range []struct {
		detail string
		fact   string
	}{
		{"open /tmp/aoci/config.json: permission denied", "/tmp/aoci/config.json"},
		{`json: unknown field "unexpected"`, `"unexpected"`},
		{"dial tcp 127.0.0.1: connection refused", "127.0.0.1"},
	} {
		safe := localeSafeCLIDetail(current.detail)
		if !hanTextPattern.MatchString(safe) || strings.Contains(safe, current.detail) || !strings.Contains(safe, current.fact) {
			t.Fatalf("zh-CN detail was not localized with machine facts: input=%q output=%q", current.detail, safe)
		}
		rendered := localizedCLIErrorMessage(errors.New(current.detail), ExitInvalid)
		if !hanTextPattern.MatchString(rendered) || strings.Contains(rendered, current.detail) || !strings.Contains(rendered, current.fact) {
			t.Fatalf("zh-CN error shell was not localized with machine facts: input=%q output=%q", current.detail, rendered)
		}
	}
}

func TestLocaleCommandRewritesOnlyVolumeRootMarker(t *testing.T) {
	previousLocale := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previousLocale) })
	root := t.TempDir()
	_, stderr, code := runLocaleInit(t, root)
	if code != ExitOK {
		t.Fatalf("init exit=%d stderr=%s", code, stderr)
	}
	initializedConfig, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	saveCurrentBaseline(t, root, initializedConfig)
	before := map[string][]byte{}
	for _, rel := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt"} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		before[rel] = data
	}

	var stdoutBuffer, stderrBuffer bytes.Buffer
	code = executeCLI(
		[]string{"--repo", root, "config", "set", "locale", textassets.LegacyLocale},
		&stdoutBuffer,
		&stderrBuffer,
	)
	if code != ExitOK {
		t.Fatalf("Volume Locale change failed: code=%d stdout=%s stderr=%s", code, stdoutBuffer.String(), stderrBuffer.String())
	}
	wantRoot, err := index.ReplaceLocaleMarker(before["aoci.txt"], textassets.LegacyLocale)
	if err != nil {
		t.Fatal(err)
	}
	for rel, expected := range before {
		if rel == "aoci.txt" {
			expected = wantRoot
		}
		actual, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(actual, expected) {
			t.Fatalf("Volume Locale change modified bytes outside the Root Locale marker: %s", rel)
		}
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Locale != textassets.LegacyLocale || cfg.LocaleMigration != nil {
		t.Fatalf("Volume Locale state created migration debt: locale=%s receipt=%+v", cfg.Locale, cfg.LocaleMigration)
	}
}

func TestInitLocaleOnExistingVolumesUsesMarkerTransaction(t *testing.T) {
	previousLocale := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previousLocale) })
	root := t.TempDir()
	if _, stderr, code := runLocaleInit(t, root); code != ExitOK {
		t.Fatalf("init exit=%d stderr=%s", code, stderr)
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	saveCurrentBaseline(t, root, cfg)
	if stdout, stderr, code := runLocaleInit(t, root, "--locale", textassets.LegacyLocale); code != ExitOK {
		t.Fatalf("existing init Locale switch exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	cfg, err = config.LoadBase(root)
	if err != nil || cfg.Locale != textassets.LegacyLocale {
		t.Fatalf("existing init did not persist target Locale: cfg=%+v err=%v", cfg, err)
	}
	rootRaw, err := os.ReadFile(filepath.Join(root, cfg.IndexPath))
	if err != nil {
		t.Fatal(err)
	}
	rootLocale, explicit, err := index.DetectLocale(string(rootRaw))
	if err != nil || !explicit || rootLocale != cfg.Locale {
		t.Fatalf("existing init left Root/config Locale inconsistent: root=%s explicit=%t cfg=%s err=%v", rootLocale, explicit, cfg.Locale, err)
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists || state.Files[cfg.IndexPath].SHA256 != cognitiontxn.SHA256(rootRaw) {
		t.Fatalf("existing init did not bind the switched Root Baseline: exists=%t err=%v", exists, err)
	}
}

func TestInitLocaleOnExistingVolumesWithoutTeamConfigUsesMarkerTransaction(t *testing.T) {
	previousLocale := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previousLocale) })
	root := t.TempDir()
	if _, stderr, code := runLocaleInit(t, root); code != ExitOK {
		t.Fatalf("init exit=%d stderr=%s", code, stderr)
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	saveCurrentBaseline(t, root, cfg)
	before := map[string][]byte{}
	for _, rel := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt"} {
		before[rel], err = os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(config.FilePath(root)); err != nil {
		t.Fatal(err)
	}

	if stdout, stderr, code := runLocaleInit(t, root, "--locale", textassets.LegacyLocale); code != ExitOK {
		t.Fatalf("missing-config init Locale switch exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	cfg, err = config.LoadBase(root)
	if err != nil || cfg.Locale != textassets.LegacyLocale {
		t.Fatalf("missing-config init did not persist target Locale: cfg=%+v err=%v", cfg, err)
	}
	wantRoot, err := index.ReplaceLocaleMarker(before["aoci.txt"], textassets.LegacyLocale)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(localeSwitchRead(t, filepath.Join(root, "aoci.txt")), wantRoot) ||
		!bytes.Equal(localeSwitchRead(t, filepath.Join(root, "aoci.meta.txt")), before["aoci.meta.txt"]) ||
		!bytes.Equal(localeSwitchRead(t, filepath.Join(root, "aoci.code.txt")), before["aoci.code.txt"]) {
		t.Fatal("missing-config init did not perform a marker-only formal change")
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists || state.Files[cfg.IndexPath].SHA256 != cognitiontxn.SHA256(wantRoot) {
		t.Fatalf("missing-config init did not bind switched Root: exists=%t err=%v", exists, err)
	}
}
