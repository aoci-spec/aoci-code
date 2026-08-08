// Ledger v5 Entries Check修复状态专项测试。
package ledger

import "testing"

func TestRepairRequiredCountsRoundTrip(
	t *testing.T,
) {
	root := t.TempDir()

	Append(
		root,
		true,
		Event{
			Op:            "entries_check",
			Source:        SourceAgent,
			Result:        ResultRepairRequired,
			PathsCount:    13,
			PassedCount:   10,
			WarnedCount:   2,
			RejectedCount: 1,
			SkippedCount:  0,
			WarningsCount: 2,
			DraftRunID:    "20260722T141134Z",
		},
	)

	events, corrupt := Recent(
		root,
		0,
	)

	if corrupt != 0 ||
		len(events) != 1 {
		t.Fatalf(
			"repair_required事件读取异常: count=%d corrupt=%d",
			len(events),
			corrupt,
		)
	}

	current := events[0]

	if current.SchemaVersion !=
		EventSchemaVersion ||
		current.Result !=
			ResultRepairRequired ||
		current.PathsCount != 13 ||
		current.PassedCount != 10 ||
		current.WarnedCount != 2 ||
		current.RejectedCount != 1 ||
		current.SkippedCount != 0 ||
		current.WarningsCount != 2 ||
		current.DraftRunID !=
			"20260722T141134Z" {
		t.Fatalf(
			"repair_required字段往返不符: %+v",
			current,
		)
	}
}
