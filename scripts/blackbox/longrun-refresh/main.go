// Command longrun-refresh exercises the R67-A refresh contract through a real
// AOCI binary and two disposable Git repositories. It is intentionally outside
// the product runtime: it observes MCP and CLI behavior and never supplies an
// alternative governance path.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/index"
)

const (
	statusNotRequired = "refresh_not_required"
	statusRequired    = "refresh_required"
	statusDeferred    = "refresh_deferred_until_stable"
	statusReady       = "refresh_ready_for_overview"
)

type runReport struct {
	Version       int                `json:"version"`
	GeneratedAt   string             `json:"generated_at"`
	Binary        artifactIdentity   `json:"binary"`
	Environment   environmentFacts   `json:"environment"`
	Experiments   []experimentReport `json:"experiments"`
	TotalWallMS   int64              `json:"total_wall_ms"`
	OverallPassed bool               `json:"overall_passed"`
}

type artifactIdentity struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Version string `json:"version"`
}

type environmentFacts struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	GoRuntime    string `json:"go_runtime"`
}

type experimentReport struct {
	ID                     string               `json:"id"`
	Task                   string               `json:"task"`
	RepositoryCommit       string               `json:"repository_commit"`
	Threshold              int                  `json:"threshold"`
	RulesReads             int                  `json:"rules_reads"`
	OverviewFullReads      int                  `json:"overview_full_reads"`
	OverviewCompactReads   int                  `json:"overview_compact_reads"`
	MaintainCalls          int                  `json:"maintain_calls"`
	LocalRecallCalls       int                  `json:"local_recall_calls"`
	SemanticCounts         []semanticCheckpoint `json:"semantic_counts"`
	TriggerReasons         []string             `json:"trigger_reasons"`
	IndexSHA256Before      string               `json:"index_sha256_before"`
	IndexSHA256After       string               `json:"index_sha256_after"`
	IndexBodyBytes         int                  `json:"index_body_bytes_transmitted"`
	EstimatedTokens        int                  `json:"estimated_index_tokens_transmitted"`
	DeterministicMS        int64                `json:"deterministic_ms"`
	WallMS                 int64                `json:"wall_ms"`
	HostEventSimulation    bool                 `json:"host_event_simulation"`
	DirtyOverviewDelivered bool                 `json:"dirty_overview_delivered_unreliable"`
	RepeatedBodyDelivered  bool                 `json:"repeated_body_delivered"`
	AttestationPassed      string               `json:"attestation_passed"`
	FullSystemClaimAllowed bool                 `json:"full_system_claim_allowed"`
	SourceBoundContinued   bool                 `json:"source_bound_continued"`
	RefreshLoopPrevented   bool                 `json:"refresh_loop_prevented"`
	QuestionAnswered       bool                 `json:"question_answered_then_continued"`
	RepairAttempts         int                  `json:"repair_attempts"`
	ToolsCount             int                  `json:"tools_count"`
	Verify                 commandEvidence      `json:"verify"`
	Check                  commandEvidence      `json:"check"`
	Guide                  commandEvidence      `json:"guide"`
	Ledger                 ledgerEvidence       `json:"ledger"`
	Passed                 bool                 `json:"passed"`
}

type semanticCheckpoint struct {
	Label   string   `json:"label"`
	Count   int      `json:"count"`
	Status  string   `json:"status"`
	Reasons []string `json:"reasons"`
}

type commandEvidence struct {
	ExitCode int    `json:"exit_code"`
	SHA256   string `json:"output_sha256"`
	Output   string `json:"output"`
}

type ledgerEvidence struct {
	Events            int  `json:"events"`
	RulesReads        int  `json:"rules_reads"`
	OverviewFullReads int  `json:"overview_full_reads"`
	CognitionChecks   int  `json:"cognition_checks"`
	MaintainCalls     int  `json:"maintain_calls"`
	LocalRecallCalls  int  `json:"local_recall_calls"`
	SemanticPeak      int  `json:"semantic_peak"`
	Consistent        bool `json:"consistent"`
}

type assessment struct {
	State          string   `json:"state"`
	RefreshStatus  string   `json:"refresh_status"`
	RefreshReasons []string `json:"refresh_reasons"`
	Semantic       struct {
		Count     int `json:"semantic_change_count"`
		Threshold int `json:"semantic_change_threshold"`
	} `json:"semantic"`
	NextAction string `json:"next_action"`
}

type autoCandidate struct {
	Path         string `json:"path"`
	SourceSHA256 string `json:"source_sha256"`
}

