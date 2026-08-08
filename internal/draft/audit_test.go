// P-23 草稿审计状态测试:
// generation state 不被覆盖、review/application 追加、草稿摘要稳定、旧 manifest
// 向后兼容。
package draft

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestAppendApplicationSerializesConcurrentAudit(t *testing.T) {
	root := t.TempDir()
	runID := "20260726T120000Z"
	if err := SaveManifest(root, &Manifest{RunID: runID, Kind: KindEntries}); err != nil {
		t.Fatal(err)
	}

	const writers = 16
	errCh := make(chan error, writers)
	var wait sync.WaitGroup
	for position := 0; position < writers; position++ {
		position := position
		wait.Add(1)
		go func() {
			defer wait.Done()
			errCh <- AppendApplication(root, runID, ApplicationRecord{
				DraftHash: fmt.Sprintf("draft-%02d", position),
				Applied:   1,
			}, position == writers-1)
		}()
	}
	wait.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("并发Application追加失败: %v", err)
		}
	}

	manifest, err := LoadManifest(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, record := range manifest.Applications {
		seen[record.DraftHash] = true
	}
	if len(manifest.Applications) != writers || len(seen) != writers || manifest.AppliedAt == "" {
		t.Fatalf("Manifest并发审计不得丢记录或AppliedAt: %+v", manifest)
	}
}

func TestManifestAuditHistoryRoundtrip(t *testing.T) {
	root := t.TempDir()
	runID := "20260713T130000Z"

	if err := WriteFile(
		root,
		runID,
		"a.entry.txt",
		[]byte("a.go[XUT5T]: F:x | R:- | A:- | S:-\n"),
	); err != nil {
		t.Fatal(err)
	}

	manifest := &Manifest{
		RunID: runID,
		Kind:  KindEntries,
		Entries: []EntryStatus{
			{
				Path:   "a.go",
				Status: "warned",
				Note:   "AI 初始警告",
			},
		},
		Files: []string{"a.entry.txt"},
	}
	if err := SaveManifest(root, manifest); err != nil {
		t.Fatal(err)
	}

	hash, err := HashFiles(
		root,
		runID,
		[]string{"a.entry.txt"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(hash) != 64 {
		t.Fatalf("SHA-256 应为64位十六进制: %q", hash)
	}

	if err := AppendReview(
		root,
		runID,
		ReviewRecord{
			Action:     ReviewActionCheck,
			DraftHash:  hash,
			PathsCount: 1,
			Passed:     1,
		},
	); err != nil {
		t.Fatal(err)
	}

	if err := AppendApplication(
		root,
		runID,
		ApplicationRecord{
			DraftHash:  hash,
			PathsCount: 1,
			Applied:    1,
		},
		true,
	); err != nil {
		t.Fatal(err)
	}

	got, err := LoadManifest(root, runID)
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Entries) != 1 ||
		got.Entries[0].Status != "warned" ||
		got.Entries[0].Note != "AI 初始警告" {
		t.Fatalf(
			"generation state 不得被审阅/应用覆盖: %+v",
			got.Entries,
		)
	}
	if len(got.Reviews) != 1 ||
		got.Reviews[0].DraftHash != hash ||
		got.Reviews[0].At == "" {
		t.Fatalf("review state 往返不符: %+v", got.Reviews)
	}
	if len(got.Applications) != 1 ||
		got.Applications[0].Applied != 1 ||
		got.Applications[0].DraftHash != hash ||
		got.Applications[0].At == "" {
		t.Fatalf(
			"application state 往返不符: %+v",
			got.Applications,
		)
	}
	if got.AppliedAt == "" ||
		got.AppliedAt != got.Applications[0].At {
		t.Fatalf(
			"AppliedAt 应与干净 application 同时刻: %+v",
			got,
		)
	}
}

func TestHashFilesChangesAfterManualEdit(t *testing.T) {
	root := t.TempDir()
	runID := "20260713T130001Z"
	name := "a.entry.txt"

	if err := WriteFile(
		root,
		runID,
		name,
		[]byte("first\n"),
	); err != nil {
		t.Fatal(err)
	}
	firstHash, err := HashFiles(
		root,
		runID,
		[]string{name},
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := WriteFile(
		root,
		runID,
		name,
		[]byte("second\n"),
	); err != nil {
		t.Fatal(err)
	}
	secondHash, err := HashFiles(
		root,
		runID,
		[]string{name},
	)
	if err != nil {
		t.Fatal(err)
	}

	if firstHash == secondHash {
		t.Fatalf(
			"人工修改草稿后摘要必须变化: %s",
			firstHash,
		)
	}
}

func TestOldManifestWithoutAuditFieldsLoads(t *testing.T) {
	root := t.TempDir()
	runID := "20260713T130002Z"
	runDir, err := RunDir(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}

	oldJSON := `{
  "run_id": "20260713T130002Z",
  "kind": "entries",
  "created_at": "2026-07-13T13:00:02Z",
  "entries": [
    {"path": "a.go", "status": "drafted"}
  ]
}
`
	if err := os.WriteFile(
		filepath.Join(runDir, ManifestFileName),
		[]byte(oldJSON),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	got, err := LoadManifest(root, runID)
	if err != nil {
		t.Fatalf("旧 manifest 应免迁移读取: %v", err)
	}
	if len(got.Reviews) != 0 ||
		len(got.Applications) != 0 {
		t.Fatalf(
			"旧 manifest 新字段应为空: %+v",
			got,
		)
	}
}
