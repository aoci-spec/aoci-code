package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
)

// TestReadObservationContractIsSharedByHelpAndDocs keeps the active-Locale
// boundary in command help and the matching canonical boundary in each public
// README language surface.
func TestReadObservationContractIsSharedByHelpAndDocs(t *testing.T) {
	contract := readObservationAuditHelp()

	longHelp := map[string]string{
		"index":           indexLongHelp(),
		"verify":          verifyLongHelp(),
		"check":           checkLongHelp(),
		"index score":     indexScoreLongHelp(),
		"index inventory": indexInventoryLongHelp(),
	}
	for name, help := range longHelp {
		if strings.Count(help, contract) != 1 {
			t.Fatalf("%s help must contain the canonical audit boundary once: %q", name, help)
		}
	}

	shortHelp := map[string]string{
		"verify":          verifyShortHelp(),
		"check":           checkShortHelp(),
		"index score":     indexScoreShortHelp(),
		"index inventory": indexInventoryShortHelp(),
	}
	commands := map[string]*cobra.Command{
		"verify":          newVerifyCmd(),
		"check":           newCheckCmd(),
		"index score":     newIndexScoreCmd(),
		"index inventory": newIndexInventoryCmd(),
	}
	for name, help := range shortHelp {
		if !strings.Contains(help, "不改正式索引或Baseline") {
			t.Fatalf("%s short help does not state the formal-asset boundary: %q", name, help)
		}
		if strings.Contains(help, "零副作用") {
			t.Fatalf("%s short help must not claim zero side effects: %q", name, help)
		}
		if commands[name].Short != help || commands[name].Long != longHelp[name] {
			t.Fatalf("%s command does not use the canonical Short and Long help", name)
		}
	}

	for _, relPath := range []string{
		filepath.Join("..", "..", "README.zh-CN.md"),
		filepath.Join("..", "..", "docs", "windows-host-agent.md"),
	} {
		data, err := os.ReadFile(relPath)
		if err != nil {
			t.Fatalf("read public documentation %s: %v", relPath, err)
		}
		if strings.Count(string(data), contract) != 1 {
			t.Fatalf("public documentation %s must contain the canonical audit boundary once", relPath)
		}
	}

	englishContract, err := textassets.Load(
		textassets.DefaultLocale,
		textassets.ContractHelpReadObservationAudit,
	)
	if err != nil {
		t.Fatalf("load canonical English audit boundary: %v", err)
	}
	englishREADME, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatalf("read English README: %v", err)
	}
	if strings.Count(string(englishREADME), englishContract) != 1 {
		t.Fatal("English README must contain the canonical audit boundary once")
	}
	for _, anchor := range []string{
		"strict zero-file-write operation",
		"all four may append to the local Ledger",
		"verify` also attempts to write Verify History",
	} {
		if !strings.Contains(string(englishREADME), anchor) {
			t.Fatalf("English README contract is missing %q", anchor)
		}
	}
}

// TestReadObservationCommandsKeepFormalAssetsAndWriteAuditEvidence proves the
// existing implementation boundary: formal assets stay byte-identical while
// local audit evidence is recorded under the default configuration.
func TestReadObservationCommandsKeepFormalAssetsAndWriteAuditEvidence(t *testing.T) {
	root := buildUpdateRepo(t)
	indexPath := filepath.Join(root, "aoci.txt")
	baselinePath := filepath.Join(root, ".aoci", "baseline.json")

	indexBefore, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	baselineBefore, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatal(err)
	}

	oldRepo := flagRepo
	oldJSON := flagJSON
	oldQuiet := flagQuiet
	flagRepo = root
	flagJSON = false
	flagQuiet = false
	t.Cleanup(func() {
		flagRepo = oldRepo
		flagJSON = oldJSON
		flagQuiet = oldQuiet
	})

	commands := []struct {
		name    string
		command *cobra.Command
	}{
		{name: "verify", command: newVerifyCmd()},
		{name: "check", command: newCheckCmd()},
		{name: "index_score", command: newIndexScoreCmd()},
		{name: "index_inventory", command: newIndexInventoryCmd()},
	}
	for _, current := range commands {
		var output bytes.Buffer
		current.command.SetOut(&output)
		current.command.SetErr(&output)
		if err := current.command.RunE(current.command, nil); err != nil {
			t.Fatalf("%s observation failed: %v\n%s", current.name, err, output.String())
		}
	}

	indexAfter, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	baselineAfter, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(indexBefore, indexAfter) {
		t.Fatal("observation commands must not modify the formal index")
	}
	if !bytes.Equal(baselineBefore, baselineAfter) {
		t.Fatal("observation commands must not modify the Baseline")
	}

	history, err := os.ReadDir(filepath.Join(root, ".aoci", "verify_history"))
	if err != nil {
		t.Fatalf("Verify History must be written: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("Verify must write one history snapshot, got %d", len(history))
	}

	events, corrupt := ledger.Recent(root, 20)
	if corrupt != 0 {
		t.Fatalf("local Ledger must stay valid, corrupt lines=%d", corrupt)
	}
	wantOps := map[string]bool{
		"verify":          false,
		"check":           false,
		"index_score":     false,
		"index_inventory": false,
	}
	for _, event := range events {
		if _, exists := wantOps[event.Op]; exists {
			wantOps[event.Op] = true
		}
	}
	for op, found := range wantOps {
		if !found {
			t.Fatalf("local Ledger is missing %s event: %+v", op, events)
		}
	}
}
