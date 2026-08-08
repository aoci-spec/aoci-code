// 换行等价判定与宽容模式测试。
package baseline

import (
	"testing"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
)

// TestEquivalentFingerprints验证严格和宽容判定矩阵。
func TestEquivalentFingerprints(t *testing.T) {
	testCases := []struct {
		name         string
		baseline     Fingerprint
		current      Fingerprint
		tolerate     bool
		wantEqual    bool
		wantLineOnly bool
	}{
		{
			name: "原始字节相同",
			baseline: Fingerprint{
				SHA256:           "raw",
				NormalizedSHA256: "normalized-a",
			},
			current: Fingerprint{
				SHA256:           "raw",
				NormalizedSHA256: "normalized-b",
			},
			tolerate:     false,
			wantEqual:    true,
			wantLineOnly: false,
		},
		{
			name: "严格模式拒绝仅换行差异",
			baseline: Fingerprint{
				SHA256:           "lf",
				NormalizedSHA256: "same",
			},
			current: Fingerprint{
				SHA256:           "crlf",
				NormalizedSHA256: "same",
			},
			tolerate:     false,
			wantEqual:    false,
			wantLineOnly: false,
		},
		{
			name: "宽容模式接受仅换行差异",
			baseline: Fingerprint{
				SHA256:           "lf",
				NormalizedSHA256: "same",
			},
			current: Fingerprint{
				SHA256:           "crlf",
				NormalizedSHA256: "same",
			},
			tolerate:     true,
			wantEqual:    true,
			wantLineOnly: true,
		},
		{
			name: "旧Baseline缺规范化指纹退严格",
			baseline: Fingerprint{
				SHA256: "old",
			},
			current: Fingerprint{
				SHA256:           "new",
				NormalizedSHA256: "same",
			},
			tolerate:     true,
			wantEqual:    false,
			wantLineOnly: false,
		},
		{
			name: "规范化指纹不同仍为真实漂移",
			baseline: Fingerprint{
				SHA256:           "old",
				NormalizedSHA256: "normalized-old",
			},
			current: Fingerprint{
				SHA256:           "new",
				NormalizedSHA256: "normalized-new",
			},
			tolerate:     true,
			wantEqual:    false,
			wantLineOnly: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(
			testCase.name,
			func(t *testing.T) {
				equal, lineOnly :=
					EquivalentFingerprints(
						testCase.baseline,
						testCase.current,
						testCase.tolerate,
					)

				if equal != testCase.wantEqual ||
					lineOnly != testCase.wantLineOnly {
					t.Fatalf(
						"判定错误: equal=%v lineOnly=%v",
						equal,
						lineOnly,
					)
				}
			},
		)
	}
}

// TestDetectWithLineEndingTolerance验证严格壳、宽容判定和真实漂移。
func TestDetectWithLineEndingTolerance(t *testing.T) {
	root, write := buildRepo(t)

	write(
		"src/x.go",
		"package src\n\nvar Value = 1\n",
	)

	options := afs.WalkOptions{}

	firstSnapshot, _, err := Snapshot(
		root,
		options,
	)
	if err != nil {
		t.Fatal(err)
	}

	baselineValue := NewBaseline(
		map[string]Fingerprint{
			"src/x.go": firstSnapshot["src/x.go"],
		},
	)

	document := buildSingleEntryDocument(
		t,
		root,
		"src/x.go",
	)

	write(
		"src/x.go",
		"package src\r\n\r\nvar Value = 1\r\n",
	)

	secondSnapshot, _, err := Snapshot(
		root,
		options,
	)
	if err != nil {
		t.Fatal(err)
	}

	strictResult := Detect(
		root,
		document,
		baselineValue,
		secondSnapshot,
		options,
	)

	if len(strictResult.Stale) != 1 ||
		len(strictResult.LineEndingOnly) != 0 {
		t.Fatalf(
			"严格Detect必须保持历史行为: %+v",
			strictResult,
		)
	}

	tolerantResult := DetectWith(
		root,
		document,
		baselineValue,
		secondSnapshot,
		options,
		true,
	)

	if len(tolerantResult.Stale) != 0 ||
		len(tolerantResult.LineEndingOnly) != 1 ||
		tolerantResult.LineEndingOnly[0] != "src/x.go" {
		t.Fatalf(
			"宽容模式应仅报告LineEndingOnly: %+v",
			tolerantResult,
		)
	}

	legacyBaseline := NewBaseline(
		map[string]Fingerprint{
			"src/x.go": {
				SHA256: firstSnapshot["src/x.go"].SHA256,
				Size:   firstSnapshot["src/x.go"].Size,
			},
		},
	)

	legacyResult := DetectWith(
		root,
		document,
		legacyBaseline,
		secondSnapshot,
		options,
		true,
	)

	if len(legacyResult.Stale) != 1 ||
		len(legacyResult.LineEndingOnly) != 0 {
		t.Fatalf(
			"旧Baseline缺规范化指纹时必须退严格: %+v",
			legacyResult,
		)
	}

	write(
		"src/x.go",
		"package src\r\n\r\nvar Value = 2\r\n",
	)

	thirdSnapshot, _, err := Snapshot(
		root,
		options,
	)
	if err != nil {
		t.Fatal(err)
	}

	realChangeResult := DetectWith(
		root,
		document,
		baselineValue,
		thirdSnapshot,
		options,
		true,
	)

	if len(realChangeResult.Stale) != 1 ||
		len(realChangeResult.LineEndingOnly) != 0 {
		t.Fatalf(
			"真实内容变化不得被宽容吞掉: %+v",
			realChangeResult,
		)
	}
}

// TestIsStaleFileWithLineEndingTolerance验证单文件宽容信息态。
func TestIsStaleFileWithLineEndingTolerance(t *testing.T) {
	root, write := buildRepo(t)

	write(
		"x.go",
		"alpha\nbeta\n",
	)

	fingerprint, err := HashFile(
		root + "/x.go",
	)
	if err != nil {
		t.Fatal(err)
	}

	baselineValue := NewBaseline(
		map[string]Fingerprint{
			"x.go": fingerprint,
		},
	)

	write(
		"x.go",
		"alpha\r\nbeta\r\n",
	)

	stale, unbaselined, lineOnly :=
		IsStaleFileWith(
			root,
			"x.go",
			baselineValue,
			true,
		)

	if stale || unbaselined || !lineOnly {
		t.Fatalf(
			"仅换行变化应宽容并可见: stale=%v unbaselined=%v lineOnly=%v",
			stale,
			unbaselined,
			lineOnly,
		)
	}
}
