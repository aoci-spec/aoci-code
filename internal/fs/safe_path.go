// Relative-path safety validation: the first line of defence for agent input
// Index entry: safe_path.go[FS.Paths.9.S]
//
// Threat model: the MCP path argument comes from the agent, and the agent's input may be tainted by
// repository content (instructions injected through a README or a code comment); a ../ escape or an
// absolute path could land a write outside the repository. The scar of validatePathInWorkDir in the platform's agent_tools.go is transplanted here.
//
// Discipline: every path entry point of index queries, writes, and hooks passes through NormalizeRelPath first, without exception.
package fs

import (
	"errors"
	"path"
	"strings"
)

// NormalizeRelPath normalizes and validates a repository-relative path.
// Rules: backslashes become forward slashes, then path.Clean, then reject empty, absolute, drive-letter, and .. escaping paths, and a path empty after cleaning.
// Returns the clean forward-slash relative path; a directory path keeps its trailing slash.
// The refusals are sentinel errors whose text is a machine token, never a
// sentence in one language: the hook, the MCP tools, and the CLI render the
// reason through the UI catalog by RelPathMessageKey, so an en-US host no
// longer sees a Chinese clause inside an English line and a zh-CN host keeps
// the wording it had.
var (
	ErrRelPathEmpty    = errors.New("path_empty")
	ErrRelPathAbsolute = errors.New("path_absolute_rejected")
	ErrRelPathDrive    = errors.New("path_drive_letter_rejected")
	ErrRelPathUNC      = errors.New("path_unc_rejected")
	ErrRelPathNoTarget = errors.New("path_without_target")
	ErrRelPathEscapes  = errors.New("path_escapes_repository")
)

// RelPathMessageKey names the UI catalog message that explains a
// NormalizeRelPath refusal; false for any other error.
func RelPathMessageKey(err error) (string, bool) {
	switch {
	case errors.Is(err, ErrRelPathEmpty):
		return "path.rejected.empty", true
	case errors.Is(err, ErrRelPathAbsolute):
		return "path.rejected.absolute", true
	case errors.Is(err, ErrRelPathDrive):
		return "path.rejected.drive", true
	case errors.Is(err, ErrRelPathUNC):
		return "path.rejected.unc", true
	case errors.Is(err, ErrRelPathNoTarget):
		return "path.rejected.no_target", true
	case errors.Is(err, ErrRelPathEscapes):
		return "path.rejected.escape", true
	}
	return "", false
}

func NormalizeRelPath(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", ErrRelPathEmpty
	}
	p := strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")

	// Reject absolute paths (UNC, a POSIX root, a Windows drive letter); UNC is judged first, otherwise "//"
	// always lands in the absolute-path branch and the UNC reason is never reachable.
	if strings.HasPrefix(p, "//") {
		return "", ErrRelPathUNC
	}
	if strings.HasPrefix(p, "/") {
		return "", ErrRelPathAbsolute
	}
	if len(p) >= 2 && p[1] == ':' {
		return "", ErrRelPathDrive
	}

	// Remember the trailing slash (directory-entry semantics); Clean strips it
	trailingSlash := strings.HasSuffix(p, "/")

	clean := path.Clean(p)
	if clean == "." || clean == "" {
		return "", ErrRelPathNoTarget
	}
	// A .. escape: still starting with .. after Clean means out of bounds
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", ErrRelPathEscapes
	}
	// Clean can expose a drive spelling the raw check did not see: "./C:/x"
	// cleans to "C:/x", which is absolute on Windows and refused when given
	// directly. A rooted "/" cannot appear here, since Clean keeps a relative
	// path relative.
	if len(clean) >= 2 && clean[1] == ':' {
		return "", ErrRelPathDrive
	}

	if trailingSlash {
		clean += "/"
	}
	return clean, nil
}