type autoResult struct {
	Status     string          `json:"status"`
	Aligned    bool            `json:"aligned"`
	Candidates []autoCandidate `json:"candidates"`
	Findings   []string        `json:"findings"`
	Metrics    struct {
		DeterministicMS int64 `json:"deterministic_ms"`
	} `json:"metrics"`
}

type rpcClient struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	stderr  bytes.Buffer
	nextID  int
}

func main() {
	binary := flag.String("binary", "", "absolute path to the candidate AOCI binary")
	output := flag.String("output", "", "optional report output path")
	keep := flag.Bool("keep", false, "retain disposable repositories")
	flag.Parse()
	if *binary == "" {
		fatal(errors.New("--binary is required"))
	}
	absBinary, err := filepath.Abs(*binary)
	if err != nil {
		fatal(err)
	}
	started := time.Now()
	identity, err := identifyBinary(absBinary)
	if err != nil {
		fatal(err)
	}
	base, err := os.MkdirTemp("", "aoci-r67-longrun-")
	if err != nil {
		fatal(err)
	}
	if !*keep {
		defer os.RemoveAll(base)
	}

	threshold, err := runThresholdExperiment(absBinary, filepath.Join(base, "threshold"))
	if err != nil {
		fatal(fmt.Errorf("threshold experiment: %w", err))
	}
	compaction, err := runCompactionExperiment(absBinary, filepath.Join(base, "compaction"))
	if err != nil {
		fatal(fmt.Errorf("compaction experiment: %w", err))
	}
	report := runReport{
		Version:     1,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Binary:      identity,
		Environment: environmentFacts{
			OS:           runtime.GOOS,
			Architecture: runtime.GOARCH,
			GoRuntime:    runtime.Version(),
		},
		Experiments:   []experimentReport{threshold, compaction},
		TotalWallMS:   time.Since(started).Milliseconds(),
		OverallPassed: threshold.Passed && compaction.Passed,
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fatal(err)
	}
	data = append(data, '\n')
	if *output != "" {
		if err := os.WriteFile(*output, data, 0o644); err != nil {
			fatal(err)
		}
	}
	if _, err := os.Stdout.Write(data); err != nil {
		fatal(err)
	}
	if !report.OverallPassed {
		os.Exit(1)
	}
}

