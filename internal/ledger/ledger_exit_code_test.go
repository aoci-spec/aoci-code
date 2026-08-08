// Ledger业务退出码协议专项测试。
package ledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExitCodeRoundTripAndZeroPreserved验证成功码0不会被omitempty删除，
// 非零业务退出码也能稳定读取。
func TestExitCodeRoundTripAndZeroPreserved(
	t *testing.T,
) {
	root := t.TempDir()

	successCode := 0
	driftCode := 1

	Append(
		root,
		true,
		Event{
			Op:       "verify",
			Source:   SourceHuman,
			ExitCode: &successCode,
		},
	)

	Append(
		root,
		true,
		Event{
			Op:       "check",
			Source:   SourceHuman,
			ExitCode: &driftCode,
		},
	)

	events, corrupt := Recent(root, 0)

	if corrupt != 0 {
		t.Fatalf("期望零损坏行,实得%d", corrupt)
	}

	if len(events) != 2 {
		t.Fatalf("期望2条事件,实得%d", len(events))
	}

	if events[0].ExitCode == nil ||
		*events[0].ExitCode != 0 {
		t.Fatalf(
			"成功事件应显式保存exit_code=0: %+v",
			events[0],
		)
	}

	if events[1].ExitCode == nil ||
		*events[1].ExitCode != 1 {
		t.Fatalf(
			"漂移事件应保存exit_code=1: %+v",
			events[1],
		)
	}

	data, err := os.ReadFile(
		filepath.Join(
			root,
			".aoci",
			"ledger.jsonl",
		),
	)
	if err != nil {
		t.Fatal(err)
	}

	text := string(data)

	if !strings.Contains(
		text,
		`"exit_code":0`,
	) {
		t.Fatalf(
			"JSONL必须显式包含成功码0:\n%s",
			text,
		)
	}

	if !strings.Contains(
		text,
		`"exit_code":1`,
	) {
		t.Fatalf(
			"JSONL必须包含漂移码1:\n%s",
			text,
		)
	}
}

// TestHistoricalEventWithoutExitCodeRemainsCompatible验证旧行未记录业务退出码时
// 保持nil，而不是被错误解释为成功码0。
func TestHistoricalEventWithoutExitCodeRemainsCompatible(
	t *testing.T,
) {
	root := t.TempDir()

	writeRawLedgerLines(
		t,
		root,
		[]string{
			`{"ts":"2026-07-04T10:00:00Z","op":"verify","source":"human"}`,
		},
	)

	events, corrupt := Recent(root, 0)

	if corrupt != 0 ||
		len(events) != 1 {
		t.Fatalf(
			"历史事件读取异常: count=%d corrupt=%d",
			len(events),
			corrupt,
		)
	}

	if events[0].ExitCode != nil {
		t.Fatalf(
			"历史事件未声明exit_code时必须保持nil: %+v",
			events[0],
		)
	}
}
