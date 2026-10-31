// Table-driven and protocol compatibility tests of the Ledger package.
//
// Coverage:
//  1. complete round trip of the v4 fields;
//  2. Append fills schema, event_id, nanosecond time, result, and the experiment environment automatically;
//  3. a caller's explicit experiment context must not be overridden by the environment;
//  4. event ID uniqueness;
//  5. backward compatibility of old v1 lines;
//  6. corrupt lines skipped and counted;
//  7. enabled=false skips silently;
//  8. Recent takes the latest n in forward order;
//  9. the EstimateTokens rough coefficient.
//
// Every case builds a repository in t.TempDir, independent of a real user repository.
package ledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeRawLedgerLines writes raw lines directly, to build old-format or corrupt lines.
func writeRawLedgerLines(
	t *testing.T,
	root string,
	lines []string,
) {
	t.Helper()

	dir := filepath.Join(root, ".aoci")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建.aoci目录失败: %v", err)
	}

	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(
		filepath.Join(dir, "ledger.jsonl"),
		[]byte(content),
		0o644,
	); err != nil {
		t.Fatalf("写入原始台账失败: %v", err)
	}
}

func ledgerBoolPointer(value bool) *bool {
	return &value
}

func assertHexString(
	t *testing.T,
	value string,
	length int,
) {
	t.Helper()

	if len(value) != length {
		t.Fatalf(
			"十六进制字符串长度期望%d,实得%d: %q",
			length,
			len(value),
			value,
		)
	}

	for _, character := range value {
		if !strings.ContainsRune(
			"0123456789abcdef",
			character,
		) {
			t.Fatalf(
				"字符串含非小写十六进制字符%q: %q",
				character,
				value,
			)
		}
	}
}

