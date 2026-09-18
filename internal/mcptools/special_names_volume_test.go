package mcptools

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/textassets"
)

// legacyCutCharacters are where the original header reading stopped a path.
const legacyCutCharacters = " \t=(（"

// copyRepositoryTree relocates a governed fixture the way a clone or a CI
// checkout does: same bytes, different absolute root.
func copyRepositoryTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(from, current)
		if err != nil {
			return err
		}
		target := filepath.Join(to, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(current)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// volumeTestFileSHA256 is the source fingerprint a direct update has to carry.
func volumeTestFileSHA256(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

var specialNameSources = []string{"src/deep dir/b.go", "app/(home)/page.go", "pkg/max=/c.go", "pages/docs/[...id].go"}

func specialNamesRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "example project", "repo")
}

// Issues #58 and #60, end to end through the MCP surface. A repository root with
// a space, a directory with a space, one with "=", one with "(", and a file
// whose name holds "[" (every Next.js, Nuxt, or SvelteKit dynamic route) could
// not be governed: the directory headers were read back truncated, so an Entry
// resolved to a path that does not exist and the repository reported the same
// file as orphan and missing for good; the bracketed file name failed its whole
// atomic batch. One ordinary batch now aligns all of them, under natural names.
func TestSpecialNamesAuthorToAlignmentUnderARootWithSpaces(t *testing.T) {
	root := buildVolumeRepoAt(t, specialNamesRoot(t), true, false, "")
	for _, rel := range specialNameSources {
		writeVolumeTestFile(t, root, rel, "package fixture\n")
	}
	enableProductionManagedScope(t, root)
	session := connectMCPClient(t, root)

	planned := maintainVolumeBatch(t, session)
	if planned.Status != autoStatusRepairRequired || planned.CodePlan == nil || len(planned.Candidates) != len(specialNameSources) {
		t.Fatalf("every special name must be an ordinary candidate: %#v", planned)
	}
	// The bracketed candidate names its Entry by full path, which the write path
	// normalizes to the section-relative name for a plain file and must do the same
	// for a bracketed one (it used to anchor on the first "[" and refuse it).
	arguments := codeBatchArguments(planned.CodePlan)
	for _, item := range arguments["entries"].([]map[string]any) {
		if item["path"] == "pages/docs/[...id].go" {
			item["new_entry"] = "pages/docs/" + item["new_entry"].(string)
		}
	}
	applied := applyVolumeBatch(t, session, arguments)
	if applied.Status != autoStatusApplied || !applied.Aligned {
		t.Fatalf("one ordinary batch must align the special names: status=%s aligned=%t findings=%#v", applied.Status, applied.Aligned, applied.Findings)
	}
	lines := cognitionEntryLines(t, root, cognition.ScopeCode)
	for _, rel := range specialNameSources {
		if !strings.HasPrefix(lines["code:"+rel], filepath.Base(rel)+"[") {
			t.Fatalf("code:%s did not resolve to its own Entry: %q", rel, lines["code:"+rel])
		}
	}
	// Directory names are written as they are. The root they hang from is the one
	// the index already uses: this fixture's root section carries the full root, and
	// every later section continues it as the original reading reads it back, which
	// is the shape every earlier release wrote and the only one in the wild.
	volume := volumeFileText(t, root, "aoci.code.txt")
	slashRoot := filepath.ToSlash(root)
	truncated := strings.TrimSuffix(slashRoot[:strings.IndexAny(slashRoot, legacyCutCharacters)], "/")
	for _, header := range []string{"/src/deep dir/===", "/app/(home)/===", "/pkg/max=/===", "/pages/docs/==="} {
		if !strings.Contains(volume, "==="+truncated+header+"\n") {
			t.Fatalf("section header must carry the natural directory name under the index's root ...%s:\n%s", header, volume)
		}
	}
	if again := maintainVolumeBatch(t, session); again.Status != autoStatusApplied || !again.Aligned || len(again.OrphanRemovals) != 0 {
		t.Fatalf("the aligned state must be stable on the next read: %#v", again)
	}
}

