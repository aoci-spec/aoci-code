// 四态差集、快照和严格单文件速查测试。
package baseline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
)

// buildRepo使用t.TempDir创建独立测试仓库。
func buildRepo(
	t *testing.T,
) (
	string,
	func(relPath string, content string),
) {
	t.Helper()

	root := t.TempDir()

	write := func(
		relPath string,
		content string,
	) {
		path := filepath.Join(
			root,
			filepath.FromSlash(relPath),
		)

		if err := os.MkdirAll(
			filepath.Dir(path),
			0o755,
		); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(
			path,
			[]byte(content),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
	}

	return root, write
}

// buildSingleEntryDocument构造包含单个文件条目的最小索引。
func buildSingleEntryDocument(
	t *testing.T,
	root string,
	relPath string,
) *index.Document {
	t.Helper()

	directory := filepath.Dir(
		filepath.FromSlash(relPath),
	)
	fileName := filepath.Base(
		filepath.FromSlash(relPath),
	)

	sectionPath := root

	if directory != "." {
		sectionPath = filepath.Join(
			root,
			directory,
		)
	}

	text := "===段 " +
		filepath.ToSlash(sectionPath) +
		"/===\n" +
		fileName +
		"[T.T.5.T]: F:- | R:- | A:- | S:-\n"

	document, warnings := index.Parse(text)

	if len(warnings) != 0 {
		t.Fatalf(
			"测试索引不应产生警告: %+v",
			warnings,
		)
	}

	index.ResolveRelPaths(
		document,
		root,
	)

	return document
}

// TestDetectFourStates验证四态、非nil切片与.aoci排除口径。
func TestDetectFourStates(t *testing.T) {
	root, write := buildRepo(t)

	write("src/a.go", "A1")
	write("src/b.go", "B1")
	write("src/d.go", "D1")

	options := afs.WalkOptions{
		ExcludeFiles: []string{
			"*.backup.*",
		},
	}

	firstSnapshot, _, err := Snapshot(
		root,
		options,
	)
	if err != nil {
		t.Fatal(err)
	}

	baselineValue := NewBaseline(
		map[string]Fingerprint{
			"src/a.go": firstSnapshot["src/a.go"],
			"src/b.go": firstSnapshot["src/b.go"],
		},
	)

	write("src/b.go", "B2")

	if err := os.Remove(
		filepath.Join(
			root,
			"src",
			"a.go",
		),
	); err != nil {
		t.Fatal(err)
	}

	write("src/new.go", "N1")

	indexText := strings.Join(
		[]string{
			"===段 " + filepath.ToSlash(root) + "/src/===",
			"a.go[T.T.5.T]: F:- | R:- | A:- | S:-",
			"b.go[T.T.5.T]: F:- | R:- | A:- | S:-",
			"d.go[T.T.5.T]: F:- | R:- | A:- | S:-",
			"lost_dir/[T.T.5.T]: F:- | R:- | A:- | S:-",
			"===点aoci段 " + filepath.ToSlash(root) + "/.aoci/===",
			"index.txt[T.T.5.T]: F:.aoci条目应被排除不判Orphan | R:- | A:- | S:-",
		},
		"\n",
	)

	document, _ := index.Parse(indexText)

	index.ResolveRelPaths(
		document,
		root,
	)

	secondSnapshot, _, err := Snapshot(
		root,
		options,
	)
	if err != nil {
		t.Fatal(err)
	}

	result := Detect(
		root,
		document,
		baselineValue,
		secondSnapshot,
		options,
	)

	if result.Missing == nil ||
		result.Orphan == nil ||
		result.Stale == nil ||
		result.Unbaselined == nil ||
		result.LineEndingOnly == nil {
		t.Fatal("结果slice必须非nil")
	}

	assertPaths := func(
		name string,
		got []string,
		expected ...string,
	) {
		t.Helper()

		if strings.Join(got, ",") !=
			strings.Join(expected, ",") {
			t.Fatalf(
				"%s不符: want=%v got=%v",
				name,
				expected,
				got,
			)
		}
	}

	assertPaths(
		"Missing",
		result.Missing,
		"src/new.go",
	)

	assertPaths(
		"Orphan",
		result.Orphan,
		"src/a.go",
		"src/lost_dir/",
	)

	assertPaths(
		"Stale",
		result.Stale,
		"src/b.go",
	)

	assertPaths(
		"Unbaselined",
		result.Unbaselined,
		"src/d.go",
	)

	assertPaths(
		"LineEndingOnly",
		result.LineEndingOnly,
	)

	for _, orphan := range result.Orphan {
		if strings.Contains(
			orphan,
			".aoci",
		) {
			t.Fatal(".aoci下条目不得判Orphan")
		}
	}
}

// TestDetectNilBaseline验证无Baseline时不误报Stale。
func TestDetectNilBaseline(t *testing.T) {
	root, write := buildRepo(t)

	write("m.go", "M1")

	document := buildSingleEntryDocument(
		t,
		root,
		"m.go",
	)

	snapshot, _, err := Snapshot(
		root,
		afs.WalkOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}

	result := Detect(
		root,
		document,
		nil,
		snapshot,
		afs.WalkOptions{},
	)

	if len(result.Stale) != 0 ||
		len(result.Unbaselined) != 1 {
		t.Fatalf(
			"无Baseline应Unbaselined=1 Stale=0: %+v",
			result,
		)
	}
}

// TestIsStaleFile验证严格单文件速查兼容语义。
func TestIsStaleFile(t *testing.T) {
	root, write := buildRepo(t)

	write("x.go", "X1")

	fingerprint, err := HashFile(
		filepath.Join(
			root,
			"x.go",
		),
	)
	if err != nil {
		t.Fatal(err)
	}

	baselineValue := NewBaseline(
		map[string]Fingerprint{
			"x.go": fingerprint,
		},
	)

	if stale, unbaselined :=
		IsStaleFile(
			root,
			"x.go",
			baselineValue,
		); stale || unbaselined {
		t.Fatal("未改动不应stale或unbaselined")
	}

	write("x.go", "X2")

	if stale, _ := IsStaleFile(
		root,
		"x.go",
		baselineValue,
	); !stale {
		t.Fatal("改动后应stale")
	}

	if _, unbaselined := IsStaleFile(
		root,
		"ghost.go",
		baselineValue,
	); !unbaselined {
		t.Fatal("Baseline缺失应unbaselined")
	}
}

// TestSnapshotSymlinkAndSort验证符号链接排除和排序。
func TestSnapshotSymlinkAndSort(t *testing.T) {
	root, write := buildRepo(t)

	write("b.go", "B")
	write("a.go", "A")

	if err := os.Symlink(
		filepath.Join(root, "a.go"),
		filepath.Join(root, "link.go"),
	); err == nil {
		snapshot, _, err := Snapshot(
			root,
			afs.WalkOptions{},
		)
		if err != nil {
			t.Fatal(err)
		}

		if _, exists := snapshot["link.go"]; exists {
			t.Fatal("符号链接不得入快照")
		}
	}

	files, err := afs.WalkRepo(
		root,
		afs.WalkOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(files) >= 2 &&
		files[0] > files[1] {
		t.Fatal("WalkRepo输出必须有序")
	}
}