func runThresholdExperiment(binary, root string) (experimentReport, error) {
	started := time.Now()
	commit, err := createFixture(binary, root, 30, 30)
	if err != nil {
		return experimentReport{}, err
	}
	report := experimentReport{
		ID:               "r67-a-threshold",
		Task:             "Update twenty existing components, add eleven components, and preserve repository cognition at stable checkpoints.",
		RepositoryCommit: commit,
		Threshold:        30,
	}
	report.IndexSHA256Before, err = fileSHA256(filepath.Join(root, "aoci.txt"))
	if err != nil {
		return report, err
	}
	client, err := startMCP(binary, root)
	if err != nil {
		return report, err
	}
	defer client.close()
	tools, err := client.listTools()
	if err != nil {
		return report, err
	}
	report.ToolsCount = tools
	if _, err := client.tool("aoci_rules", map[string]any{}); err != nil {
		return report, err
	}
	report.RulesReads++
	initialAttestation, err := recordAttestedOverview(client, root, map[string]any{}, 0, &report)
	if err != nil {
		return report, err
	}
	if !strings.Contains(initialAttestation, "challenge_passed: 10/10") ||
		!strings.Contains(initialAttestation, "model_full_cognition_reliable: true") {
		return report, fmt.Errorf("initial strict Attestation failed: %s", initialAttestation)
	}

	for number := 0; number < 20; number++ {
		if err := writeText(
			filepath.Join(root, "src", fmt.Sprintf("file-%03d.txt", number)),
			fmt.Sprintf("threshold task updated component %03d\n", number),
		); err != nil {
			return report, err
		}
	}
	for number := 0; number < 9; number++ {
		if err := writeText(
			filepath.Join(root, "src", fmt.Sprintf("new-%03d.txt", number)),
			fmt.Sprintf("threshold task new component %03d\n", number),
		); err != nil {
			return report, err
		}
	}
	at29, raw, err := client.assess(map[string]any{"check_only": true})
	if err != nil {
		return report, err
	}
	report.OverviewCompactReads++
	report.SemanticCounts = append(report.SemanticCounts, checkpoint("before-threshold", at29))
	if at29.Semantic.Count != 29 || at29.RefreshStatus != statusNotRequired || containsIndexBody(raw) {
		return report, fmt.Errorf("29-file contract failed: %s", raw)
	}
	if err := writeText(
		filepath.Join(root, "src", "new-009.txt"),
		"threshold task new component 009\n",
	); err != nil {
		return report, err
	}
	stable := true
	at30, raw, err := client.assess(map[string]any{
		"check_only":        true,
		"stable_checkpoint": stable,
	})
	if err != nil {
		return report, err
	}
	report.OverviewCompactReads++
	report.SemanticCounts = append(report.SemanticCounts, checkpoint("at-threshold", at30))
	report.TriggerReasons = uniqueSorted(append(report.TriggerReasons, at30.RefreshReasons...))
	if at30.Semantic.Count != 30 || at30.RefreshStatus != statusRequired || containsIndexBody(raw) {
		return report, fmt.Errorf("30-file contract failed: %s", raw)
	}
	dirtyText, err := client.tool("aoci_overview", map[string]any{})
	if err != nil {
		return report, err
	}
	if err := recordOverviewBody(dirtyText, &report); err != nil {
		return report, err
	}
	report.DirtyOverviewDelivered = strings.Contains(dirtyText, "cognition_currency: dirty_or_stale") &&
		strings.Contains(dirtyText, "governance_aligned: false") &&
		strings.Contains(dirtyText, "model_full_cognition_reliable: false")
	if !report.DirtyOverviewDelivered {
		return report, fmt.Errorf("dirty Overview was not delivered as unreliable: %s", dirtyText)
	}

	maintainMS, repairAttempts, err := maintainAndApply(client, "threshold")
	if err != nil {
		return report, err
	}
	report.MaintainCalls++
	report.RepairAttempts += repairAttempts
	report.DeterministicMS += maintainMS
	if err := collectAlignment(binary, root, &report); err != nil {
		return report, err
	}
	if _, err := recordAttestedOverview(client, root, map[string]any{}, 0, &report); err != nil {
		return report, err
	}
	if _, err := client.tool("aoci_get_entries", map[string]any{"paths": []string{"src/file-000.txt"}}); err != nil {
		return report, err
	}
	report.LocalRecallCalls++
	if err := recordOverview(client, map[string]any{}, &report); err != nil {
		return report, err
	}
	report.RepeatedBodyDelivered = true
	report.IndexSHA256After, err = fileSHA256(filepath.Join(root, "aoci.txt"))
	if err != nil {
		return report, err
	}
	report.Ledger, err = inspectLedger(root, report)
	if err != nil {
		return report, err
	}
	report.WallMS = time.Since(started).Milliseconds()
	report.Passed = report.ToolsCount == 9 && report.RulesReads == 1 &&
		report.OverviewFullReads == 4 && report.MaintainCalls == 1 &&
		report.RepairAttempts == 1 &&
		report.DirtyOverviewDelivered && report.RepeatedBodyDelivered &&
		report.Verify.ExitCode == 0 && report.Check.ExitCode == 0 && report.Guide.ExitCode == 0 &&
		report.Ledger.Consistent
	return report, nil
}

