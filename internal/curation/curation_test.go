// 文件级策展资产、画像和Missing分类测试。
package curation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/config"
)

func writeCurationFile(
	t *testing.T,
	root,
	rel string,
	data []byte,
) {
	t.Helper()

	absolutePath := filepath.Join(
		root,
		filepath.FromSlash(rel),
	)
	if err := os.MkdirAll(
		filepath.Dir(absolutePath),
		0o755,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		absolutePath,
		data,
		0o644,
	); err != nil {
		t.Fatal(err)
	}
}

func TestClassificationPendingPreservesThreeWay(
	t *testing.T,
) {
	root := t.TempDir()

	writeCurationFile(
		t,
		root,
		"src/main.go",
		[]byte("package main\n"),
	)
	writeCurationFile(
		t,
		root,
		"pkg/py.typed",
		[]byte{},
	)
	writeCurationFile(
		t,
		root,
		"assets/logo.bin",
		[]byte{
			'P',
			'N',
			'G',
			0,
			1,
		},
	)
	writeCurationFile(
		t,
		root,
		"docs/guide.md",
		[]byte("# guide\n"),
	)

	cfg := config.DefaultConfig()
	cfg.CurationExclude = []string{
		"docs",
	}

	classification, _, _, err :=
		BuildClassification(
			root,
			cfg,
			[]string{
				"src/main.go",
				"pkg/py.typed",
				"assets/logo.bin",
				"docs/guide.md",
				"missing/unreadable.txt",
			},
		)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Join(
		classification.Actionable,
		",",
	) != "src/main.go" {
		t.Fatalf(
			"普通文本应直接Actionable: %+v",
			classification,
		)
	}

	if len(classification.Pending) != 2 ||
		classification.Pending[0].Path !=
			"assets/logo.bin" ||
		classification.Pending[1].Path !=
			"pkg/py.typed" {
		t.Fatalf(
			"empty/binary应进入Pending: %+v",
			classification.Pending,
		)
	}

	if len(classification.Skipped) != 3 {
		t.Fatalf(
			"Pending两项加unreadable一项应均在Skipped中: %+v",
			classification.Skipped,
		)
	}

	if len(classification.CurationExcluded) != 1 ||
		classification.CurationExcluded[0].Path !=
			"docs/guide.md" {
		t.Fatalf(
			"团队策展排除不符: %+v",
			classification.CurationExcluded,
		)
	}

	if len(classification.Actionable)+
		len(classification.CurationExcluded)+
		len(classification.Skipped) !=
		len(classification.Missing) {
		t.Fatalf(
			"Missing三分必须守恒: %+v",
			classification,
		)
	}
}

func TestValidDecisionsPromoteOrExclude(
	t *testing.T,
) {
	root := t.TempDir()

	writeCurationFile(
		t,
		root,
		"pkg/py.typed",
		[]byte{},
	)
	writeCurationFile(
		t,
		root,
		"pkg/__init__.py",
		[]byte{},
	)

	profiles := BuildProfiles(
		root,
		[]string{
			"pkg/py.typed",
			"pkg/__init__.py",
		},
	)

	document := NewDocument()
	document.Decisions = []Decision{
		{
			Path:         "pkg/py.typed",
			Decision:     DecisionInclude,
			Role:         "PEP 561类型信息标记",
			Reason:       "空文件本身声明该包发布类型信息",
			Confidence:   100,
			SourceSHA256: profiles["pkg/py.typed"].SourceSHA256,
			Agent:        "codex",
			UpdatedAt:    "2026-07-15T00:00:00Z",
		},
		{
			Path:         "pkg/__init__.py",
			Decision:     DecisionExclude,
			Role:         "Python包边界标记",
			Reason:       "该空文件没有独立于目录和模块条目的维护语义",
			Confidence:   95,
			SourceSHA256: profiles["pkg/__init__.py"].SourceSHA256,
			Agent:        "codex",
			UpdatedAt:    "2026-07-15T00:00:00Z",
		},
	}

	classification := Classify(
		config.DefaultConfig(),
		[]string{
			"pkg/py.typed",
			"pkg/__init__.py",
		},
		profiles,
		document,
	)

	if len(classification.Actionable) != 1 ||
		classification.Actionable[0] !=
			"pkg/py.typed" ||
		len(classification.Included) != 1 {
		t.Fatalf(
			"include决策应把空文件提升为Actionable: %+v",
			classification,
		)
	}

	if len(classification.CurationExcluded) != 1 ||
		classification.CurationExcluded[0].Path !=
			"pkg/__init__.py" {
		t.Fatalf(
			"exclude决策应进入CurationExcluded: %+v",
			classification,
		)
	}

	if len(classification.Pending) != 0 ||
		len(classification.Skipped) != 0 {
		t.Fatalf(
			"有效决策后不应继续Pending或Skipped: %+v",
			classification,
		)
	}
}

