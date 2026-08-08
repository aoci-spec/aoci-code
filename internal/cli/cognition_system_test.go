package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
)

func TestCognitionSystemCLIProjectsImpactAndEvolutionWithoutNewState(t *testing.T) {
	root := cognitionSystemCLIRepo(t)
	baselineBefore, err := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "--json", "cognition", "system", "impact",
		"--object", "database://primary/public/orders"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("impact failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var impact cognition.DatabaseImpact
	if err := json.Unmarshal(stdout.Bytes(), &impact); err != nil {
		t.Fatal(err)
	}
	if !impact.Derived || !impact.Complete || len(impact.AffectedCodeObjects) != 1 ||
		impact.AffectedCodeObjects[0].ObjectRef != "code:main.go" || impact.NetworkAccessed {
		t.Fatalf("unexpected impact: %#v", impact)
	}
	stdout.Reset()
	stderr.Reset()
	if code := executeCLI([]string{"--repo", root, "--json", "cognition", "system", "snapshot"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("snapshot failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	snapshotPath := filepath.Join(root, "previous-cognition-snapshot.json")
	if err := os.WriteFile(snapshotPath, stdout.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	codePath := filepath.Join(root, "aoci.code.txt")
	codeBytes, err := os.ReadFile(codePath)
	if err != nil {
		t.Fatal(err)
	}
	changed := bytes.Replace(codeBytes, []byte("coordinate database access"), []byte("coordinate changed database access"), 1)
	if err := os.WriteFile(codePath, changed, 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := executeCLI([]string{"--repo", root, "--json", "cognition", "system", "evolution", "--snapshot-file", snapshotPath}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("evolution failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var evolution cognition.CognitionEvolution
	if err := json.Unmarshal(stdout.Bytes(), &evolution); err != nil {
		t.Fatal(err)
	}
	if !evolution.Derived || evolution.Summary.SemanticChanged != 1 || len(evolution.Changes) != 1 {
		t.Fatalf("unexpected evolution: %#v", evolution)
	}
	baselineAfter, err := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	if err != nil || !bytes.Equal(baselineBefore, baselineAfter) {
		t.Fatalf("derived commands changed Baseline: err=%v", err)
	}
}

func cognitionSystemCLIRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	initCLITestGitRepository(t, root)
	cfg := config.DefaultConfig()
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	meta := cognition.MetaVolumeMarker + "\n#Object-Protocol: repository-cognition-object/v2\n#FRAS-Discipline: 2\n" +
		"#FRAS-v2-Limits-Authority: machine-contract\n#S-Admission: non-inferable-and-error-preventing\n" +
		"#Object-Kinds: code=file database=table\n#[Tag dictionary: code]\n#A Layer: C Code\n#B Module: D Domain\n" +
		"#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n#[Tag dictionary: database]\n#A Layer: D Database\n" +
		"#B Module: B Business\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n"
	rootText := cognition.RootManifestMarker + "\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n#Project: system fixture\n" +
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled\n" +
		"#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta state=enabled\n" +
		"#Volume: id=database kind=database path=aoci.database.txt format=table-fras-v2 depends=meta state=enabled\n"
	codeEntry := "main.go[CD9S]: F:coordinate database access | R:database://primary/public/orders | A:- | S:Keep explicit relation evidence\n"
	databaseEntry := "orders[DB9S]: F:store order state | R:- | A:- | S:Preserve transactional ownership\n"
	files := map[string]string{
		"aoci.txt": rootText, "aoci.meta.txt": meta,
		"aoci.code.txt":     cognition.CodeVolumeMarker + "\n===Go sources" + filepath.ToSlash(root) + "/===\n" + codeEntry,
		"aoci.database.txt": cognition.DatabaseMarker + "\n===Primary tables/database://primary/public/===\n" + databaseEntry,
		"main.go":           "package main\n",
	}
	for rel, content := range files {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	state := baseline.NewBaseline(map[string]baseline.Fingerprint{})
	for _, rel := range []string{"main.go", "aoci.code.txt", "aoci.database.txt"} {
		fingerprint, err := baseline.HashFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		state.Files[rel] = fingerprint
	}
	line := databaseEntry[:len(databaseEntry)-1]
	digest := sha256.Sum256([]byte(line))
	if err := baseline.UpdateDatabaseCognitionBinding(state, baseline.DatabaseCognitionBinding{
		ObjectRef: "database://primary/public/orders", SourceID: "primary", EvidenceVersion: "database-evidence/v1",
		TableEvidenceSHA256: hex.EncodeToString(digest[:]), EntrySHA256: hex.EncodeToString(digest[:]),
	}); err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	return root
}