func runCompactionExperiment(binary, root string) (experimentReport, error) {
	started := time.Now()
	commit, err := createFixture(binary, root, 12, 1)
	if err != nil {
		return experimentReport{}, err
	}
	report := experimentReport{
		ID:                  "r67-a-compaction-recovery",
		Task:                "Simulate host compaction, merge it with a major phase transition and semantic threshold, then recover at one stable checkpoint.",
		RepositoryCommit:    commit,
		Threshold:           1,
		HostEventSimulation: true,
	}
	report.IndexSHA256Before, err = fileSHA256(filepath.Join(root, "aoci.txt"))
	if err != nil {
		return report, err
	}
	client, err := startMCP(binary, root)
	if err != nil {
		return report, err
	}
	defer client.close()
	report.ToolsCount, err = client.listTools()
	if err != nil {
		return report, err
	}
	if _, err := client.tool("aoci_rules", map[string]any{}); err != nil {
		return report, err
	}
	report.RulesReads++
	initialAttestation, err := recordAttestedOverview(client, root, map[string]any{}, 0, &report)
	if err != nil {
		return report, err
	}
	if !strings.Contains(initialAttestation, "challenge_passed: 10/10") ||
		!strings.Contains(initialAttestation, "model_full_cognition_reliable: true") {
		return report, fmt.Errorf("initial strict Attestation failed: %s", initialAttestation)
	}
	cleanEvent := map[string]any{
		"refresh_reasons":  []string{"context_compaction"},
		"refresh_event_id": "simulated-clean-compaction-1",
	}
	partialAttestation, err := recordAttestedOverview(client, root, cleanEvent, 3, &report)
	if err != nil {
		return report, err
	}
	report.AttestationPassed = "7/10"
	report.FullSystemClaimAllowed = false
	if !strings.Contains(partialAttestation, "challenge_passed: 7/10") ||
		!strings.Contains(partialAttestation, "model_full_cognition_reliable: false") ||
		!strings.Contains(partialAttestation, "full_system_claim_disabled_source_bound_task_continuation_allowed") {
		return report, fmt.Errorf("partial refresh Attestation contract failed: %s", partialAttestation)
	}
	afterPartial, raw, err := client.assess(map[string]any{
		"check_only":       true,
		"refresh_reasons":  []string{"context_compaction"},
		"refresh_event_id": "simulated-clean-compaction-1",
	})
	if err != nil {
		return report, err
	}
	report.OverviewCompactReads++
	report.RefreshLoopPrevented = afterPartial.RefreshStatus == statusNotRequired &&
		afterPartial.State == "uncertain" && !containsIndexBody(raw)
	if !report.RefreshLoopPrevented {
		return report, fmt.Errorf("partial Attestation retriggered refresh: %s", raw)
	}
	// A simulated non-control user question is answered by preserving the task
	// identity; the next source-bound read proves execution continued.
	report.QuestionAnswered = true
	if _, err := client.tool("aoci_get_entries", map[string]any{"paths": []string{"src/file-000.txt"}}); err != nil {
		return report, err
	}
	report.LocalRecallCalls++
	report.SourceBoundContinued = true
	phaseOnly, raw, err := client.assess(map[string]any{
		"check_only":       true,
		"refresh_reasons":  []string{"phase_transition"},
		"refresh_event_id": "simulated-clean-phase-1",
	})
	if err != nil {
		return report, err
	}
	report.OverviewCompactReads++
	if phaseOnly.RefreshStatus != statusNotRequired || containsIndexBody(raw) {
		return report, fmt.Errorf("clean phase transition resent Overview: %s", raw)
	}

	if err := writeText(filepath.Join(root, "src", "file-000.txt"), "compaction task semantic update\n"); err != nil {
		return report, err
	}
	mergedInput := map[string]any{
		"check_only":       true,
		"refresh_reasons":  []string{"context_compaction", "phase_transition"},
		"refresh_event_id": "simulated-dirty-merged-1",
	}
	deferred, raw, err := client.assess(mergedInput)
	if err != nil {
		return report, err
	}
	report.OverviewCompactReads++
	report.SemanticCounts = append(report.SemanticCounts, checkpoint("dirty-unstable", deferred))
	if deferred.RefreshStatus != statusDeferred || len(deferred.RefreshReasons) != 3 || containsIndexBody(raw) {
		return report, fmt.Errorf("dirty compaction was not deferred and merged: %s", raw)
	}
	mergedInput["stable_checkpoint"] = true
	required, raw, err := client.assess(mergedInput)
	if err != nil {
		return report, err
	}
	report.OverviewCompactReads++
	report.SemanticCounts = append(report.SemanticCounts, checkpoint("dirty-stable", required))
	report.TriggerReasons = uniqueSorted(append(report.TriggerReasons, required.RefreshReasons...))
	if required.RefreshStatus != statusRequired || containsIndexBody(raw) {
		return report, fmt.Errorf("dirty stable event bypassed Maintain: %s", raw)
	}
	maintainMS, repairAttempts, err := maintainAndApply(client, "compaction")
	if err != nil {
		return report, err
	}
	report.MaintainCalls++
	report.RepairAttempts += repairAttempts
	report.DeterministicMS += maintainMS
	if err := collectAlignment(binary, root, &report); err != nil {
		return report, err
	}
	delete(mergedInput, "check_only")
	if _, err := recordAttestedOverview(client, root, mergedInput, 0, &report); err != nil {
		return report, err
	}
	report.IndexSHA256After, err = fileSHA256(filepath.Join(root, "aoci.txt"))
	if err != nil {
		return report, err
	}
	report.Ledger, err = inspectLedger(root, report)
	if err != nil {
		return report, err
	}
	report.WallMS = time.Since(started).Milliseconds()
	report.Passed = report.ToolsCount == 9 && report.RulesReads == 1 &&
		report.OverviewFullReads == 3 && report.MaintainCalls == 1 &&
		len(report.TriggerReasons) == 3 && report.RefreshLoopPrevented &&
		report.AttestationPassed == "7/10" && !report.FullSystemClaimAllowed &&
		report.SourceBoundContinued && report.QuestionAnswered && report.RepairAttempts == 1 &&
		report.Verify.ExitCode == 0 &&
		report.Check.ExitCode == 0 && report.Guide.ExitCode == 0 && report.Ledger.Consistent
	return report, nil
}

