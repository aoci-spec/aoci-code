package scopechange

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// A Volume's Baseline record is a guard: whatever role a rule once recorded
// there, the guard carries index, so the next Apply writes the role back and
// the Plan never asks for the Volume to be reviewed as Observe evidence.
func TestFormalVolumeGuardsCarryTheIndexRole(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("aoci.txt", cognition.RootManifestMarker+"\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n"+
		"#Project: guard role fixture\n#Global-Invariants: -\n"+
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled\n"+
		"#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta state=enabled\n")
	write("aoci.meta.txt", cognition.MetaVolumeMarker+"\n#Object-Protocol: repository-cognition-object/v2\n#FRAS-Discipline: 2\n"+
		"#FRAS-v2-Limits-Authority: machine-contract\n#S-Admission: non-inferable-and-error-preventing\n"+
		"#Object-Kinds: code=file database=table\n#[Tag dictionary: code]\n#A Layer: C Code\n#B Module: D Domain\n"+
		"#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n#[Tag dictionary: database]\n#A Layer: D Database\n"+
		"#B Module: B Business\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n")
	write("main.go", "package main\n")
	write("aoci.code.txt", cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\n"+
		"main.go[CD7T]: F:run the deterministic fixture | R:- | A:main | S:Execution remains deterministic\n")
	active := baseline.NewBaseline(map[string]baseline.Fingerprint{})
	for _, rel := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt"} {
		fingerprint, err := baseline.HashFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		if rel == "aoci.code.txt" {
			fingerprint.Role = machinecontract.ScopeRoleObserve
		}
		active.Files[rel] = fingerprint
	}
	guards, err := FormalCognitionBaselineGuards(root, "aoci.txt", active, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt"} {
		if guards[rel].Role != machinecontract.ScopeRoleIndex || guards[rel].SHA256 != active.Files[rel].SHA256 {
			t.Fatalf("guard for %s does not carry the index role over the stored bytes: %+v", rel, guards[rel])
		}
	}
}
