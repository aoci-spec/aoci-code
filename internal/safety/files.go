package safety

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var publicTextExtensions = map[string]struct{}{
	".json": {},
	".md":   {},
	".toml": {},
	".tmpl": {},
	".txt":  {},
	".yaml": {},
	".yml":  {},
}

// PublicTextFiles returns the files covered by the public-text safety gate.
// Explicit paths preserve the script's historical behavior: missing files,
// backups, and files under the non-exportable spec/private boundary are skipped.
func PublicTextFiles(root string, explicit []string) ([]string, error) {
	files := map[string]struct{}{}
	appendFile := func(candidate string) {
		info, err := os.Stat(candidate)
		if err != nil || !info.Mode().IsRegular() || excludedPublicTextFile(candidate) {
			return
		}
		files[filepath.Clean(candidate)] = struct{}{}
	}

	if len(explicit) > 0 {
		for _, candidate := range explicit {
			appendFile(candidate)
		}
		return sortedPublicTextFiles(files), nil
	}

	for _, pattern := range []string{
		filepath.Join(root, "README.md"),
		filepath.Join(root, "README.*.md"),
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		for _, candidate := range matches {
			appendFile(candidate)
		}
	}

	for _, relativeRoot := range []string{"docs", "prompts", "textassets", "spec"} {
		if err := walkPublicTextRoot(root, relativeRoot, func(candidate string, entry fs.DirEntry) {
			if _, ok := publicTextExtensions[strings.ToLower(filepath.Ext(entry.Name()))]; ok {
				appendFile(candidate)
			}
		}); err != nil {
			return nil, err
		}
	}

	for _, current := range []struct {
		directory string
		extension string
	}{
		{directory: "templates", extension: ".tmpl"},
		{directory: filepath.Join("internal", "mcptools"), extension: ".go"},
	} {
		entries, err := os.ReadDir(filepath.Join(root, current.directory))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), current.extension) {
				appendFile(filepath.Join(root, current.directory, entry.Name()))
			}
		}
	}

	return sortedPublicTextFiles(files), nil
}

func walkPublicTextRoot(
	root,
	relativeRoot string,
	visit func(string, fs.DirEntry),
) error {
	directory := filepath.Join(root, relativeRoot)
	if _, err := os.Stat(directory); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	return filepath.WalkDir(directory, func(candidate string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			visit(candidate, entry)
		}
		return nil
	})
}

func excludedPublicTextFile(candidate string) bool {
	clean := filepath.ToSlash(filepath.Clean(candidate))
	return strings.Contains(filepath.Base(clean), ".backup.") ||
		strings.Contains("/"+clean+"/", "/spec/private/")
}

func sortedPublicTextFiles(files map[string]struct{}) []string {
	result := make([]string, 0, len(files))
	for candidate := range files {
		result = append(result, candidate)
	}
	sort.Strings(result)
	return result
}