func createFixture(binary, root string, files, threshold int) (string, error) {
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(root, ".aoci"), 0o755); err != nil {
		return "", err
	}
	for number := 0; number < files; number++ {
		if err := writeText(
			filepath.Join(root, "src", fmt.Sprintf("file-%03d.txt", number)),
			fmt.Sprintf("baseline fixture component %03d\n", number),
		); err != nil {
			return "", err
		}
	}
	configBody := fmt.Sprintf("{\n  \"version\": 2,\n  \"locale\": \"en-US\",\n  \"index_path\": \"aoci.txt\",\n  \"cognition_refresh_threshold\": %d\n}\n", threshold)
	if err := writeText(filepath.Join(root, ".aoci", "config.json"), configBody); err != nil {
		return "", err
	}
	var index strings.Builder
	index.WriteString("#====R67 Long-Run Fixture Complete Index====\n")
	index.WriteString("#Locale: en-US\n#===Header Index===\n")
	index.WriteString("#[Deployment] Static text fixture exercised by an isolated black-box runner\n")
	index.WriteString("#[System] Synthetic repository for cognition refresh ordering and idempotency\n")
	index.WriteString("#[Overall Rules] Entries describe independent text fixture components\n")
	index.WriteString("#[Correct Index Examples]\n")
	index.WriteString("#file-000.txt[X.Y.5.T]: F:fixture component | R:- | A:- | S:Each file is independent test evidence\n")
	index.WriteString("#===Index Rules===\n#[Format] filename[tag]: F:function | R:relationships | A:interface | S:information\n")
	index.WriteString("#A Layer: X-Fixture\n#B Module: Y-Refresh\n#C Importance: 5-routine\n#D Trait (optional): Z-test\n#E Scale: T-tiny<100\n")
	index.WriteString("#S quota: C9-8≤600 C7-4≤200 C3-1≤50\n#===Index Rules Complete===\n#===Header Index Complete===\n\n#Code Index\n")
	index.WriteString("===Repository " + filepath.ToSlash(root) + "/===\n")
	index.WriteString("aoci.txt[X.Y.5.T]: F:fixture cognition | R:src | A:- | S:Maintained only through the candidate governance path\n")
	index.WriteString("===Sources " + filepath.ToSlash(filepath.Join(root, "src")) + "/===\n")
	for number := 0; number < files; number++ {
		fmt.Fprintf(&index, "file-%03d.txt[X.Y.5.T]: F:fixture component | R:- | A:- | S:Independent baseline component %03d\n", number, number)
	}
	index.WriteString("#Code Index Complete\n")
	if err := writeText(filepath.Join(root, "aoci.txt"), index.String()); err != nil {
		return "", err
	}
	if evidence, err := runCommand(binary, "--repo", root, "--quiet", "scan"); err != nil {
		return "", fmt.Errorf("initial scan: %w: %s", err, evidence.Output)
	}
	commands := [][]string{
		{"git", "init", "-q"},
		{"git", "config", "user.name", "AOCI Blackbox"},
		{"git", "config", "user.email", "blackbox@example.invalid"},
		{"git", "add", "aoci.txt", "src", ".aoci/config.json", ".aoci/baseline.json"},
	}
	for _, args := range commands {
		if evidence, err := runCommandAt(root, args[0], args[1:]...); err != nil {
			return "", fmt.Errorf("%s: %w: %s", strings.Join(args, " "), err, evidence.Output)
		}
	}
	commit := exec.Command("git", "commit", "-q", "-m", "fixture: freeze long-run subject")
	commit.Dir = root
	commit.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z",
		"GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
	)
	if output, err := commit.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git commit: %w: %s", err, output)
	}
	evidence, err := runCommandAt(root, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(evidence.Output), nil
}

