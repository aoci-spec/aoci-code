package fs

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	gitObjectIDRe = regexp.MustCompile(`^[0-9a-f]{40}$|^[0-9a-f]{64}$`)
	gitRefNameRe  = regexp.MustCompile(`^refs/[A-Za-z0-9._/@+-]+$`)
)

const gitSmallFileLimit = 64 << 10

// GitHeadFast resolves HEAD of the repository at root from the files git
// keeps, without a git process: the detached commit in HEAD, or the ref HEAD
// names, loose under the common directory or listed in packed-refs, through a
// worktree's gitdir and commondir. It answers false for every shape it does
// not fully understand (no .git, a symlinked .git, an unborn branch, the
// reftable backend, a malformed or oversized file), and the caller then asks
// git itself; it never guesses. Every governance call used to spend one git
// process on this answer, which on Windows cost more than the work it
// qualified (#127).
func GitHeadFast(root string) (string, bool) {
	gitPath := filepath.Join(root, ".git")
	info, err := os.Lstat(gitPath)
	if err != nil {
		return "", false
	}
	gitDir := gitPath
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return "", false
	case info.Mode().IsRegular():
		data, ok := readGitSmallFile(gitPath)
		if !ok || !strings.HasPrefix(data, "gitdir:") {
			return "", false
		}
		gitDir = strings.TrimSpace(strings.TrimPrefix(data, "gitdir:"))
		if gitDir == "" {
			return "", false
		}
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(root, gitDir)
		}
		if dirInfo, dirErr := os.Lstat(gitDir); dirErr != nil || !dirInfo.IsDir() {
			return "", false
		}
	case !info.IsDir():
		return "", false
	}
	commonDir := gitDir
	if data, ok := readGitSmallFile(filepath.Join(gitDir, "commondir")); ok {
		common := strings.TrimSpace(data)
		if common == "" {
			return "", false
		}
		if !filepath.IsAbs(common) {
			common = filepath.Join(gitDir, common)
		}
		commonDir = common
	}
	head, ok := readGitSmallFile(filepath.Join(gitDir, "HEAD"))
	if !ok {
		return "", false
	}
	head = strings.TrimSpace(head)
	if gitObjectIDRe.MatchString(head) {
		return head, true
	}
	if !strings.HasPrefix(head, "ref:") {
		return "", false
	}
	ref := strings.TrimSpace(strings.TrimPrefix(head, "ref:"))
	if !gitRefNameRe.MatchString(ref) || strings.Contains(ref, "..") || strings.Contains(ref, "//") {
		return "", false
	}
	if loose, looseOK := readGitSmallFile(filepath.Join(commonDir, filepath.FromSlash(ref))); looseOK {
		value := strings.TrimSpace(loose)
		if gitObjectIDRe.MatchString(value) {
			return value, true
		}
		return "", false
	}
	packed, packedOK := readGitSmallFile(filepath.Join(commonDir, "packed-refs"))
	if !packedOK {
		return "", false
	}
	for _, line := range strings.Split(packed, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || line[0] == '#' || line[0] == '^' {
			continue
		}
		space := strings.IndexByte(line, ' ')
		if space < 0 {
			continue
		}
		if line[space+1:] == ref {
			value := line[:space]
			if gitObjectIDRe.MatchString(value) {
				return value, true
			}
			return "", false
		}
	}
	return "", false
}

// readGitSmallFile reads a regular, bounded git metadata file; anything else
// (absent, a symlink, a directory, too large) is not understood.
func readGitSmallFile(path string) (string, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > gitSmallFileLimit {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}
