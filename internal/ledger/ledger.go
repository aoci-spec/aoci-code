// Telemetry event JSONL appending / recent event summary / rough token estimate / endpoint masking hash.
//
// Discipline:
//   - O_APPEND single-line writes, one Write per event, so this process never splits a line;
//   - a write failure only warns on stderr and never blocks the main flow;
//   - on read, a corrupt line is skipped and counted, never crashing the whole;
//   - EstimateTokens is a rough estimate for trends only, and outward wording must not present it as exact billing;
//   - timestamps use UTC RFC3339Nano, so events in the same second do not lose their direct ordering evidence;
//   - keys, source bodies, Prompt bodies, and conversation bodies never enter the Ledger.
//
// Ledger versions:
//   - v1: basic operations, path counts, durations, drift, and source;
//   - v2: model, endpoint masking, Token, draft, and Warning metering;
//   - v3: result/fail_code and batch Apply or refusal metering;
//   - v4: schema_version, event_id, experiment context, business exit code, and Overview delivery observation fields;
//   - v5: Entries Check records passed, warned, rejected, and skipped separately and adds the
//     repair_required result, so a candidate refusal is not misrecorded as an ordinary Warning success.
//   - v6: the Auto path adds tool round trips, cognition recall, semantic files, format-only,
//     and duplicate-protection metering, so the low-overhead goal can be recomputed from the Ledger.
//   - v7: distinguishes this call's real writes from the recovery count confirmed already at the target state on crash/replay.
//   - v8: Entries post-write recovery records old pre/postimages, current
//     Baseline, recovery transaction, and later governance proof chain.
//
// The v5 experiment context:
//   - the CLI cannot reliably probe the Codex Desktop model, the Memory switch, or the UI session ID;
//   - an experiment Harness may declare them explicitly through environment variables such as AOCI_EXPERIMENT_ID;
//   - Append fills the environment declaration only when the Event's field is empty, never overwriting the caller's facts;
//   - an environment value has its surrounding whitespace stripped, newlines and runs of whitespace folded, and is capped at 256 runes;
//   - when an ordinary user sets no experiment variable, these fields stay omitted.
//
// result and exit_code:
//   - result says whether the operation completed its own verdict or write transaction normally;
//   - repair_required says the verdict completed normally but the candidates must be corrected before continuing;
//   - exit_code is the CLI business exit code the caller formed at the same verdict point;
//   - so a Verify that normally finds drift can be result=ok and exit_code=1 at once;
//   - exit_code is a pointer, telling a historical event without a record from an explicit success code 0.
//
// Dependency direction:
// this package belongs to the deterministic core layer and never imports internal/llm.
package ledger

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
)

// EventSchemaVersion is the machine protocol version of newly written Ledger events.
// 9 adds the governance outcome fields (#150).
const EventSchemaVersion = 9

// The Source enumeration: the originator of the event.
const (
	SourceHuman  = "human"
	SourceAgent  = "agent"
	SourceCLIAI  = "cli_ai"
	SourceCI     = "ci"
	SourcePolicy = "policy"
)

// The TokenSource enumeration: the source of token metering.
const (
	TokenSourceExact     = "exact"
	TokenSourceReported  = "reported"
	TokenSourceEstimated = "estimated"
	TokenSourceMissing   = "missing"
)

// The Result enumeration: the terminal state of the operation.
// An old event may leave Result empty and reads as success; a new event writes ok explicitly by default.
const (
	ResultOK             = "ok"
	ResultRepairRequired = machinecontract.AutoStatusRepairRequired
	ResultRejected       = "rejected"
	ResultConflict       = "conflict"
	ResultError          = "error"
)