func maintainAndApply(client *rpcClient, label string) (int64, int, error) {
	text, err := client.tool("aoci_maintain", map[string]any{})
	if err != nil {
		return 0, 0, err
	}
	var result autoResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return 0, 0, fmt.Errorf("decode Maintain result: %w: %s", err, text)
	}
	if result.Status != "repair_required" || len(result.Candidates) == 0 {
		return result.Metrics.DeterministicMS, 0, fmt.Errorf("maintain did not return semantic candidates: %s", text)
	}
	entries := make([]map[string]any, 0, len(result.Candidates))
	for _, candidate := range result.Candidates {
		base := filepath.Base(candidate.Path)
		entries = append(entries, map[string]any{
			"path":          candidate.Path,
			"source_sha256": candidate.SourceSHA256,
			"new_entry": fmt.Sprintf(
				"%s[X.Y.5.T]: F:updated fixture | R:- | A:- | S:Semantic evidence produced by the %s black-box task",
				base,
				label,
			),
		})
	}
	broken := append([]map[string]any{}, entries...)
	broken[0] = map[string]any{}
	for key, value := range entries[0] {
		broken[0][key] = value
	}
	broken[0]["new_entry"] = "deliberately invalid test-only candidate"
	text, err = client.tool("aoci_update_entry", map[string]any{"entries": broken})
	if err != nil {
		return result.Metrics.DeterministicMS, 0, err
	}
	var repair autoResult
	if err := json.Unmarshal([]byte(text), &repair); err != nil {
		return result.Metrics.DeterministicMS, 0, fmt.Errorf("decode repair result: %w: %s", err, text)
	}
	if repair.Status != "repair_required" || len(repair.Findings) == 0 {
		return result.Metrics.DeterministicMS, 0, fmt.Errorf("invalid candidate did not request bounded repair: %s", text)
	}
	text, err = client.tool("aoci_update_entry", map[string]any{"entries": entries})
	if err != nil {
		return result.Metrics.DeterministicMS + repair.Metrics.DeterministicMS, 1, err
	}
	var applied autoResult
	if err := json.Unmarshal([]byte(text), &applied); err != nil {
		return result.Metrics.DeterministicMS + repair.Metrics.DeterministicMS, 1, fmt.Errorf("decode Update result: %w: %s", err, text)
	}
	if applied.Status != "applied" || !applied.Aligned {
		return result.Metrics.DeterministicMS + repair.Metrics.DeterministicMS + applied.Metrics.DeterministicMS, 1,
			fmt.Errorf("update did not align repository: %s", text)
	}
	return result.Metrics.DeterministicMS + repair.Metrics.DeterministicMS + applied.Metrics.DeterministicMS, 1, nil
}

func collectAlignment(binary, root string, report *experimentReport) error {
	var err error
	report.Verify, err = runCommand(binary, "--repo", root, "--json", "verify")
	if err != nil {
		return fmt.Errorf("verify: %w: %s", err, report.Verify.Output)
	}
	report.Check, err = runCommand(binary, "--repo", root, "--json", "check")
	if err != nil {
		return fmt.Errorf("check: %w: %s", err, report.Check.Output)
	}
	report.Guide, err = runCommand(
		binary,
		"--repo", root,
		"--json",
		"index", "agent", "guide",
		"--agent", "r67-blackbox",
	)
	if err != nil {
		return fmt.Errorf("guide: %w: %s", err, report.Guide.Output)
	}
	return nil
}

