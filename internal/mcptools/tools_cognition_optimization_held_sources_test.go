package mcptools

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionoptimization"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// A repository that holds a binary index-role file is aligned under rc15
// governance (the file is a held source, never authoring debt). An
// optimization replacement written in that repository must finalize like any
// other: the batch's own alignment inspection has to classify held sources the
// way Verify does, or the optimization stops with checkpoint_recovery_required,
// leaves its entries receipt pending, and the identical retry stops the same
// way (#90).
func TestCognitionOptimizationReplacementFinalizesBesideHeldSources(t *testing.T) {
	root := buildManagedScopeCognitionOptimizationRepo(t, 3)
	const held = "optimized/logo.png"
	writeVolumeTestFile(t, root, held, "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00")
	state, _, err := baseline.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(held)))
	if err != nil {
		t.Fatal(err)
	}
	fingerprint.Role = machinecontract.ScopeRoleIndex
	state.Files[held] = fingerprint
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	assertOrdinaryVolumeGovernanceAligned(t, root)

	session := connectMCPClient(t, root)
	maintain := callCognitionOptimizationMaintain(t, session, []string{"code:optimized/0000.go"})
	assertOptimizationReviewBatch(t, maintain, 1, 1, 0)
	replacement := refinedOptimizationEntry(t, maintain.Candidates[0].ExistingEntry)
	arguments := optimizationUpdateArguments(maintain, func(int, optimizationCandidatePayload) string { return replacement })

	update := callCognitionOptimizationUpdate(t, session, arguments)
	if update.Status != autoStatusApplied || update.Applied != 1 || update.Optimization == nil || update.Optimization.State != "complete" {
		t.Fatalf("optimization replacement beside a held source did not finalize: %#v", update)
	}
	assertNoActiveOptimizationRecovery(t, root)
	assertOrdinaryVolumeGovernanceAligned(t, root)

	replay := callCognitionOptimizationUpdate(t, session, arguments)
	if replay.Status != autoStatusApplied || replay.Applied != 0 || replay.FormalWritesStarted {
		t.Fatalf("lost-response replay repeated or refused the finished optimization: %#v", replay)
	}
	entries, _ := os.ReadDir(filepath.Join(root, ".aoci", "transactions"))
	for _, entry := range entries {
		if !entry.IsDir() {
			t.Fatalf("a receipt remained after the finished optimization: %s", entry.Name())
		}
	}
}

// The all-no-change branch shares the finalization, so it must advance the
// checkpoint beside a held source as well.
func TestCognitionOptimizationNoChangeBatchAdvancesBesideHeldSources(t *testing.T) {
	root := buildManagedScopeCognitionOptimizationRepo(t, 3)
	const held = "optimized/empty.txt"
	writeVolumeTestFile(t, root, held, "")
	state, _, err := baseline.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(held)))
	if err != nil {
		t.Fatal(err)
	}
	fingerprint.Role = machinecontract.ScopeRoleIndex
	state.Files[held] = fingerprint
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	session := connectMCPClient(t, root)
	maintain := callCognitionOptimizationMaintain(t, session, []string{"code:optimized/0001.go"})
	update := callCognitionOptimizationUpdate(t, session, optimizationUpdateArguments(maintain,
		func(_ int, candidate optimizationCandidatePayload) string { return candidate.ExistingEntry }))
	if update.Status != autoStatusApplied || update.Applied != 0 || update.FormalWritesStarted ||
		update.Optimization == nil || update.Optimization.State != "complete" {
		t.Fatalf("no-change optimization beside a held source did not advance: %#v", update)
	}
	assertNoActiveOptimizationRecovery(t, root)
	assertOrdinaryVolumeGovernanceAligned(t, root)
}

// Optimization candidates are the longest Entries by construction, so the
// transport budget cuts a full batch. The cut must follow the selector's
// importance order and the issued batch must be exactly what the response
// carries; before v0.1.0-rc16 the count-selected batch was iterated over a
// byte-cut plan and Maintain failed with candidate_identity_missing.
func TestCognitionOptimizationBudgetCutsInImportanceOrderAndCompletes(t *testing.T) {
	root := buildCognitionOptimizationRepo(t, 30)
	// Default transport budget and cap, long Entries of alternating importance.
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaintainTransportBudgetBytes = 0
	if err := cfg.SetCodeCognitionBatchEntries(machinecontract.CodeCognitionBatchEntriesDefault); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	longS := strings.Repeat("Keep this fixture's externally visible behavior stable across refactors; ", 5)
	codePath := filepath.Join(root, "aoci.code.txt")
	raw, err := os.ReadFile(codePath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(raw), "\n")
	for index, line := range lines {
		if !strings.Contains(line, "]: F:") {
			continue
		}
		importance := "9"
		if index%2 == 0 {
			importance = "8"
		}
		head := line[:strings.Index(line, "[")]
		lines[index] = fmt.Sprintf("%s[CD%sS]: F:represent optimization fixture %s | R:- | A:- | S:%s", head, importance, head, strings.TrimSpace(longS))
	}
	if err := os.WriteFile(codePath, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	refreshVolumeBaseline(t, root)
	assertOrdinaryVolumeGovernanceAligned(t, root)

	session := connectMCPClient(t, root)
	reviewed := 0
	for round := 0; round < 12; round++ {
		maintain := callCognitionOptimizationMaintain(t, session, nil)
		if maintain.Optimization == nil || maintain.CodePlan == nil {
			t.Fatalf("round %d: optimization Maintain issued no batch: %#v", round, maintain)
		}
		included := maintain.CodePlan.Included
		if included != len(maintain.Candidates) || included < 1 || included >= 30 && round == 0 {
			t.Fatalf("round %d: the issued batch must be the candidates the response carries and the budget must cut a 30-Entry review: included=%d candidates=%d", round, included, len(maintain.Candidates))
		}
		if round == 0 {
			// The cut keeps the selector's order: the issued batch is the
			// leading prefix of the order the selector gives the whole review.
			set, err := cognition.Load(root, "aoci.txt")
			if err != nil {
				t.Fatal(err)
			}
			entries, err := optimizationAlignedEntries(root, nil, set.Volumes[cognition.ScopeCode], nil)
			if err != nil {
				t.Fatal(err)
			}
			selection, err := cognitionoptimization.Select(entries, cfg.EffectiveCognitionBudget(), cognitionoptimization.SelectOptions{MaxEntries: len(entries)})
			if err != nil {
				t.Fatal(err)
			}
			expected := make([]string, 0, included)
			for _, candidate := range selection.Batch[:included] {
				expected = append(expected, candidate.ObjectRef)
			}
			got := make([]string, 0, included)
			for _, candidate := range maintain.Candidates {
				got = append(got, candidate.ObjectRef)
			}
			if !reflect.DeepEqual(got, expected) {
				t.Fatalf("the budget cut broke the selector's order:\ngot=%v\nwant=%v", got, expected)
			}
		}
		update := callCognitionOptimizationUpdate(t, session, optimizationUpdateArguments(maintain,
			func(_ int, candidate optimizationCandidatePayload) string { return candidate.ExistingEntry }))
		if update.Status != autoStatusApplied || update.Optimization == nil {
			t.Fatalf("round %d: no-change batch failed: %#v", round, update)
		}
		reviewed = update.Optimization.Reviewed
		if update.Optimization.State == "complete" {
			break
		}
	}
	if reviewed != 30 {
		t.Fatalf("the budgeted rounds must review every Entry once: reviewed=%d", reviewed)
	}
	assertOrdinaryVolumeGovernanceAligned(t, root)
}