// TestAppendAndRecentV8RoundTrip verifies all v8 fields survive a round trip.
func TestAppendAndRecentV8RoundTrip(t *testing.T) {
	root := t.TempDir()

	ev := Event{
		SchemaVersion: EventSchemaVersion,
		EventID:       "00112233445566778899aabb",
		Op:            "overview",
		PathsCount:    3,
		DurationMs:    1234,
		Source:        SourceAgent,

		GenerationSource: "host_agent",
		AgentName:        "codex",
		Result:           ResultOK,

		Model:         "qwen2.5-coder",
		Provider:      "openai-compatible",
		EndpointHash:  "6e0b3f1c9a2d4e85",
		InputTokens:   120,
		OutputTokens:  16,
		TokenSrc:      TokenSourceExact,
		CostEstimate:  0.0021,
		DraftRunID:    "run-20260708-001",
		WarningsCount: 2,

		AppliedCount:   2,
		RecoveredCount: 3,
		RejectedCount:  1,
		RejectKinds:    "format",

		ExperimentID:    "experiment-e1",
		TaskID:          "task-a03r",
		ExperimentGroup: "aoci",
		RunID:           "run-001",
		SessionID:       "session-001",
		AgentModel:      "gpt-5.6-sol",
		MemoryMode:      "off",
		RepositoryHead:  "0123456789012345678901234567890123456789",
		BinaryVersion:   "d984f99",

		DeliveryMode:     "full",
		FullTextIncluded: ledgerBoolPointer(true),
		IndexSHA256:      strings.Repeat("a", 64),
		RepositorySHA256: strings.Repeat("b", 64),
		IndexBytes:       36606,
		OutputBytes:      37200,
		EstimatedTokens:  14282,
		SectionCount:     14,
		EntryCount:       119,

		AOCIToolCalls:     2,
		ShellAOCICalls:    0,
		OverviewReads:     1,
		LocalRecalls:      1,
		SemanticFiles:     3,
		FormatOnlyFiles:   4,
		DuplicateApplies:  1,
		RepeatedMaintains: 1,

		RecoveryStatus:        "superseded_recovered",
		RecoveryTransactionID: strings.Repeat("c", 64),
		PreIndexSHA256:        strings.Repeat("d", 64),
		PostIndexSHA256:       strings.Repeat("e", 64),
		BaselineSHA256:        strings.Repeat("f", 64),
		GovernanceReceipts:    []string{strings.Repeat("1", 64)},
	}

	Append(root, true, ev)

	got, corrupt := Recent(root, 0)
	if corrupt != 0 {
		t.Fatalf("期望零损坏行,实得%d", corrupt)
	}
	if len(got) != 1 {
		t.Fatalf("期望读回1条,实得%d", len(got))
	}

	current := got[0]

	if current.Ts == "" {
		t.Fatal("Ts应由Append自动填充")
	}
	if _, err := time.Parse(time.RFC3339Nano, current.Ts); err != nil {
		t.Fatalf("Ts不是RFC3339Nano: %q %v", current.Ts, err)
	}

	if current.SchemaVersion != EventSchemaVersion ||
		current.EventID != ev.EventID ||
		current.Op != ev.Op ||
		current.Result != ResultOK {
		t.Fatalf("v4基础字段往返不一致: %+v", current)
	}

	if current.Model != ev.Model ||
		current.Provider != ev.Provider ||
		current.EndpointHash != ev.EndpointHash ||
		current.InputTokens != ev.InputTokens ||
		current.OutputTokens != ev.OutputTokens ||
		current.TokenSrc != ev.TokenSrc ||
		current.CostEstimate != ev.CostEstimate {
		t.Fatalf("模型与Token字段往返不一致: %+v", current)
	}

	if current.ExperimentID != ev.ExperimentID ||
		current.TaskID != ev.TaskID ||
		current.ExperimentGroup != ev.ExperimentGroup ||
		current.RunID != ev.RunID ||
		current.SessionID != ev.SessionID ||
		current.AgentModel != ev.AgentModel ||
		current.MemoryMode != ev.MemoryMode ||
		current.RepositoryHead != ev.RepositoryHead ||
		current.BinaryVersion != ev.BinaryVersion {
		t.Fatalf("实验上下文字段往返不一致: %+v", current)
	}

	if current.FullTextIncluded == nil ||
		!*current.FullTextIncluded ||
		current.DeliveryMode != "full" ||
		current.IndexBytes != 36606 ||
		current.OutputBytes != 37200 ||
		current.EstimatedTokens != 14282 ||
		current.SectionCount != 14 ||
		current.EntryCount != 119 {
		t.Fatalf("Overview观测字段往返不一致: %+v", current)
	}
	if current.AOCIToolCalls != 2 || current.OverviewReads != 1 ||
		current.LocalRecalls != 1 || current.SemanticFiles != 3 ||
		current.FormatOnlyFiles != 4 || current.DuplicateApplies != 1 ||
		current.RepeatedMaintains != 1 || current.RecoveredCount != 3 {
		t.Fatalf("Auto低开销与防重字段往返不一致: %+v", current)
	}
	if current.RecoveryStatus != ev.RecoveryStatus ||
		current.RecoveryTransactionID != ev.RecoveryTransactionID ||
		current.PreIndexSHA256 != ev.PreIndexSHA256 ||
		current.PostIndexSHA256 != ev.PostIndexSHA256 ||
		current.BaselineSHA256 != ev.BaselineSHA256 ||
		strings.Join(current.GovernanceReceipts, "\x00") !=
			strings.Join(ev.GovernanceReceipts, "\x00") {
		t.Fatalf("Entries recovery proof fields did not round-trip: %+v", current)
	}
}