func inspectLedger(root string, report experimentReport) (ledgerEvidence, error) {
	file, err := os.Open(filepath.Join(root, ".aoci", "ledger.jsonl"))
	if err != nil {
		return ledgerEvidence{}, err
	}
	defer file.Close()
	evidence := ledgerEvidence{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var event struct {
			Op            string `json:"op"`
			OverviewReads int    `json:"overview_reads"`
			SemanticFiles int    `json:"semantic_files"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return evidence, fmt.Errorf("decode Ledger event: %w", err)
		}
		evidence.Events++
		switch event.Op {
		case "rules":
			evidence.RulesReads++
		case "overview":
			evidence.OverviewFullReads += event.OverviewReads
		case "cognition_check":
			evidence.CognitionChecks++
		case "maintain":
			evidence.MaintainCalls++
		case "get_entries":
			evidence.LocalRecallCalls++
		}
		if event.SemanticFiles > evidence.SemanticPeak {
			evidence.SemanticPeak = event.SemanticFiles
		}
	}
	if err := scanner.Err(); err != nil {
		return evidence, err
	}
	wantPeak := 0
	for _, current := range report.SemanticCounts {
		if current.Count > wantPeak {
			wantPeak = current.Count
		}
	}
	evidence.Consistent = evidence.RulesReads == report.RulesReads &&
		evidence.OverviewFullReads == report.OverviewFullReads &&
		evidence.CognitionChecks == report.OverviewCompactReads &&
		evidence.MaintainCalls == report.MaintainCalls &&
		evidence.LocalRecallCalls == report.LocalRecallCalls &&
		evidence.SemanticPeak == wantPeak
	return evidence, nil
}

func startMCP(binary, root string) (*rpcClient, error) {
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, binary, "--repo", root, "mcp")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	client := &rpcClient{cmd: cmd, stdin: stdin, nextID: 1}
	client.scanner = bufio.NewScanner(stdout)
	client.scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	cmd.Stderr = &client.stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	if _, err := client.call("initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name": "aoci-r67-longrun-blackbox", "version": "1",
		},
	}); err != nil {
		client.close()
		return nil, err
	}
	if err := client.notify("notifications/initialized", map[string]any{}); err != nil {
		client.close()
		return nil, err
	}
	return client, nil
}

func (client *rpcClient) listTools() (int, error) {
	raw, err := client.call("tools/list", map[string]any{})
	if err != nil {
		return 0, err
	}
	var result struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return 0, err
	}
	return len(result.Tools), nil
}

func (client *rpcClient) tool(name string, arguments map[string]any) (string, error) {
	raw, err := client.call("tools/call", map[string]any{"name": name, "arguments": arguments})
	if err != nil {
		return "", err
	}
	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", err
	}
	if len(result.Content) == 0 {
		return "", errors.New("tool returned no content")
	}
	if result.IsError {
		return "", fmt.Errorf("tool %s failed: %s", name, result.Content[0].Text)
	}
	return result.Content[0].Text, nil
}

func (client *rpcClient) assess(arguments map[string]any) (assessment, string, error) {
	text, err := client.tool("aoci_overview", arguments)
	if err != nil {
		return assessment{}, "", err
	}
	var result struct {
		Assessment assessment `json:"assessment"`
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return assessment{}, text, fmt.Errorf("decode compact assessment: %w", err)
	}
	return result.Assessment, text, nil
}

func (client *rpcClient) call(method string, params any) (json.RawMessage, error) {
	id := client.nextID
	client.nextID++
	request := map[string]any{
		"jsonrpc": "2.0", "id": id, "method": method, "params": params,
	}
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	if _, err := client.stdin.Write(append(data, '\n')); err != nil {
		return nil, err
	}
	for client.scanner.Scan() {
		line := append([]byte{}, client.scanner.Bytes()...)
		var envelope struct {
			ID     json.RawMessage `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(line, &envelope); err != nil {
			return nil, fmt.Errorf("decode JSON-RPC response: %w: %s", err, line)
		}
		if string(envelope.ID) != strconv.Itoa(id) {
			continue
		}
		if envelope.Error != nil {
			return nil, fmt.Errorf("JSON-RPC %s failed (%d): %s", method, envelope.Error.Code, envelope.Error.Message)
		}
		return envelope.Result, nil
	}
	if err := client.scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("MCP closed during %s: %s", method, client.stderr.String())
}

func (client *rpcClient) notify(method string, params any) error {
	data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		return err
	}
	_, err = client.stdin.Write(append(data, '\n'))
	return err
}

func (client *rpcClient) close() {
	if client.stdin != nil {
		_ = client.stdin.Close()
	}
	if client.cmd != nil && client.cmd.Process != nil {
		_ = client.cmd.Process.Kill()
		_ = client.cmd.Wait()
	}
}

func recordOverview(client *rpcClient, arguments map[string]any, report *experimentReport) error {
	text, err := client.tool("aoci_overview", arguments)
	if err != nil {
		return err
	}
	return recordOverviewBody(text, report)
}

func recordAttestedOverview(
	client *rpcClient,
	root string,
	arguments map[string]any,
	wrongAnswers int,
	report *experimentReport,
) (string, error) {
	body, err := client.tool("aoci_overview", arguments)
	if err != nil {
		return "", err
	}
	if err := recordOverviewBody(body, report); err != nil {
		return "", err
	}
	attestationArguments, err := buildAttestationArguments(root, body, wrongAnswers)
	if err != nil {
		return "", err
	}
	for key, value := range arguments {
		attestationArguments[key] = value
	}
	return client.tool("aoci_overview", attestationArguments)
}