// Event is one governance, experiment, or metering event.
//
// Every extension field uses omitempty, so historical JSONL lines need no migration.
// ExitCode and FullTextIncluded are pointers, telling "not recorded historically / explicitly 0"
// and "not applicable / explicitly false" apart.
type Event struct {
	SchemaVersion int    `json:"schema_version,omitempty"`
	EventID       string `json:"event_id,omitempty"`
	Ts            string `json:"ts"`
	Op            string `json:"op"`

	PathsCount  int    `json:"paths_count,omitempty"`
	TagFilter   string `json:"tag_filter,omitempty"`
	DurationMs  int64  `json:"duration_ms,omitempty"`
	DriftWarned bool   `json:"drift_warned,omitempty"`
	Source      string `json:"source,omitempty"`
	Confidence  string `json:"confidence,omitempty"`

	GenerationSource string `json:"generation_source,omitempty"`
	AgentName        string `json:"agent_name,omitempty"`

	Result   string `json:"result,omitempty"`
	FailCode string `json:"fail_code,omitempty"`
	ExitCode *int   `json:"exit_code,omitempty"`

	Model         string  `json:"model,omitempty"`
	Provider      string  `json:"provider,omitempty"`
	EndpointHash  string  `json:"endpoint_hash,omitempty"`
	InputTokens   int     `json:"input_tokens,omitempty"`
	OutputTokens  int     `json:"output_tokens,omitempty"`
	TokenSrc      string  `json:"token_source,omitempty"`
	CostEstimate  float64 `json:"cost_estimate,omitempty"`
	DraftRunID    string  `json:"draft_run_id,omitempty"`
	WarningsCount int     `json:"warnings_count,omitempty"`

	PassedCount  int `json:"passed_count,omitempty"`
	WarnedCount  int `json:"warned_count,omitempty"`
	SkippedCount int `json:"skipped_count,omitempty"`

	AppliedCount   int    `json:"applied_count,omitempty"`
	RecoveredCount int    `json:"recovered_count,omitempty"`
	RejectedCount  int    `json:"rejected_count,omitempty"`
	RejectKinds    string `json:"reject_kinds,omitempty"`

	// The experiment context is filled explicitly by the caller, or supplemented by Append from the controlled environment variables.
	ExperimentID    string `json:"experiment_id,omitempty"`
	TaskID          string `json:"task_id,omitempty"`
	ExperimentGroup string `json:"experiment_group,omitempty"`
	RunID           string `json:"run_id,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
	AgentModel      string `json:"agent_model,omitempty"`
	MemoryMode      string `json:"memory_mode,omitempty"`
	RepositoryHead  string `json:"repository_head,omitempty"`
	BinaryVersion   string `json:"binary_version,omitempty"`

	// Research observation fields usable by Overview or other cognition delivery operations.
	DeliveryMode     string `json:"delivery_mode,omitempty"`
	FullTextIncluded *bool  `json:"full_text_included,omitempty"`
	FallbackReason   string `json:"fallback_reason,omitempty"`
	IndexSHA256      string `json:"index_sha256,omitempty"`
	RepositorySHA256 string `json:"repository_sha256,omitempty"`
	IndexBytes       int    `json:"index_bytes,omitempty"`
	OutputBytes      int    `json:"output_bytes,omitempty"`
	EstimatedTokens  int    `json:"estimated_tokens,omitempty"`
	SectionCount     int    `json:"section_count,omitempty"`
	EntryCount       int    `json:"entry_count,omitempty"`

	// Overview delivery chains (#114): a fresh complete delivery mints one
	// chain ID that its continuations and proof calls inherit, so an auditor
	// joins a chain by ID instead of by time adjacency. Each delivered
	// response records its chunk position and whether it completed the body;
	// an attestation call records the Host confirmation it carried.
	DeliveryChainID    string   `json:"delivery_chain_id,omitempty"`
	ChunkIndex         int      `json:"chunk_index,omitempty"`
	ChunkCount         int      `json:"chunk_count,omitempty"`
	DeliveryCompleted  *bool    `json:"delivery_completed,omitempty"`
	RefreshReasons     []string `json:"refresh_reasons,omitempty"`
	RefreshEventID     string   `json:"refresh_event_id,omitempty"`
	HostDeliveryStatus string   `json:"host_delivery_status,omitempty"`

	// The low-overhead and duplicate-protection facts of the Auto path.
	AOCIToolCalls     int `json:"aoci_tool_calls,omitempty"`
	ShellAOCICalls    int `json:"shell_aoci_calls,omitempty"`
	OverviewReads     int `json:"overview_reads,omitempty"`
	LocalRecalls      int `json:"local_recalls,omitempty"`
	SemanticFiles     int `json:"semantic_files,omitempty"`
	FormatOnlyFiles   int `json:"format_only_files,omitempty"`
	DuplicateApplies  int `json:"duplicate_applies,omitempty"`
	RepeatedMaintains int `json:"repeated_maintains,omitempty"`

	// Governance outcome of a governed call (#150). result keeps its coarse
	// meaning; these say which machine verdict the call reached, the finding
	// that stopped it, the next action it named, whether formal writes began,
	// and the batch it bound. For check and verify, result is that the
	// command ran and check_passed is the verdict, so a failing check no
	// longer reads as ok with exit_code=1. Machine tokens only: never source,
	// Entry text, or connection details.
	GovernanceResult    string `json:"governance_result,omitempty"`
	Blocker             string `json:"blocker,omitempty"`
	NextAction          string `json:"next_action,omitempty"`
	FormalWritesStarted *bool  `json:"formal_writes_started,omitempty"`
	BatchID             string `json:"batch_id,omitempty"`
	CheckPassed         *bool  `json:"check_passed,omitempty"`

	// Entries post-write recovery and supersession proof.
	RecoveryStatus        string   `json:"recovery_status,omitempty"`
	RecoveryTransactionID string   `json:"recovery_transaction_id,omitempty"`
	PreIndexSHA256        string   `json:"pre_index_sha256,omitempty"`
	PostIndexSHA256       string   `json:"post_index_sha256,omitempty"`
	BaselineSHA256        string   `json:"baseline_sha256,omitempty"`
	GovernanceReceipts    []string `json:"governance_receipts,omitempty"`
}

func ledgerPath(root string) string {
	return filepath.Join(root, ".aoci", "ledger.jsonl")
}

// Append appends one event.
// With enabled=false it skips silently; any failure only warns on stderr and never blocks the main flow.
func Append(root string, enabled bool, ev Event) {
	if !enabled {
		return
	}

	enrichEvent(&ev)

	line, err := json.Marshal(ev)
	if err != nil {
		writeDiagnostic("ledger.marshal_failed", err)
		return
	}

	dir := filepath.Dir(ledgerPath(root))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeDiagnostic("ledger.mkdir_failed", err)
		return
	}

	file, err := os.OpenFile(
		ledgerPath(root),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0o644,
	)
	if err != nil {
		writeDiagnostic("ledger.open_failed", err)
		return
	}
	defer file.Close()

	// One event lands through one Write, so this process never splits a JSONL line into several writes on its own.
	if _, err := file.Write(append(line, '\n')); err != nil {
		writeDiagnostic("ledger.write_failed", err)
	}
}

// writeDiagnostic keeps Ledger best-effort while ensuring its user-visible
// stderr shell comes from the active Locale. A broken catalog suppresses this
// non-authoritative diagnostic instead of changing the caller's write result.
func writeDiagnostic(key string, err error) {
	detail := err.Error()
	hasHan := strings.ContainsFunc(detail, func(character rune) bool {
		return unicode.Is(unicode.Han, character)
	})
	hasASCII := strings.ContainsFunc(detail, func(character rune) bool {
		return (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z')
	})
	if (textassets.ActiveLocale() == textassets.DefaultLocale && hasHan) ||
		(textassets.ActiveLocale() == textassets.LegacyLocale && !hasHan && hasASCII) {
		key := "ledger.localized_detail_unavailable"
		arguments := []any(nil)
		if facts := textassets.DiagnosticFacts(detail); facts != "" {
			key = "ledger.localized_detail_with_facts"
			arguments = []any{facts}
		}
		localized, localizedErr := textassets.Message(textassets.ActiveLocale(), key, arguments...)
		if localizedErr != nil {
			return
		}
		detail = localized
	}
	message, messageErr := textassets.Message(textassets.ActiveLocale(), key, detail)
	if messageErr == nil {
		fmt.Fprintln(os.Stderr, message)
	}
}

// enrichEvent fills the basic observation fields every new event should carry.
func enrichEvent(ev *Event) {
	if ev.SchemaVersion == 0 {
		ev.SchemaVersion = EventSchemaVersion
	}
	if ev.EventID == "" {
		ev.EventID = newEventID()
	}

	ev.Ts = time.Now().UTC().Format(time.RFC3339Nano)

	if ev.Result == "" {
		ev.Result = ResultOK
	}

	fillContextField(&ev.ExperimentID, "AOCI_EXPERIMENT_ID")
	fillContextField(&ev.TaskID, "AOCI_TASK_ID")
	fillContextField(&ev.ExperimentGroup, "AOCI_EXPERIMENT_GROUP")
	fillContextField(&ev.RunID, "AOCI_RUN_ID")
	fillContextField(&ev.SessionID, "AOCI_SESSION_ID")
	fillContextField(&ev.AgentModel, "AOCI_AGENT_MODEL")
	fillContextField(&ev.MemoryMode, "AOCI_MEMORY_MODE")
	fillContextField(&ev.RepositoryHead, "AOCI_REPOSITORY_HEAD")
	fillContextField(&ev.BinaryVersion, "AOCI_BINARY_VERSION")
}

// fillContextField reads the experiment environment declaration only when the caller gave no fact.
func fillContextField(target *string, environmentName string) {
	if *target != "" {
		return
	}

	*target = normalizeContextValue(os.Getenv(environmentName))
}

// normalizeContextValue keeps an experiment label from carrying newlines or overlong text into the JSONL.
func normalizeContextValue(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return ""
	}

	const maximumRunes = 256

	runes := []rune(value)
	if len(runes) > maximumRunes {
		runes = runes[:maximumRunes]
	}

	return string(runes)
}

// newEventID generates a random event identifier holding no repository content.
// When the system random source fails, a digest of time and PID serves as the fallback that does not block the main flow.
func newEventID() string {
	randomBytes := make([]byte, 12)
	if _, err := rand.Read(randomBytes); err == nil {
		return hex.EncodeToString(randomBytes)
	}

	fallback := sha256.Sum256(
		[]byte(
			fmt.Sprintf(
				"%d:%d",
				time.Now().UnixNano(),
				os.Getpid(),
			),
		),
	)

	return hex.EncodeToString(fallback[:12])
}

// Recent reads the latest n valid events (returned in time order).
// It returns (event list, corrupt line count); a missing file returns an empty list with zero corruption.
func Recent(root string, n int) ([]Event, int) {
	file, err := os.Open(ledgerPath(root))
	if err != nil {
		return []Event{}, 0
	}
	defer file.Close()

	var all []Event

	corrupt := 0

	// A line longer than the bound counts as one corrupt line and reading goes
	// on; bufio.Scanner stopped at it silently, so every event after an
	// oversize line vanished from the read-back a terminal event depends on.
	reader := bufio.NewReaderSize(file, 64*1024)
	for {
		line, tooLong, err := readLedgerLine(reader, maxLedgerLineBytes)
		if err != nil {
			break
		}
		if tooLong {
			corrupt++
			continue
		}
		if len(line) == 0 {
			continue
		}

		var event Event
		if err := json.Unmarshal(line, &event); err != nil {
			corrupt++
			continue
		}

		all = append(all, event)
	}

	if n > 0 && len(all) > n {
		all = all[len(all)-n:]
	}
	if all == nil {
		all = []Event{}
	}

	return all, corrupt
}

// maxLedgerLineBytes bounds one JSONL event on read; a longer line is reported
// as corrupt and skipped whole, never truncated into a different event.
const maxLedgerLineBytes = 1024 * 1024

// readLedgerLine returns the next line without its terminator, whether it
// exceeded maxBytes, and io.EOF once the file is exhausted. An unterminated
// final line is returned like any other.
func readLedgerLine(reader *bufio.Reader, maxBytes int) ([]byte, bool, error) {
	var line []byte
	tooLong := false
	for {
		part, isPrefix, err := reader.ReadLine()
		if err != nil {
			return line, tooLong, err
		}
		if !tooLong {
			if len(line)+len(part) > maxBytes {
				tooLong = true
				line = nil
			} else {
				line = append(line, part...)
			}
		}
		if !isPrefix {
			return line, tooLong, nil
		}
	}
}

// EstimateTokens is a rough token estimate for trend reference only.
func EstimateTokens(text string) int {
	return utf8.RuneCountInString(text) * 6 / 10
}