// A Code Volume exactly as v0.1.0-rc13 and earlier wrote it under such a root:
// the first section carries the full root, the later one hangs from the root the
// legacy reading truncated at the first space. That repository was wedged
// (orphan plus missing); on this binary it reads as aligned with no rewrite.
func TestCodeVolumeWrittenWithATruncatedRootReadsAligned(t *testing.T) {
	root := buildVolumeRepoAt(t, specialNamesRoot(t), true, false, "")
	writeVolumeTestFile(t, root, "src/deep dir/b.go", "package fixture\n")
	slashRoot := filepath.ToSlash(root)
	truncated := strings.TrimSuffix(slashRoot[:strings.IndexAny(slashRoot, legacyCutCharacters)], "/")
	volume := volumeFileText(t, root, "aoci.code.txt")
	writeVolumeTestFile(t, root, "aoci.code.txt", volume+"\n==="+truncated+"/src/deep dir/===\n"+
		"b.go[CD5T]: F:implement fixture behavior | R:- | A:- | S:Keep the fixture deterministic\n")
	enableProductionManagedScope(t, root)
	before, err := os.ReadFile(filepath.Join(root, "aoci.code.txt"))
	if err != nil {
		t.Fatal(err)
	}
	session := connectMCPClient(t, root)
	state := maintainVolumeBatch(t, session)
	if state.Status != autoStatusApplied || !state.Aligned || len(state.OrphanRemovals) != 0 || len(state.Candidates) != 0 {
		t.Fatalf("a Volume written under a truncated root must read as aligned, not as orphan plus missing: %#v", state)
	}
	if lines := cognitionEntryLines(t, root, cognition.ScopeCode); !strings.HasPrefix(lines["code:src/deep dir/b.go"], "b.go[") {
		t.Fatalf("the truncated-root section did not resolve: %v", lines)
	}
	if after, _ := os.ReadFile(filepath.Join(root, "aoci.code.txt")); string(after) != string(before) {
		t.Fatal("reading must not rewrite the Volume")
	}

	// This binary then adds a section to that index. It continues the truncated
	// spelling the section family already uses, and the repository stays aligned
	// both here and in a checkout at another path, which has no root to compare
	// the two spellings against. A full-spelled second section stranded that
	// checkout: every object came back orphan plus missing.
	writeVolumeTestFile(t, root, "lib/new dir/c.go", "package fixture\n")
	planned := maintainVolumeBatch(t, session)
	if planned.Status != autoStatusRepairRequired || planned.CodePlan == nil || len(planned.Candidates) != 1 {
		t.Fatalf("the new source must be one ordinary candidate: %#v", planned)
	}
	if applied := applyVolumeBatch(t, session, codeBatchArguments(planned.CodePlan)); applied.Status != autoStatusApplied || !applied.Aligned {
		t.Fatalf("authoring under the healed index must align: %#v", applied)
	}
	if volume := volumeFileText(t, root, "aoci.code.txt"); !strings.Contains(volume, "==="+truncated+"/lib/new dir/===\n") {
		t.Fatalf("the new section must continue the truncated spelling:\n%s", volume)
	}
	checkout := filepath.Join(t.TempDir(), "checkout")
	copyRepositoryTree(t, root, checkout)
	relocated := maintainVolumeBatch(t, connectMCPClient(t, checkout))
	if relocated.Status != autoStatusApplied || !relocated.Aligned || len(relocated.OrphanRemovals) != 0 || len(relocated.Candidates) != 0 {
		t.Fatalf("a checkout at another path must read the same index as aligned: %#v", relocated)
	}
	for _, rel := range []string{"main.go", "src/deep dir/b.go", "lib/new dir/c.go"} {
		if lines := cognitionEntryLines(t, checkout, cognition.ScopeCode); !strings.HasPrefix(lines["code:"+rel], filepath.Base(rel)+"[") {
			t.Fatalf("code:%s does not resolve in the checkout: %v", rel, lines)
		}
	}
}

