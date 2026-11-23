package migrationapply

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
)

// A header atom may land only in the assets its record allows (Meta or
// Root); one resolved into a Code Volume is refused (audit G2-14).
func TestMigrationRefusesAHeaderAtomResolvedIntoCode(t *testing.T) {
	root := migrationFixture(t, false)
	plan, err := cognitionplan.MigrationPlan(cognitionplan.Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code"}})
	if err != nil {
		t.Fatal(err)
	}
	candidate := migrationCandidate(root, plan, []string{"code"})
	moved := false
	units := map[string]string{}
	for _, record := range plan.Mapping.Records {
		units[record.UnitID] = record.UnitKind
	}
	for index := range candidate.MappingResolutions {
		if units[candidate.MappingResolutions[index].UnitID] == "header_semantic_atom" {
			candidate.MappingResolutions[index].TargetAsset = "code"
			moved = true
			break
		}
	}
	if !moved {
		t.Skip("fixture has no header semantic atom")
	}
	preview, err := cognitionplan.ValidateCandidate(root, plan, candidate)
	found := false
	if preview != nil {
		for _, risk := range preview.Risks {
			found = found || risk.Code == "mapping_disposition_target_invalid"
		}
	}
	if !found {
		t.Fatalf("a header atom resolved into Code was accepted: err=%v preview=%+v", err, preview)
	}
}
