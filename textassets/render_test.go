package textassets

import (
	"strings"
	"testing"
)

func TestRenderHeaderUserTemplate(
	t *testing.T,
) {
	result, err := Render(
		LegacyLocale,
		PromptHeaderUser,
		struct {
			ProjectName      string
			RepoRootSlash    string
			HasCurrentHeader bool
			CurrentHeader    string
			TotalFiles       int
			Dirs             []struct {
				Dir   string
				Count int
			}
			Exts        []struct{}
			SampleFiles []string
		}{
			ProjectName:      "demo",
			RepoRootSlash:    "/repo",
			HasCurrentHeader: true,
			CurrentHeader:    "# header\n",
			TotalFiles:       1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	for _, anchor := range []string{
		"项目名: demo",
		"仓库根(目录段头的绝对路径基准): /repo",
		"# header",
		"文件总数: 1",
	} {
		if !strings.Contains(
			result,
			anchor,
		) {
			t.Fatalf(
				"rendered Header user template is missing %q:\n%s",
				anchor,
				result,
			)
		}
	}
}

func TestRenderRejectsUnknownAsset(
	t *testing.T,
) {
	if _, err := Render(
		LegacyLocale,
		ID("prompts/not-found"),
		struct{}{},
	); err == nil {
		t.Fatal(
			"rendering an unknown asset must fail",
		)
	}
}