// The rarer shape of the same old writer: the first file it met lived in a
// directory, so its one full-spelled section names that directory, the old reader
// took it for the root section, and the old writer filed main.go under it. Such
// a repository never aligned on v0.1.0-rc13. What matters now is that the origin
// and a checkout report the same thing (second review: they disagreed, so the two
// could never be aligned together) and that the ordinary repair aligns both.
func TestOldWriterIndexStartingInADirectoryConvergesEverywhere(t *testing.T) {
	root := buildVolumeRepoAt(t, specialNamesRoot(t), true, false, "")
	writeVolumeTestFile(t, root, "cmd/tool/gen.go", "package fixture\n")
	writeVolumeTestFile(t, root, "src/a.go", "package fixture\n")
	slashRoot := filepath.ToSlash(root)
	truncated := strings.TrimSuffix(slashRoot[:strings.IndexAny(slashRoot, legacyCutCharacters)], "/")
	volume := volumeFileText(t, root, "aoci.code.txt")
	rootHeader := "===Go sources" + slashRoot + "/===\n"
	if !strings.Contains(volume, rootHeader) {
		t.Fatalf("fixture header changed:\n%s", volume)
	}
	entry := func(name string) string {
		return name + "[CD5T]: F:implement fixture behavior | R:- | A:- | S:Keep the fixture deterministic\n"
	}
	volume = strings.Replace(volume, rootHeader, "==="+slashRoot+"/cmd/tool/===\n"+entry("gen.go"), 1) +
		"\n===" + truncated + "/src/===\n" + entry("a.go")
	writeVolumeTestFile(t, root, "aoci.code.txt", volume)
	enableProductionManagedScope(t, root)

	report := func(at string) (orphans, candidates []string) {
		state := maintainVolumeBatch(t, connectMCPClient(t, at))
		for _, candidate := range state.Candidates {
			candidates = append(candidates, candidate.Path)
		}
		return state.OrphanRemovals, candidates
	}
	checkout := filepath.Join(t.TempDir(), "checkout")
	copyRepositoryTree(t, root, checkout)
	originOrphans, originCandidates := report(root)
	cloneOrphans, cloneCandidates := report(checkout)
	if strings.Join(originOrphans, ",") != "code:gen.go" || strings.Join(originOrphans, ",") != strings.Join(cloneOrphans, ",") ||
		strings.Join(originCandidates, ",") != strings.Join(cloneCandidates, ",") {
		t.Fatalf("the origin and a checkout must report the same repair: origin %v %v, checkout %v %v",
			originOrphans, originCandidates, cloneOrphans, cloneCandidates)
	}

	// The ordinary repair, run at the origin: drop the orphan, author what is missing.
	session := connectMCPClient(t, root)
	callVolumeTool(t, session, "aoci_remove_entry", map[string]any{"path": originOrphans[0]})
	planned := maintainVolumeBatch(t, session)
	if planned.CodePlan == nil || len(planned.Candidates) != 1 || planned.Candidates[0].Path != "cmd/tool/gen.go" {
		t.Fatalf("after the orphan is gone the real file is the one candidate: %#v", planned)
	}
	if applied := applyVolumeBatch(t, session, codeBatchArguments(planned.CodePlan)); applied.Status != autoStatusApplied || !applied.Aligned {
		t.Fatalf("the repair must align the origin: %#v", applied)
	}
	if volume := volumeFileText(t, root, "aoci.code.txt"); !strings.Contains(volume, "==="+truncated+"/cmd/tool/===\n") {
		t.Fatalf("the repaired section continues the truncated root:\n%s", volume)
	}
	repaired := filepath.Join(t.TempDir(), "repaired checkout")
	copyRepositoryTree(t, root, repaired)
	if state := maintainVolumeBatch(t, connectMCPClient(t, repaired)); state.Status != autoStatusApplied || !state.Aligned ||
		len(state.OrphanRemovals) != 0 || len(state.Candidates) != 0 {
		t.Fatalf("a checkout of the repaired origin must read aligned too: %#v", state)
	}
}

// A fresh repository under such a root is written in full everywhere, and a
// checkout of it at another path reads aligned as well.
func TestFreshSpecialNamesRepositoryReadsAlignedInACheckout(t *testing.T) {
	root := buildVolumeRepoAt(t, specialNamesRoot(t), true, false, "")
	for _, rel := range specialNameSources {
		writeVolumeTestFile(t, root, rel, "package fixture\n")
	}
	enableProductionManagedScope(t, root)
	session := connectMCPClient(t, root)
	planned := maintainVolumeBatch(t, session)
	if applied := applyVolumeBatch(t, session, codeBatchArguments(planned.CodePlan)); applied.Status != autoStatusApplied || !applied.Aligned {
		t.Fatalf("fixture must align: %#v", applied)
	}
	checkout := filepath.Join(t.TempDir(), "checkout")
	copyRepositoryTree(t, root, checkout)
	if relocated := maintainVolumeBatch(t, connectMCPClient(t, checkout)); relocated.Status != autoStatusApplied || !relocated.Aligned || len(relocated.OrphanRemovals) != 0 {
		t.Fatalf("a checkout of a fresh special-names repository must read aligned: %#v", relocated)
	}
}