// TestAppendAutoEnrichment verifies that a new event is filled automatically with the experiment-level basic evidence.
func TestAppendAutoEnrichment(t *testing.T) {
	root := t.TempDir()

	t.Setenv("AOCI_EXPERIMENT_ID", " experiment-e1 ")
	t.Setenv("AOCI_TASK_ID", "task-a03r")
	t.Setenv("AOCI_EXPERIMENT_GROUP", "aoci")
	t.Setenv("AOCI_RUN_ID", "run-002")
	t.Setenv("AOCI_SESSION_ID", "session-002")
	t.Setenv("AOCI_AGENT_MODEL", "gpt-5.6-sol")
	t.Setenv("AOCI_MEMORY_MODE", "off")
	t.Setenv(
		"AOCI_REPOSITORY_HEAD",
		"0123456789012345678901234567890123456789",
	)
	t.Setenv("AOCI_BINARY_VERSION", "d984f99")

	Append(
		root,
		true,
		Event{
			Op:     "rules",
			Source: SourceAgent,
		},
	)

	got, corrupt := Recent(root, 0)
	if corrupt != 0 || len(got) != 1 {
		t.Fatalf(
			"自动补齐事件读取异常: count=%d corrupt=%d",
			len(got),
			corrupt,
		)
	}

	current := got[0]

	if current.SchemaVersion != EventSchemaVersion {
		t.Fatalf(
			"schema_version期望%d,实得%d",
			EventSchemaVersion,
			current.SchemaVersion,
		)
	}
	assertHexString(t, current.EventID, 24)

	if current.Result != ResultOK {
		t.Fatalf("默认Result期望ok,实得%q", current.Result)
	}
	if _, err := time.Parse(time.RFC3339Nano, current.Ts); err != nil {
		t.Fatalf("自动时间戳非法: %q %v", current.Ts, err)
	}

	if current.ExperimentID != "experiment-e1" ||
		current.TaskID != "task-a03r" ||
		current.ExperimentGroup != "aoci" ||
		current.RunID != "run-002" ||
		current.SessionID != "session-002" ||
		current.AgentModel != "gpt-5.6-sol" ||
		current.MemoryMode != "off" ||
		current.BinaryVersion != "d984f99" {
		t.Fatalf("实验环境自动补齐不完整: %+v", current)
	}
}

// TestAppendPreservesExplicitContext verifies that environment variables cannot override the caller's facts.
func TestAppendPreservesExplicitContext(t *testing.T) {
	root := t.TempDir()

	t.Setenv("AOCI_EXPERIMENT_ID", "environment-experiment")
	t.Setenv("AOCI_TASK_ID", "environment-task")
	t.Setenv("AOCI_MEMORY_MODE", "on")

	Append(
		root,
		true,
		Event{
			SchemaVersion: 99,
			EventID:       "ffeeddccbbaa998877665544",
			Op:            "entries_apply",
			Result:        ResultRejected,
			FailCode:      "bad_args",
			ExperimentID:  "explicit-experiment",
			TaskID:        "explicit-task",
			MemoryMode:    "off",
		},
	)

	got, _ := Recent(root, 0)
	current := got[0]

	if current.SchemaVersion != 99 ||
		current.EventID != "ffeeddccbbaa998877665544" ||
		current.Result != ResultRejected ||
		current.ExperimentID != "explicit-experiment" ||
		current.TaskID != "explicit-task" ||
		current.MemoryMode != "off" {
		t.Fatalf("调用方显式事实被覆盖: %+v", current)
	}
}

// TestExperimentContextWhitespaceNormalization verifies that an environment value does not carry newlines into the ledger.
func TestExperimentContextWhitespaceNormalization(t *testing.T) {
	root := t.TempDir()

	t.Setenv(
		"AOCI_TASK_ID",
		"  task-one\r\n task-two   task-three  ",
	)

	Append(root, true, Event{Op: "verify"})

	got, _ := Recent(root, 0)
	if got[0].TaskID != "task-one task-two task-three" {
		t.Fatalf("上下文空白归一失败: %q", got[0].TaskID)
	}
}

// TestEventIDsUnique verifies that consecutive events have independent event identities.
func TestEventIDsUnique(t *testing.T) {
	root := t.TempDir()

	Append(root, true, Event{Op: "rules"})
	Append(root, true, Event{Op: "overview"})

	got, corrupt := Recent(root, 0)
	if corrupt != 0 || len(got) != 2 {
		t.Fatalf(
			"连续事件读取异常: count=%d corrupt=%d",
			len(got),
			corrupt,
		)
	}

	assertHexString(t, got[0].EventID, 24)
	assertHexString(t, got[1].EventID, 24)

	if got[0].EventID == got[1].EventID {
		t.Fatalf("连续事件ID重复: %q", got[0].EventID)
	}
}

