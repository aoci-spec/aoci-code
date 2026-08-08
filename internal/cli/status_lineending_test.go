// status --deep换行宽容生产路径测试。
package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
)

// TestStatusDeepLineEndingToleranceAndStrictMode验证默认宽容和团队严格模式。
func TestStatusDeepLineEndingToleranceAndStrictMode(
	t *testing.T,
) {
	root, cfg := buildVerifyLineEndingRepo(t)

	if err := os.WriteFile(
		filepath.Join(
			root,
			"x.go",
		),
		[]byte(
			"package x\r\n\r\nvar Value = 1\r\n",
		),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	paths := config.AOCIPaths(
		root,
		cfg.IndexPath,
	)

	tolerantConfig, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	tolerant, err := buildStatusDeepCounts(
		root,
		paths.IndexPath,
		tolerantConfig,
	)
	if err != nil {
		t.Fatal(err)
	}

	if tolerant.Stale != 0 ||
		tolerant.LineEndingOnly != 1 ||
		tolerant.Missing != 0 ||
		tolerant.Orphan != 0 ||
		tolerant.Unbaselined != 0 {
		t.Fatalf(
			"默认宽容status --deep口径不符: %+v",
			tolerant,
		)
	}

	cfg.LineEndingTolerance = false

	if err := config.Save(
		root,
		cfg,
	); err != nil {
		t.Fatal(err)
	}

	strictConfig, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	strict, err := buildStatusDeepCounts(
		root,
		paths.IndexPath,
		strictConfig,
	)
	if err != nil {
		t.Fatal(err)
	}

	if strict.Stale != 1 ||
		strict.LineEndingOnly != 0 {
		t.Fatalf(
			"团队严格status --deep口径不符: %+v",
			strict,
		)
	}
}

// TestStatusDeepLineEndingToleranceDoesNotHideRealChange验证真实变化仍为Stale。
func TestStatusDeepLineEndingToleranceDoesNotHideRealChange(
	t *testing.T,
) {
	root, cfg := buildVerifyLineEndingRepo(t)

	if err := os.WriteFile(
		filepath.Join(
			root,
			"x.go",
		),
		[]byte(
			"package x\r\n\r\nvar Value = 2\r\n",
		),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	paths := config.AOCIPaths(
		root,
		cfg.IndexPath,
	)

	loadedConfig, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	counts, err := buildStatusDeepCounts(
		root,
		paths.IndexPath,
		loadedConfig,
	)
	if err != nil {
		t.Fatal(err)
	}

	if counts.Stale != 1 ||
		counts.LineEndingOnly != 0 {
		t.Fatalf(
			"真实内容变化不得被换行宽容吞掉: %+v",
			counts,
		)
	}
}
