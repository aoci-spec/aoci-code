package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/textassets"
)

func TestDefaultLocaleIsEnglish(t *testing.T) {
	if textassets.DefaultLocale != "en-US" {
		t.Fatalf("catalog default locale = %q", textassets.DefaultLocale)
	}
	if got := DefaultConfig().Locale; got != "en-US" {
		t.Fatalf("new-project locale = %q, want en-US", got)
	}
}

func TestLegacyConfigMaterializesChineseWithoutRewritingIndex(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, DirName), 0o755); err != nil {
		t.Fatal(err)
	}
	legacyConfig := []byte("{\"version\":1,\"index_path\":\"aoci.txt\"}\n")
	if err := os.WriteFile(FilePath(root), legacyConfig, 0o644); err != nil {
		t.Fatal(err)
	}
	indexBefore := []byte("# AOCI cognition index\n旧版正式索引\n")
	indexPath := filepath.Join(root, "aoci.txt")
	if err := os.WriteFile(indexPath, indexBefore, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Locale != textassets.LegacyLocale || cfg.Version != 2 {
		t.Fatalf("legacy migration = locale %q version %d", cfg.Locale, cfg.Version)
	}
	indexAfter, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(indexAfter) != string(indexBefore) {
		t.Fatal("legacy locale materialization rewrote aoci.txt")
	}
	materialized, err := os.ReadFile(FilePath(root))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(materialized), `"locale": "zh-CN"`) {
		t.Fatalf("materialized config does not persist zh-CN:\n%s", materialized)
	}
}

func TestLegacyIndexWithoutConfigMaterializesChinese(t *testing.T) {
	root := t.TempDir()
	indexBefore := []byte("legacy index bytes\n")
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), indexBefore, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Locale != textassets.LegacyLocale {
		t.Fatalf("legacy index locale = %q", cfg.Locale)
	}
	indexAfter, err := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(indexAfter) != string(indexBefore) {
		t.Fatal("legacy detection rewrote aoci.txt")
	}
}

func TestExplicitIndexLocaleWithoutConfigMaterializesMarkerLocale(t *testing.T) {
	root := t.TempDir()
	indexBefore := []byte("#Locale: en-US\nexplicit index bytes\n")
	indexPath := filepath.Join(root, "aoci.txt")
	if err := os.WriteFile(indexPath, indexBefore, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Locale != textassets.DefaultLocale {
		t.Fatalf("explicit index locale = %q, want %q", cfg.Locale, textassets.DefaultLocale)
	}
	indexAfter, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(indexAfter) != string(indexBefore) {
		t.Fatal("explicit Locale materialization rewrote aoci.txt")
	}
	materialized, err := os.ReadFile(FilePath(root))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(materialized), `"locale": "en-US"`) {
		t.Fatalf("materialized config did not preserve explicit Locale:\n%s", materialized)
	}
}

func TestLocalConfigCannotOverrideLocale(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig()
	cfg.Locale = textassets.LegacyLocale
	if err := Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(LocalFilePath(root), []byte(`{"locale":"en-US"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Locale != textassets.LegacyLocale {
		t.Fatalf("local config overrode team locale: %q", loaded.Locale)
	}
}

func TestAdvanceLocaleMigrationIsIncrementalAndIdempotent(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig()
	cfg.Locale = textassets.LegacyLocale
	cfg.LocaleMigration = &LocaleMigration{
		FromLocale:    textassets.DefaultLocale,
		ToLocale:      textassets.LegacyLocale,
		HeaderPending: true,
		EntryPaths:    []string{"b.go", "a.go", ".aoci/ledger.jsonl"},
		CurationPaths: []string{"generated.bin"},
	}
	if err := Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := AdvanceLocaleMigration(root, true, []string{"a.go"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := AdvanceLocaleMigration(root, true, []string{"a.go"}, nil); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LocaleMigration == nil || loaded.LocaleMigration.HeaderPending ||
		len(loaded.LocaleMigration.EntryPaths) != 1 || loaded.LocaleMigration.EntryPaths[0] != "b.go" {
		t.Fatalf("unexpected partial receipt: %+v", loaded.LocaleMigration)
	}
	if err := AdvanceLocaleMigration(root, false, []string{"b.go"}, []string{"generated.bin"}); err != nil {
		t.Fatal(err)
	}
	loaded, err = LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LocaleMigration != nil {
		t.Fatalf("completed receipt was not cleared: %+v", loaded.LocaleMigration)
	}
}
