// .aoci/curation.json路径模型测试。
package config

import (
	"path/filepath"
	"testing"
)

func TestAOCIPathsIncludesCurationAsset(
	t *testing.T,
) {
	root := t.TempDir()

	paths := AOCIPaths(
		root,
		"aoci.txt",
	)

	want := filepath.Join(
		root,
		".aoci",
		"curation.json",
	)

	if paths.CurationPath != want {
		t.Fatalf(
			"CurationPath不符: got=%q want=%q",
			paths.CurationPath,
			want,
		)
	}
}
