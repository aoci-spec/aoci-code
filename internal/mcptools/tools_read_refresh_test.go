package mcptools

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func buildSemanticRefreshRepo(t *testing.T, existing int) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".aoci"), 0o755); err != nil {
		t.Fatal(err)
	}

	var indexText strings.Builder
	indexText.WriteString("# AOCI test index\n===Sources " + filepath.ToSlash(root) + "/src/===\n")
	for number := 0; number < existing; number++ {
		name := fmt.Sprintf("file-%03d.txt", number)
		if err := os.WriteFile(
			filepath.Join(root, "src", name),
			[]byte(fmt.Sprintf("baseline %03d\n", number)),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&indexText, "%s[X.Y.5.T]: F:test | R:- | A:- | S:-\n", name)
	}
	if err := os.WriteFile(
		filepath.Join(root, ".aoci", "index.txt"),
		[]byte(indexText.String()),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg := legacyTestConfig()
	cfg.IndexPath = ".aoci/index.txt"
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	snapshot, warnings, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil || len(warnings) != 0 {
		t.Fatalf("snapshot: warnings=%v err=%v", warnings, err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}
	return root
}

func inspectRefreshFactsForTest(t *testing.T, root string) semanticChangeFacts {
	t.Helper()
	repository, fail := loadRepoCtx(root)
	if fail != nil {
		t.Fatal(fail.Msg)
	}
	facts, fail := inspectSemanticChanges(root, repository)
	if fail != nil {
		t.Fatal(fail.Msg)
	}
	return facts
}

func TestSemanticChangeCountDefaultThresholdMatrix(t *testing.T) {
	t.Run("29 modified", func(t *testing.T) {
		root := buildSemanticRefreshRepo(t, 30)
		for number := 0; number < 29; number++ {
			path := filepath.Join(root, "src", fmt.Sprintf("file-%03d.txt", number))
			if err := os.WriteFile(path, []byte("changed\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		facts := inspectRefreshFactsForTest(t, root)
		if facts.Count != 29 || facts.Threshold != 30 {
			t.Fatalf("facts = %+v", facts)
		}
	})

	t.Run("30 modified", func(t *testing.T) {
		root := buildSemanticRefreshRepo(t, 30)
		for number := 0; number < 30; number++ {
			path := filepath.Join(root, "src", fmt.Sprintf("file-%03d.txt", number))
			if err := os.WriteFile(path, []byte("changed\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		facts := inspectRefreshFactsForTest(t, root)
		if facts.Count != 30 || facts.SemanticStale != 30 {
			t.Fatalf("facts = %+v", facts)
		}
	})

	t.Run("30 new", func(t *testing.T) {
		root := buildSemanticRefreshRepo(t, 1)
		for number := 0; number < 30; number++ {
			path := filepath.Join(root, "src", fmt.Sprintf("new-%03d.txt", number))
			if err := os.WriteFile(path, []byte("new semantic file\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		facts := inspectRefreshFactsForTest(t, root)
		if facts.Count != 30 || facts.ActionableMissing != 30 {
			t.Fatalf("facts = %+v", facts)
		}
	})

	t.Run("20 modified plus 10 new", func(t *testing.T) {
		root := buildSemanticRefreshRepo(t, 20)
		for number := 0; number < 20; number++ {
			path := filepath.Join(root, "src", fmt.Sprintf("file-%03d.txt", number))
			if err := os.WriteFile(path, []byte("changed\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		for number := 0; number < 10; number++ {
			path := filepath.Join(root, "src", fmt.Sprintf("new-%03d.txt", number))
			if err := os.WriteFile(path, []byte("new\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		facts := inspectRefreshFactsForTest(t, root)
		if facts.Count != 30 || facts.SemanticStale != 20 || facts.ActionableMissing != 10 {
			t.Fatalf("facts = %+v", facts)
		}
	})
}

func TestSemanticChangeCountDeduplicatesRestoresAndExcludes(t *testing.T) {
	root := buildSemanticRefreshRepo(t, 1)
	target := filepath.Join(root, "src", "file-000.txt")
	for edit := 0; edit < 30; edit++ {
		if err := os.WriteFile(target, []byte(fmt.Sprintf("edit %d\n", edit)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := inspectRefreshFactsForTest(t, root).Count; got != 1 {
		t.Fatalf("30 edits of one path counted as %d", got)
	}
	if err := os.WriteFile(target, []byte("baseline 000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := inspectRefreshFactsForTest(t, root).Count; got != 0 {
		t.Fatalf("restored Baseline path counted as %d", got)
	}

	for _, path := range []string{
		filepath.Join(root, "build", "generated.txt"),
		filepath.Join(root, ".aoci", "runtime.tmp"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("ignored\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := inspectRefreshFactsForTest(t, root).Count; got != 0 {
		t.Fatalf("technical or runtime assets counted as %d", got)
	}
}

func TestSemanticChangeCountExcludesFormatOnly(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".aoci"), 0o755); err != nil {
		t.Fatal(err)
	}
	var indexText strings.Builder
	indexText.WriteString("# AOCI test index\n===Sources " + filepath.ToSlash(root) + "/src/===\n")
	formattedByPath := map[string][]byte{}
	for number := 0; number < 30; number++ {
		name := fmt.Sprintf("format-%03d.go", number)
		unformatted := []byte(fmt.Sprintf("package p\nfunc f%d(){println(%d)}\n", number, number))
		formatted, err := format.Source(unformatted)
		if err != nil {
			t.Fatal(err)
		}
		formattedByPath[name] = formatted
		if err := os.WriteFile(filepath.Join(root, "src", name), unformatted, 0o644); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&indexText, "%s[X.Y.5.T]: F:test | R:- | A:- | S:-\n", name)
	}
	if err := os.WriteFile(filepath.Join(root, ".aoci", "index.txt"), []byte(indexText.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := legacyTestConfig()
	cfg.IndexPath = ".aoci/index.txt"
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}
	for name, formatted := range formattedByPath {
		if err := os.WriteFile(filepath.Join(root, "src", name), formatted, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	facts := inspectRefreshFactsForTest(t, root)
	if facts.Count != 0 || facts.FormatOnly != 30 {
		t.Fatalf("facts = %+v", facts)
	}
}

func TestSemanticChangeCountCurationBoundaries(t *testing.T) {
	t.Run("pending curation counts", func(t *testing.T) {
		root := buildSemanticRefreshRepo(t, 1)
		if err := os.WriteFile(filepath.Join(root, "src", "empty.bin"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		facts := inspectRefreshFactsForTest(t, root)
		if facts.Count != 1 || facts.PendingCuration != 1 || facts.TechnicalSkipped != 0 {
			t.Fatalf("pending Curation facts = %+v", facts)
		}
	})

	t.Run("team curation exclusion does not count", func(t *testing.T) {
		root := buildSemanticRefreshRepo(t, 1)
		cfg, err := config.LoadBase(root)
		if err != nil {
			t.Fatal(err)
		}
		cfg.CurationExclude = []string{"docs"}
		if err := config.Save(root, cfg); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(
			filepath.Join(root, "docs", "excluded.md"),
			[]byte("# excluded\n"),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
		facts := inspectRefreshFactsForTest(t, root)
		if facts.Count != 0 || facts.CurationExcluded != 1 {
			t.Fatalf("Curation exclusion facts = %+v", facts)
		}
	})
}

func TestSemanticChangeCountExcludesLineEndingOnly(t *testing.T) {
	root := buildSemanticRefreshRepo(t, 30)
	for number := 0; number < 30; number++ {
		path := filepath.Join(root, "src", fmt.Sprintf("file-%03d.txt", number))
		content := fmt.Sprintf("baseline %03d\r\n", number)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	facts := inspectRefreshFactsForTest(t, root)
	if facts.Count != 0 || facts.LineEndingOnly != 30 {
		t.Fatalf("line-ending-only facts = %+v", facts)
	}
}

func deliveredTestSession() (*cognitionRefreshSession, cognitionReceipt, semanticChangeFacts) {
	session := newCognitionRefreshSession()
	current := newCognitionReceipt("/repo", "v1", "index", cognitionScopeRepositoryFull)
	clean := semanticChangeFacts{
		Threshold:         30,
		PathSetSHA256:     strings.Repeat("0", 64),
		GovernanceAligned: true,
	}
	initial := session.evaluate(overviewIn{}, current, clean, nil, "")
	receipt := session.deliveredReceipt(current, "")
	session.markDeliveryAttempt(receipt)
	if initial.RefreshStatus != machinecontract.RefreshStatusReadyForOverview {
		panic("test setup did not establish initial cognition")
	}
	return session, current, clean
}

func TestRefreshSessionThresholdAndMergedReasons(t *testing.T) {
	session, current, clean := deliveredTestSession()
	below := clean
	below.Count = 29
	below.SemanticStale = 29
	below.GovernanceAligned = false
	below.GovernanceBlockerCount = 29
	assessment := session.evaluate(overviewIn{CheckOnly: true}, current, below, nil, "")
	if assessment.RefreshStatus != machinecontract.RefreshStatusNotRequired {
		t.Fatalf("29 changes unexpectedly triggered refresh: %+v", assessment)
	}

	atThreshold := below
	atThreshold.Count = 30
	atThreshold.SemanticStale = 30
	atThreshold.GovernanceBlockerCount = 30
	assessment = session.evaluate(overviewIn{CheckOnly: true}, current, atThreshold, nil, "")
	if assessment.RefreshStatus != machinecontract.RefreshStatusDeferredUntilStable ||
		!containsRefreshReason(assessment.RefreshReasons, machinecontract.RefreshReasonSemanticThreshold) {
		t.Fatalf("threshold did not defer until stable: %+v", assessment)
	}

	stable := true
	hostReasons := []string{
		machinecontract.RefreshReasonContextCompaction,
		machinecontract.RefreshReasonPhaseTransition,
	}
	assessment = session.evaluate(
		overviewIn{CheckOnly: true, StableCheckpoint: &stable},
		current,
		atThreshold,
		hostReasons,
		"merged-1",
	)
	if assessment.RefreshStatus != machinecontract.RefreshStatusRequired ||
		len(assessment.RefreshReasons) != 3 {
		t.Fatalf("merged trigger did not require one maintenance: %+v", assessment)
	}

	assessment = session.evaluate(overviewIn{}, current, clean, nil, "")
	if assessment.RefreshStatus != machinecontract.RefreshStatusReadyForOverview ||
		len(assessment.RefreshReasons) != 3 {
		t.Fatalf("aligned pending trigger not ready: %+v", assessment)
	}
	receipt := session.deliveredReceipt(current, "")
	session.markDeliveryAttempt(receipt)
	repeated := session.evaluate(overviewIn{}, current, clean, hostReasons, "merged-1")
	if repeated.RefreshStatus != machinecontract.RefreshStatusNotRequired {
		t.Fatalf("repeated merged event retransmitted: %+v", repeated)
	}
}

func TestRefreshSessionCustomThresholds(t *testing.T) {
	for _, current := range []struct {
		threshold int
		count     int
		want      string
	}{
		{threshold: 5, count: 5, want: machinecontract.RefreshStatusDeferredUntilStable},
		{threshold: 50, count: 49, want: machinecontract.RefreshStatusNotRequired},
	} {
		session, receipt, facts := deliveredTestSession()
		facts.Threshold = current.threshold
		facts.Count = current.count
		facts.SemanticStale = current.count
		facts.GovernanceAligned = false
		facts.GovernanceBlockerCount = current.count
		assessment := session.evaluate(overviewIn{CheckOnly: true}, receipt, facts, nil, "")
		if assessment.RefreshStatus != current.want {
			t.Fatalf("threshold=%d count=%d assessment=%+v", current.threshold, current.count, assessment)
		}
	}
}

func TestNormalizeRefreshInputRejectsHostContractViolations(t *testing.T) {
	tests := []overviewIn{
		{RefreshReasons: []string{machinecontract.RefreshReasonSemanticThreshold}, RefreshEventID: "host-cannot-declare"},
		{RefreshReasons: []string{machinecontract.RefreshReasonContextCompaction}},
		{RefreshReasons: []string{machinecontract.RefreshReasonPhaseTransition}, RefreshEventID: "bad\nevent"},
		{RefreshReasons: []string{machinecontract.RefreshReasonPhaseTransition}, RefreshEventID: "bad\x00event"},
	}
	for _, input := range tests {
		if _, _, fail := normalizeRefreshInput(input); fail == nil {
			t.Fatalf("invalid refresh input accepted: %+v", input)
		}
	}

	reasons, eventID, fail := normalizeRefreshInput(overviewIn{
		RefreshReasons: []string{
			machinecontract.RefreshReasonPhaseTransition,
			machinecontract.RefreshReasonContextCompaction,
			machinecontract.RefreshReasonPhaseTransition,
		},
		RefreshEventID: "merged-event",
	})
	if fail != nil || eventID != "merged-event" || len(reasons) != 2 {
		t.Fatalf("valid merged input = reasons=%v event=%q fail=%+v", reasons, eventID, fail)
	}
}

func TestLegacyInvalidOnlyDeclaresCompactionFromReliableReceipt(t *testing.T) {
	reasons, eventID, fail := normalizeRefreshInput(overviewIn{
		ModelState: cognitionStateInvalid,
	})
	if fail != nil || len(reasons) != 0 || eventID != "" {
		t.Fatalf("initial invalid assessment invented compaction: reasons=%v event=%q fail=%+v", reasons, eventID, fail)
	}

	uncertain := newCognitionReceipt("/repo", "v1", "index", cognitionScopeRepositoryFull)
	reasons, eventID, fail = normalizeRefreshInput(overviewIn{
		Receipt:    &uncertain,
		ModelState: cognitionStateInvalid,
	})
	if fail != nil || len(reasons) != 0 || eventID != "" {
		t.Fatalf("governance receipt invented compaction: reasons=%v event=%q fail=%+v", reasons, eventID, fail)
	}

	reliable := receiptWithState(uncertain, cognitionStateValid, true)
	reasons, eventID, fail = normalizeRefreshInput(overviewIn{
		Receipt:    &reliable,
		ModelState: cognitionStateInvalid,
	})
	if fail != nil || len(reasons) != 1 ||
		reasons[0] != machinecontract.RefreshReasonContextCompaction ||
		!strings.HasPrefix(eventID, "legacy-") {
		t.Fatalf("reliable legacy receipt did not declare compaction: reasons=%v event=%q fail=%+v", reasons, eventID, fail)
	}
}

func TestRefreshSessionCompactionAndPhaseTransition(t *testing.T) {
	t.Run("clean compaction once", func(t *testing.T) {
		session, current, clean := deliveredTestSession()
		reasons := []string{machinecontract.RefreshReasonContextCompaction}
		assessment := session.evaluate(overviewIn{}, current, clean, reasons, "compact-1")
		if assessment.RefreshStatus != machinecontract.RefreshStatusReadyForOverview {
			t.Fatalf("clean compaction = %+v", assessment)
		}
		session.markDeliveryAttempt(session.deliveredReceipt(current, "compact-1"))
		repeated := session.evaluate(overviewIn{}, current, clean, reasons, "compact-1")
		if repeated.RefreshStatus != machinecontract.RefreshStatusNotRequired {
			t.Fatalf("duplicate compaction = %+v", repeated)
		}
	})

	t.Run("clean phase does not refresh", func(t *testing.T) {
		session, current, clean := deliveredTestSession()
		assessment := session.evaluate(
			overviewIn{},
			current,
			clean,
			[]string{machinecontract.RefreshReasonPhaseTransition},
			"phase-1",
		)
		if assessment.RefreshStatus != machinecontract.RefreshStatusNotRequired {
			t.Fatalf("clean phase = %+v", assessment)
		}
	})

	t.Run("dirty phase waits for maintenance", func(t *testing.T) {
		session, current, clean := deliveredTestSession()
		dirty := clean
		dirty.Count = 1
		dirty.SemanticStale = 1
		dirty.GovernanceBlockerCount = 1
		dirty.GovernanceAligned = false
		stable := true
		assessment := session.evaluate(
			overviewIn{StableCheckpoint: &stable},
			current,
			dirty,
			[]string{machinecontract.RefreshReasonPhaseTransition},
			"phase-dirty",
		)
		if assessment.RefreshStatus != machinecontract.RefreshStatusRequired {
			t.Fatalf("dirty phase = %+v", assessment)
		}
	})
}

func TestRefreshSessionPartialAttestationConsumesEventWithoutGrantingReliability(t *testing.T) {
	session, current, clean := deliveredTestSession()
	reasons := []string{machinecontract.RefreshReasonContextCompaction}
	assessment := session.evaluate(overviewIn{}, current, clean, reasons, "compact-partial")
	if assessment.RefreshStatus != machinecontract.RefreshStatusReadyForOverview {
		t.Fatalf("clean compaction = %+v", assessment)
	}
	delivered := session.deliveredReceipt(current, "compact-partial")
	session.recordAttestedDelivery(
		delivered,
		overviewIn{ModelAttestation: &overviewModelAttestation{}},
		overviewAttestationResult{
			DeliveryIntegrity:     deliveryIntegrityConfirmed,
			ModelAttestation:      modelAttestationFail,
			CognitionAssimilation: cognitionAssimilationUncertain,
		},
		true,
	)

	repeated := session.evaluate(overviewIn{}, current, clean, reasons, "compact-partial")
	if repeated.RefreshStatus != machinecontract.RefreshStatusNotRequired ||
		repeated.State != cognitionStateUncertain ||
		repeated.Receipt.ModelFullReliable ||
		repeated.Receipt.RefreshGeneration != delivered.RefreshGeneration {
		t.Fatalf("partial Attestation did not consume one generation safely: %+v", repeated)
	}
}

func TestRefreshSessionDoesNotAdoptUnreliableReceiptAsValidIdentity(t *testing.T) {
	current := newCognitionReceipt("/repo", "v1", "index", cognitionScopeRepositoryFull)
	uncertain := receiptWithRefresh(
		receiptWithState(current, cognitionStateUncertain, false),
		3,
		"failed-refresh",
		nil,
	)
	clean := semanticChangeFacts{Threshold: 30, GovernanceAligned: true}
	session := newCognitionRefreshSession()
	assessment := session.evaluate(
		overviewIn{Receipt: &uncertain, ModelState: cognitionStateUncertain},
		current,
		clean,
		nil,
		"",
	)
	if !assessment.InitialCognition || assessment.RefreshStatus != machinecontract.RefreshStatusReadyForOverview {
		t.Fatalf("unreliable receipt was adopted as established cognition: %+v", assessment)
	}
}

func TestRefreshReceiptSuppressesConsumedEventAfterSessionRestart(t *testing.T) {
	current := newCognitionReceipt("/repo", "v1", "index", cognitionScopeRepositoryFull)
	receipt := receiptWithRefresh(
		receiptWithState(current, cognitionStateValid, true),
		4,
		"compaction-before-restart",
		nil,
	)
	clean := semanticChangeFacts{
		Threshold:         30,
		PathSetSHA256:     strings.Repeat("0", 64),
		GovernanceAligned: true,
	}
	session := newCognitionRefreshSession()
	assessment := session.evaluate(
		overviewIn{Receipt: &receipt, ModelState: cognitionStateValid},
		current,
		clean,
		[]string{machinecontract.RefreshReasonContextCompaction},
		"compaction-before-restart",
	)
	if assessment.RefreshStatus != machinecontract.RefreshStatusNotRequired ||
		assessment.Receipt.RefreshGeneration != 4 {
		t.Fatalf("receipt did not preserve event idempotency: %+v", assessment)
	}
}

func TestMergedPendingEventReceiptIDIsDeterministic(t *testing.T) {
	left := newCognitionRefreshSession()
	left.pendingEvents["event-b"] = true
	left.pendingEvents["event-a"] = true
	right := newCognitionRefreshSession()
	right.pendingEvents["event-a"] = true
	right.pendingEvents["event-b"] = true
	if got, want := left.pendingEventReceiptID(), right.pendingEventReceiptID(); got == "" || got != want || !strings.HasPrefix(got, "merged-") {
		t.Fatalf("merged receipt ids differ: got=%q want=%q", got, want)
	}
}

func containsRefreshReason(values []string, wanted string) bool {
	position := sort.SearchStrings(values, wanted)
	return position < len(values) && values[position] == wanted
}
