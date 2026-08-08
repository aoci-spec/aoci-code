// Manifest严格JSON、基础身份与历史兼容测试。
package draft

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRawManifest(
	t *testing.T,
	root,
	runID,
	content string,
) {
	t.Helper()

	runDirectory, err := RunDir(
		root,
		runID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(
		runDirectory,
		0o755,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(
			runDirectory,
			ManifestFileName,
		),
		[]byte(content),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
}

func TestLoadManifestRejectsStrictJSONViolations(
	t *testing.T,
) {
	const runID = "20260716T090000Z"

	tests := []struct {
		name     string
		content  string
		wantPart string
	}{
		{
			name: "duplicate_top_level",
			content: `{
  "run_id":"20260716T090000Z",
  "run_id":"20260716T090001Z",
  "kind":"entries",
  "created_at":"2026-07-16T09:00:00Z"
}`,
			wantPart: "run_id",
		},
		{
			name: "duplicate_nested",
			content: `{
  "run_id":"20260716T090000Z",
  "kind":"entries",
  "created_at":"2026-07-16T09:00:00Z",
  "entries":[
    {"path":"a.go","path":"b.go","status":"drafted"}
  ]
}`,
			wantPart: "entries[0].path",
		},
		{
			name: "unknown_top_level",
			content: `{
  "run_id":"20260716T090000Z",
  "kind":"entries",
  "created_at":"2026-07-16T09:00:00Z",
  "unexpected":true
}`,
			wantPart: `unknown field "unexpected"`,
		},
		{
			name: "unknown_nested",
			content: `{
  "run_id":"20260716T090000Z",
  "kind":"entries",
  "created_at":"2026-07-16T09:00:00Z",
  "entries":[
    {"path":"a.go","status":"drafted","unexpected":true}
  ]
}`,
			wantPart: `unknown field "unexpected"`,
		},
		{
			name: "trailing_object",
			content: `{
  "run_id":"20260716T090000Z",
  "kind":"entries",
  "created_at":"2026-07-16T09:00:00Z"
}
{"second":true}`,
			wantPart: "只能包含一个顶层对象",
		},
	}

	for _, current := range tests {
		t.Run(
			current.name,
			func(t *testing.T) {
				root := t.TempDir()

				writeRawManifest(
					t,
					root,
					runID,
					current.content,
				)

				_, err := LoadManifest(
					root,
					runID,
				)
				if err == nil ||
					!strings.Contains(
						err.Error(),
						current.wantPart,
					) {
					t.Fatalf(
						"错误应包含%q: %v",
						current.wantPart,
						err,
					)
				}
			},
		)
	}
}

func TestLoadManifestRejectsDirectoryIdentitySplit(
	t *testing.T,
) {
	const runID = "20260716T090100Z"

	root := t.TempDir()

	writeRawManifest(
		t,
		root,
		runID,
		`{
  "run_id":"20260716T090101Z",
  "kind":"entries",
  "created_at":"2026-07-16T09:01:00Z"
}`,
	)

	_, err := LoadManifest(
		root,
		runID,
	)
	if err == nil ||
		!strings.Contains(
			err.Error(),
			"与目录run_id不一致",
		) {
		t.Fatalf(
			"目录身份分裂必须拒绝: %v",
			err,
		)
	}
}

func TestLoadManifestKeepsLegacyMissingFieldsCompatible(
	t *testing.T,
) {
	const runID = "20260716T090200Z"

	root := t.TempDir()

	writeRawManifest(
		t,
		root,
		runID,
		`{
  "run_id":"20260716T090200Z",
  "kind":"entries",
  "created_at":"2026-07-16T09:02:00Z",
  "entries":[
    {"path":"a.go","status":"drafted"}
  ]
}`,
	)

	manifest, err := LoadManifest(
		root,
		runID,
	)
	if err != nil {
		t.Fatalf(
			"旧Manifest缺少新字段时仍应可读: %v",
			err,
		)
	}

	if manifest.GenerationSource != "" ||
		manifest.PlanID != "" ||
		manifest.Reviews != nil ||
		manifest.Applications != nil {
		t.Fatalf(
			"旧Manifest新增字段应保持零值: %+v",
			manifest,
		)
	}
}
