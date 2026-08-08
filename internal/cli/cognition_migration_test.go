package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/migrationapply"
)

func TestCognitionMigrationSnapshotMachineJSONIsReadOnly(t *testing.T) {
	root := migrationCLIRepo(t, "en-US")
	legacyBefore, err := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if err != nil {
		t.Fatal(err)
	}
	baselineBefore, err := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := executeCLI([]string{"--repo", root, "--json", "cognition", "migration", "snapshot", "--kind", "code", "--captured-at", "2026-07-30T00:00:00Z"}, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("Snapshot failed: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var snapshot migrationapply.LegacySnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil || snapshot.Version != machinecontract.CognitionLegacySnapshotV1 ||
		snapshot.Eligibility != machinecontract.CognitionMigrationEligibilityEligible || snapshot.NetworkAccessed {
		t.Fatalf("Snapshot machine JSON invalid: %#v err=%v", snapshot, err)
	}
	legacyAfter, _ := os.ReadFile(filepath.Join(root, "aoci.txt"))
	baselineAfter, _ := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	if !bytes.Equal(legacyBefore, legacyAfter) || !bytes.Equal(baselineBefore, baselineAfter) {
		t.Fatal("Snapshot changed formal cognition bytes")
	}
	for _, path := range []string{"aoci.meta.txt", "aoci.code.txt", "aoci.database.txt"} {
		if _, err := os.Lstat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Fatalf("Snapshot created formal Volume %s", path)
		}
	}
}

func TestCognitionMigrationHelpFollowsBothOfficialLocales(t *testing.T) {
	for _, current := range []struct {
		locale string
		want   string
	}{
		{"en-US", "Migrate an approved Legacy cognition layout with exact recovery"},
		{"zh-CN", "以精确恢复能力迁移已批准的Legacy认知布局"},
	} {
		t.Run(current.locale, func(t *testing.T) {
			root := migrationCLIRepo(t, current.locale)
			var stdout, stderr bytes.Buffer
			if code := executeCLI([]string{"--repo", root, "cognition", "migration", "--help"}, &stdout, &stderr); code != ExitOK {
				t.Fatalf("help failed: code=%d stderr=%s", code, stderr.String())
			}
			if !strings.Contains(stdout.String(), current.want) || !strings.Contains(stdout.String(), "reversal") {
				t.Fatalf("Migration help is incomplete for %s: %s", current.locale, stdout.String())
			}
		})
	}
}

func migrationCLIRepo(t *testing.T, locale string) string {
	t.Helper()
	root := t.TempDir()
	initCLITestGitRepository(t, root)
	for _, directory := range []string{".aoci", "src"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.DefaultConfig()
	cfg.Locale = locale
	cfg.LedgerEnabled = true
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	source := []byte("package fixture\n")
	if err := os.WriteFile(filepath.Join(root, "src", "main.go"), source, 0o644); err != nil {
		t.Fatal(err)
	}
	legacy := strings.Join([]string{
		"#AOCI-CLI Complete Index", "#Project: Model-authored CLI migration fixture", "#[Tag dictionary]", "#A Layer: C Code",
		"===Source " + filepath.ToSlash(filepath.Join(root, "src")) + "/===",
		"main.go[CD9S]: F:Preserve CLI fixture responsibility | R:- | A:- | S:Keep formal bytes stable during read-only Snapshot",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	files := map[string]baseline.Fingerprint{}
	for _, relative := range []string{"aoci.txt", "src/main.go"} {
		fingerprint, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		files[relative] = fingerprint
	}
	value, err := baseline.NewBaselineAt(files, "2026-07-29T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	data, err := baseline.MarshalExact(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".aoci", "baseline.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}
