package machinecontract_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestEveryGoldenHasDirectTestConsumer(t *testing.T) {
	root := repositoryRoot(t)
	testSources := strings.Builder{}
	for _, directory := range []string{"internal", "textassets"} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			testSources.Write(data)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	entries, err := os.ReadDir(filepath.Join(root, "testdata", "golden"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "README.txt" {
			continue
		}
		if !strings.Contains(testSources.String(), entry.Name()) {
			t.Errorf("孤立Golden没有测试消费者: %s", entry.Name())
		}
	}
}

func TestPrivateArchivesAreAbsentFromPublicTree(t *testing.T) {
	root := repositoryRoot(t)
	for _, denied := range []string{"experiments", "research", filepath.Join("spec", "private")} {
		entries, err := os.ReadDir(filepath.Join(root, denied))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("public tree contains non-exportable assets under %s", denied)
		}
	}

	for _, directory := range []string{"cmd", "internal", "textassets"} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || strings.HasSuffix(entry.Name(), "_test.go") {
				return nil
			}
			if filepath.Ext(entry.Name()) != ".go" && filepath.Ext(entry.Name()) != ".json" {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			text := filepath.ToSlash(string(data))
			if strings.Contains(text, "experiments/") || strings.Contains(text, "research/") {
				relative, _ := filepath.Rel(root, path)
				t.Errorf("production consumer references a private archive: %s", relative)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestNaturalLanguageTemplatesAreLocaleOwned(t *testing.T) {
	root := repositoryRoot(t)
	legacyRoot := filepath.Join(root, "templates")
	legacyEntries, err := os.ReadDir(legacyRoot)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("inspect legacy root templates directory: %v", err)
	}
	if len(legacyEntries) != 0 {
		names := make([]string, 0, len(legacyEntries))
		for _, entry := range legacyEntries {
			names = append(names, entry.Name())
		}
		t.Fatalf("legacy root templates directory must not own production assets: %v", names)
	}

	for _, locale := range []string{"en-US", "zh-CN"} {
		for _, name := range []string{
			"claude-pretool.sh.tmpl",
			"codex-cursor-stubs.txt.tmpl",
			"codex-mcp.toml.tmpl",
		} {
			path := filepath.Join(root, "textassets", locale, "templates", name)
			info, statErr := os.Stat(path)
			if statErr != nil {
				t.Errorf("Locale模板缺失%s: %v", path, statErr)
				continue
			}
			if !info.Mode().IsRegular() {
				t.Errorf("Locale模板不是普通文件: %s", path)
			}
		}
	}
}