func TestDecisionBecomesStaleAfterSourceChange(
	t *testing.T,
) {
	root := t.TempDir()

	writeCurationFile(
		t,
		root,
		"assets/data.bin",
		[]byte{
			'A',
			0,
			1,
		},
	)

	profilesBefore := BuildProfiles(
		root,
		[]string{
			"assets/data.bin",
		},
	)

	document := NewDocument()
	document.Decisions = []Decision{
		{
			Path:         "assets/data.bin",
			Decision:     DecisionInclude,
			Role:         "协议测试向量",
			Reason:       "二进制内容具有独立仓库级语义",
			Confidence:   90,
			SourceSHA256: profilesBefore["assets/data.bin"].SourceSHA256,
			Agent:        "codex",
			UpdatedAt:    "2026-07-15T00:00:00Z",
		},
	}

	writeCurationFile(
		t,
		root,
		"assets/data.bin",
		[]byte{
			'B',
			0,
			2,
		},
	)

	profilesAfter := BuildProfiles(
		root,
		[]string{
			"assets/data.bin",
		},
	)

	classification := Classify(
		config.DefaultConfig(),
		[]string{
			"assets/data.bin",
		},
		profilesAfter,
		document,
	)

	if len(classification.StaleDecisions) != 1 ||
		classification.StaleDecisions[0] !=
			"assets/data.bin" {
		t.Fatalf(
			"源码变化后旧策展决策应失效: %+v",
			classification,
		)
	}

	if len(classification.Pending) != 1 ||
		len(classification.Skipped) != 1 ||
		len(classification.Actionable) != 0 {
		t.Fatalf(
			"失效的binary include应重新等待策展: %+v",
			classification,
		)
	}
}

func TestStoreRoundTripAndMerge(
	t *testing.T,
) {
	root := t.TempDir()

	base := NewDocument()
	base.Decisions = []Decision{
		{
			Path:         "pkg/py.typed",
			Decision:     DecisionInclude,
			Role:         "类型标记",
			Reason:       "具有PEP 561语义",
			Confidence:   100,
			SourceSHA256: strings.Repeat("a", 64),
			Agent:        "codex",
			Model:        "model-a",
			UpdatedAt:    "2026-07-15T00:00:00Z",
		},
	}

	if err := Save(
		root,
		base,
	); err != nil {
		t.Fatal(err)
	}

	loaded, exists, firstHash, err :=
		Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if !exists ||
		firstHash == "" ||
		len(loaded.Decisions) != 1 {
		t.Fatalf(
			"策展资产往返失败: exists=%v hash=%s doc=%+v",
			exists,
			firstHash,
			loaded,
		)
	}

	merged, err := Merge(
		loaded,
		[]Decision{
			{
				Path:         "pkg/py.typed",
				Decision:     DecisionExclude,
				Role:         "类型标记",
				Reason:       "当前项目不发布类型包",
				Confidence:   80,
				SourceSHA256: strings.Repeat("b", 64),
			},
			{
				Path:         "pkg/__init__.py",
				Decision:     DecisionExclude,
				Role:         "包边界标记",
				Reason:       "无独立维护语义",
				Confidence:   95,
				SourceSHA256: strings.Repeat("c", 64),
			},
		},
		"gpt-5.6-thinking",
		"model-b",
		time.Date(
			2026,
			7,
			15,
			1,
			2,
			3,
			0,
			time.UTC,
		),
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(merged.Decisions) != 2 ||
		merged.Decisions[0].Path !=
			"pkg/__init__.py" ||
		merged.Decisions[1].Decision !=
			DecisionExclude ||
		merged.Decisions[1].Agent !=
			"gpt-5.6-thinking" {
		t.Fatalf(
			"合并、替换或排序不符: %+v",
			merged.Decisions,
		)
	}
}