// TestOldV1LineCompatible verifies that historical events read without migration.
func TestOldV1LineCompatible(t *testing.T) {
	root := t.TempDir()

	oldLine := `{"ts":"2026-07-04T10:00:00Z","op":"verify","paths_count":64,"duration_ms":88,"drift_warned":true,"source":"human"}`
	writeRawLedgerLines(t, root, []string{oldLine})

	got, corrupt := Recent(root, 0)
	if corrupt != 0 {
		t.Fatalf("旧格式行不应计为损坏,实得%d", corrupt)
	}
	if len(got) != 1 {
		t.Fatalf("期望读回1条,实得%d", len(got))
	}

	current := got[0]
	if current.Op != "verify" ||
		current.PathsCount != 64 ||
		!current.DriftWarned ||
		current.Source != SourceHuman {
		t.Fatalf("v1字段解析不一致: %+v", current)
	}

	if current.SchemaVersion != 0 ||
		current.EventID != "" ||
		current.Result != "" ||
		current.ExperimentID != "" ||
		current.TaskID != "" ||
		current.FullTextIncluded != nil ||
		current.IndexSHA256 != "" {
		t.Fatalf("旧行新增字段应保持零值: %+v", current)
	}
}

// TestCorruptLineSkipped verifies that a corrupt line is skipped and counted.
func TestCorruptLineSkipped(t *testing.T) {
	root := t.TempDir()

	writeRawLedgerLines(
		t,
		root,
		[]string{
			`{"ts":"2026-07-04T10:00:00Z","op":"scan","source":"human"}`,
			`{这不是合法JSON`,
			`{"ts":"2026-07-04T10:01:00Z","op":"verify","source":"human"}`,
		},
	)

	got, corrupt := Recent(root, 0)
	if corrupt != 1 {
		t.Fatalf("期望1条损坏行,实得%d", corrupt)
	}
	if len(got) != 2 ||
		got[0].Op != "scan" ||
		got[1].Op != "verify" {
		t.Fatalf("有效行顺序或内容不符: %+v", got)
	}
}

// TestAppendDisabled verifies that no ledger file is created when disabled.
func TestAppendDisabled(t *testing.T) {
	root := t.TempDir()

	Append(
		root,
		false,
		Event{
			Op:     "scan",
			Source: SourceHuman,
		},
	)

	path := filepath.Join(root, ".aoci", "ledger.jsonl")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("禁用时不应创建台账文件,Stat err=%v", err)
	}

	got, corrupt := Recent(root, 0)
	if len(got) != 0 || corrupt != 0 {
		t.Fatalf(
			"禁用后Recent应为空,实得%d条%d损坏",
			len(got),
			corrupt,
		)
	}
}

// TestRecentLimit verifies that Recent takes the latest n and keeps the write order.
func TestRecentLimit(t *testing.T) {
	root := t.TempDir()

	for _, operation := range []string{"a", "b", "c", "d", "e"} {
		Append(
			root,
			true,
			Event{
				Op:     operation,
				Source: SourceHuman,
			},
		)
	}

	got, corrupt := Recent(root, 3)
	if corrupt != 0 {
		t.Fatalf("期望零损坏,实得%d", corrupt)
	}
	if len(got) != 3 {
		t.Fatalf("期望3条,实得%d", len(got))
	}

	wanted := []string{"c", "d", "e"}
	for index, operation := range wanted {
		if got[index].Op != operation {
			t.Fatalf(
				"第%d条期望op=%s,实得%s",
				index,
				operation,
				got[index].Op,
			)
		}
	}
}

// TestEstimateTokens verifies the stable reading of the rune count times 0.6, rounded.
func TestEstimateTokens(t *testing.T) {
	cases := []struct {
		text string
		want int
	}{
		{"", 0},
		{"abcde", 3},
		{"你好世界啊", 3},
		{"ab你好", 2},
	}

	for _, current := range cases {
		if got := EstimateTokens(current.text); got != current.want {
			t.Fatalf(
				"EstimateTokens(%q)期望%d,实得%d",
				current.text,
				current.want,
				got,
			)
		}
	}
}

// Locks: an oversize line is one corrupt line and reading continues past it;
// bufio.Scanner stopped there silently, hiding every later event from the
// read-back a terminal event depends on (#121). An unterminated final line
// still decodes.
func TestRecentSkipsOversizeLineAndKeepsLaterEvents(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".aoci", "ledger.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	huge := strings.Repeat("y", maxLedgerLineBytes+1)
	if err := os.WriteFile(path, []byte(huge+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	Append(root, true, Event{Op: "after_oversize"})
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"op":"unterminated"}`); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	events, corrupt := Recent(root, 0)
	if corrupt != 1 || len(events) != 2 || events[0].Op != "after_oversize" || events[1].Op != "unterminated" {
		t.Fatalf("events = %+v corrupt = %d", events, corrupt)
	}
}