// A directory no header can spell back (a segment ending in whitespace) is
// refused with the path, the rule, and the way out. No Entry edit clears it, so
// the answer is stopped with the operator's action, never repair_required with a
// retry scope. Maintain finds it first and withholds the batch, because issuing
// it would have the model author every Entry only for the update to stop and the
// same batch come back (third review: a host that loops re-authors it forever).
// The update path gives the same answer to a caller that submits anyway, and
// renaming the directory, the first way out the message names, clears it.
func TestUnspellableDirectoryAnswersAStructuredRefusal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot create a directory whose name ends with a space")
	}
	root := buildVolumeRepo(t, true, false)
	writeVolumeTestFile(t, root, "trail /x.go", "package fixture\n")
	writeVolumeTestFile(t, root, "good/y.go", "package fixture\n")
	enableProductionManagedScope(t, root)
	session := connectMCPClient(t, root)
	before := volumeFileText(t, root, "aoci.code.txt")

	planned := maintainVolumeBatch(t, session)
	if planned.Status != autoStatusStopped || planned.Aligned || planned.NextAction != "resolve_unspellable_directory" ||
		len(planned.Candidates) != 0 || planned.CodePlan != nil {
		t.Fatalf("Maintain must withhold a batch that holds an unspellable directory: %#v", planned)
	}
	if planned.Stop == nil || planned.Stop.RuleCode != "code_directory_unspellable" || planned.Stop.AffectedAsset != "aoci.code.txt" ||
		!strings.Contains(planned.Stop.Actual, `"trail "`) ||
		planned.Stop.SafeNextAction != writeMessage("entry.repair.action.directory_unspellable") {
		t.Fatalf("the stop facts must name the rule, the asset, the quoted directory, and the way out: %#v", planned.Stop)
	}

	// A caller that submits anyway gets the same answer from the update path.
	refused := applyVolumeBatch(t, session, map[string]any{"entries": []map[string]any{{
		"path": "trail /x.go", "source_sha256": volumeTestFileSHA256(t, root, "trail /x.go"),
		"new_entry": "x.go[CD5T]: F:implement fixture behavior | R:- | A:- | S:Keep the fixture deterministic"}}})
	if refused.Status != autoStatusStopped || refused.Applied != 0 || refused.FormalWritesStarted {
		t.Fatalf("the write must stop before anything is written: %#v", refused)
	}
	if len(refused.RetryScope) != 0 || refused.PreserveOtherCandidates {
		t.Fatalf("nothing about this refusal is retryable: retry_scope=%v preserve=%t", refused.RetryScope, refused.PreserveOtherCandidates)
	}
	// The fixture's locale decides the wording; the contract is that next_action is
	// the operator's way out (it is the finding's own safe action), not a resubmit.
	if refused.NextAction != writeMessage("entry.repair.action.directory_unspellable") ||
		refused.NextAction == mcpContract(textassets.ContractMaintainActionUpdateRepair) || !strings.Contains(refused.NextAction, "aoci_maintain") {
		t.Fatalf("next_action must be the operator's way out, not a resubmit: %q", refused.NextAction)
	}
	if refused.Stop == nil || refused.Stop.RuleCode != "code_directory_unspellable" || !strings.Contains(refused.Stop.Actual, `"trail "`) {
		t.Fatalf("the stop facts must name the rule and the quoted directory: %#v", refused.Stop)
	}
	found := false
	for _, finding := range refused.Findings {
		if finding.RuleCode == "code_directory_unspellable" {
			// The directory is quoted so that the trailing space is visible.
			quoted := strings.Contains(finding.Cause, `"trail "`) || strings.Contains(finding.Cause, "“trail ”")
			found = finding.Path == "trail /x.go" && finding.Field == "path" && quoted &&
				strings.Contains(finding.SafeRepairAction, "aoci_maintain")
		}
	}
	if !found {
		t.Fatalf("the refusal must name the path, the field, the quoted directory, and the way out: %#v", refused.Findings)
	}
	if after := volumeFileText(t, root, "aoci.code.txt"); after != before {
		t.Fatal("a refused write changed the Volume")
	}

	// The way out the message names first: rename the directory. The batch is
	// issued again, both files author, and the repository aligns.
	if err := os.Rename(filepath.Join(root, "trail "), filepath.Join(root, "trail")); err != nil {
		t.Fatal(err)
	}
	renamed := maintainVolumeBatch(t, session)
	if renamed.Status != autoStatusRepairRequired || renamed.Stop != nil || renamed.CodePlan == nil || len(renamed.Candidates) != 2 {
		t.Fatalf("after the rename both files are ordinary candidates: %#v", renamed)
	}
	if applied := applyVolumeBatch(t, session, codeBatchArguments(renamed.CodePlan)); applied.Status != autoStatusApplied || !applied.Aligned {
		t.Fatalf("the renamed directory must author to alignment: %#v", applied)
	}
}