// buildAttestationArguments derives every semantic answer from the delivered
// body. The deliberate wrong-answer count is a test fixture; no source,
// Memory, Search, or Entry lookup participates in Attestation.
func buildAttestationArguments(root, delivered string, wrongAnswers int) (map[string]any, error) {
	const start = "<<<AOCI_OVERVIEW_BODY_BEGIN/v1 scope=repository_full>>>\n"
	const end = "<<<AOCI_OVERVIEW_BODY_END/v1 scope=repository_full>>>"
	startOffset := strings.Index(delivered, start)
	if startOffset < 0 {
		return nil, errors.New("overview start marker missing")
	}
	contentStart := startOffset + len(start)
	endOffset := strings.Index(delivered[contentStart:], end)
	if endOffset < 0 {
		return nil, errors.New("overview end marker missing")
	}
	content := delivered[contentStart : contentStart+endOffset]
	document, warnings := index.Parse(content)
	if len(warnings) != 0 {
		return nil, fmt.Errorf("delivered index warnings: %+v", warnings)
	}
	index.ResolveRelPaths(document, root)
	entries := make([]*index.Entry, 0)
	for _, section := range document.Sections {
		if section.AbsPath != "" {
			entries = append(entries, section.Entries...)
		}
	}
	ordinalText, err := metadataValue(delivered, "challenge_ordinals")
	if err != nil {
		return nil, err
	}
	ordinalParts := strings.Split(ordinalText, ",")
	if wrongAnswers < 0 || wrongAnswers > len(ordinalParts) {
		return nil, fmt.Errorf("invalid wrong-answer count %d for %d challenges", wrongAnswers, len(ordinalParts))
	}
	answers := make([]map[string]any, 0, len(ordinalParts))
	for position, raw := range ordinalParts {
		ordinal, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || ordinal < 1 || ordinal > len(entries) {
			return nil, fmt.Errorf("invalid delivered challenge ordinal %q", raw)
		}
		entry := entries[ordinal-1]
		identity := entry.RelPath
		if position < wrongAnswers {
			identity = "deliberately-wrong-test-object"
		}
		answers = append(answers, map[string]any{
			"ordinal": ordinal, "object_identity": identity,
			"tag": entry.TagsRaw, "core_f": entry.F,
		})
	}
	bodyBytes, err := metadataInt(delivered, "body_utf8_bytes")
	if err != nil {
		return nil, err
	}
	entryCount, err := metadataInt(delivered, "entry_count")
	if err != nil {
		return nil, err
	}
	tokens, err := metadataInt(delivered, "estimated_tokens")
	if err != nil {
		return nil, err
	}
	bodySHA, err := metadataValue(delivered, "body_sha256")
	if err != nil {
		return nil, err
	}
	challengeDigest, err := metadataValue(delivered, "challenge_digest")
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"host_delivery_confirmation": map[string]any{
			"version": "overview-delivery-receipt/v1", "body_sha256": bodySHA,
			"body_bytes": bodyBytes, "end_marker_observed": true,
		},
		"model_cognition_attestation": map[string]any{
			"version": "model-cognition-attestation/v1", "challenge_digest": challengeDigest,
			"reported_entry_count": entryCount, "reported_estimated_tokens": tokens,
			"coverage_percent": 100, "system_mastery_percent": 95,
			"confidence_percent": 95, "truncation_detected": false,
			"unseen_sections": []string{}, "uncertainty_reasons": []string{},
			"challenge_answers": answers,
		},
	}, nil
}

func recordOverviewBody(text string, report *experimentReport) error {
	if !containsIndexBody(text) || !strings.Contains(text, "full_text_included: true") {
		return fmt.Errorf("expected complete Overview, received: %s", text)
	}
	report.OverviewFullReads++
	bytesValue, err := metadataInt(text, "index_bytes")
	if err != nil {
		return err
	}
	tokens, err := metadataInt(text, "estimated_tokens")
	if err != nil {
		return err
	}
	report.IndexBodyBytes += bytesValue
	report.EstimatedTokens += tokens
	return nil
}

func metadataInt(text, key string) (int, error) {
	value, err := metadataValue(text, key)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(value)
}

func metadataValue(text, key string) (string, error) {
	prefix := key + ": "
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix)), nil
		}
	}
	return "", fmt.Errorf("overview metadata missing %s", key)
}

func containsIndexBody(text string) bool {
	return strings.Contains(text, "#====R67 Long-Run Fixture Complete Index====")
}

func checkpoint(label string, value assessment) semanticCheckpoint {
	return semanticCheckpoint{
		Label: label, Count: value.Semantic.Count, Status: value.RefreshStatus,
		Reasons: append([]string{}, value.RefreshReasons...),
	}
}

func identifyBinary(path string) (artifactIdentity, error) {
	digest, err := fileSHA256(path)
	if err != nil {
		return artifactIdentity{}, err
	}
	evidence, err := runCommand(path, "--version")
	if err != nil {
		return artifactIdentity{}, err
	}
	return artifactIdentity{Path: path, SHA256: digest, Version: strings.TrimSpace(evidence.Output)}, nil
}

func runCommand(name string, args ...string) (commandEvidence, error) {
	return runCommandAt("", name, args...)
}

func runCommandAt(dir, name string, args ...string) (commandEvidence, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	digest := sha256.Sum256(output)
	evidence := commandEvidence{
		ExitCode: exitCode,
		SHA256:   hex.EncodeToString(digest[:]),
		Output:   strings.TrimSpace(string(output)),
	}
	return evidence, err
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func writeText(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func uniqueSorted(values []string) []string {
	set := map[string]bool{}
	for _, value := range values {
		set[value] = true
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(2)
}
