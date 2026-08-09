package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var markdownDestinationPattern = regexp.MustCompile(`!?\[[^][]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)

func TestArchiveNames(t *testing.T) {
	dir := t.TempDir()
	tarPath := filepath.Join(dir, "aoci_test_linux_amd64.tar.gz")
	zipPath := filepath.Join(dir, "aoci_test_windows_amd64.zip")
	writeTar(t, tarPath, archiveNames("aoci"))
	writeZip(t, zipPath, archiveNames("aoci.exe"))
	tarEntries, err := tarNames(tarPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := requireNames(tarPath, tarEntries, archiveNames("aoci")); err != nil {
		t.Fatal(err)
	}
	zipEntries, err := zipNames(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := requireNames(zipPath, zipEntries, archiveNames("aoci.exe")); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveREADMERelativeLinksResolveInsideArchive(t *testing.T) {
	archiveFiles := make(map[string]struct{}, len(archiveDocuments))
	for _, name := range archiveDocuments {
		archiveFiles[name] = struct{}{}
	}
	repositoryRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	for _, readme := range []string{"README.md", "README.zh-CN.md"} {
		t.Run(readme, func(t *testing.T) {
			if _, ok := archiveFiles[readme]; !ok {
				t.Fatalf("packaged README is absent from archiveDocuments: %s", readme)
			}
			raw, err := os.ReadFile(filepath.Join(repositoryRoot, readme))
			if err != nil {
				t.Fatal(err)
			}
			matches := markdownDestinationPattern.FindAllSubmatch(raw, -1)
			if len(matches) == 0 {
				t.Fatalf("packaged README has no Markdown links: %s", readme)
			}
			for _, match := range matches {
				destination := string(match[1])
				if strings.HasPrefix(destination, "http://") || strings.HasPrefix(destination, "https://") || strings.HasPrefix(destination, "#") {
					continue
				}
				if separator := strings.IndexAny(destination, "?#"); separator >= 0 {
					destination = destination[:separator]
				}
				cleaned := filepath.ToSlash(filepath.Clean(filepath.FromSlash(destination)))
				if _, ok := archiveFiles[cleaned]; !ok {
					t.Errorf("%s relative Markdown link points outside the archive: %s", readme, string(match[1]))
				}
			}
		})
	}
}

func TestArchiveNamesRejectsMissingLegalAsset(t *testing.T) {
	legalAssets := []string{"LICENSE", "NOTICE", "PATENTS", "TRADEMARKS", "THIRD-PARTY-NOTICES"}
	for _, archiveType := range []struct {
		name   string
		binary string
	}{
		{name: "tar.gz", binary: "aoci"},
		{name: "zip", binary: "aoci.exe"},
	} {
		for _, missing := range legalAssets {
			t.Run(archiveType.name+"/missing_"+missing, func(t *testing.T) {
				dir := t.TempDir()
				expected := archiveNames(archiveType.binary)
				actual := make([]string, 0, len(expected)-1)
				for _, name := range expected {
					if name != missing {
						actual = append(actual, name)
					}
				}
				if len(actual) != len(expected)-1 {
					t.Fatalf("required legal asset %s is absent from the expected archive contents", missing)
				}
				switch archiveType.name {
				case "tar.gz":
					writeTar(t, filepath.Join(dir, "aoci_test_linux_amd64.tar.gz"), actual)
				case "zip":
					writeZip(t, filepath.Join(dir, "aoci_test_windows_amd64.zip"), actual)
				}
				err := smoke(dir)
				if err == nil || !strings.Contains(err.Error(), "unexpected archive contents") {
					t.Fatalf("expected archive missing %s to fail content validation, got %v", missing, err)
				}
			})
		}
	}
}

func TestArchiveNamesRejectsUnexpectedEntry(t *testing.T) {
	if err := requireNames("fixture", []string{"aoci", "../escape"}, []string{"aoci"}); err == nil {
		t.Fatal("expected unexpected archive entry to fail")
	}
}

func writeTar(t *testing.T, path string, names []string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	writer := tar.NewWriter(gzipWriter)
	for _, name := range names {
		content := []byte(name)
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeZip(t *testing.T, path string, names []string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, name := range names {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
