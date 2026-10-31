// Path criterion for E-scale participation.
//
// E scale describes the implementation size of a static source or text entity, not the length of AOCI's own governance assets.
//
// Two kinds of AOCI governance asset never take part in the E-scale judgement:
//
//   - the Ledger, Draft, lock, configuration, and audit assets inside the repository root's .aoci directory;
//   - the aoci.txt index body at the repository root.
//
// The line count of a .aoci asset reflects the length of governance history; the line count of aoci.txt grows naturally with the entry count.
// If the index body took part in the E-band check, it would form the self-referential loop "update the index → the index crosses a band → update the index again",
// so the shared path criterion must exclude it in one place.
//
// This file is the single source of truth for path participation. Score, single-entry write-back, atomic batch write-back, and
// Entries Check must not each copy their own ".aoci/" or "aoci.txt" judgement.
package index

import (
	"path/filepath"
	"strings"
)

// ShouldCheckEScalePath reports whether a repository-relative path should take part in E-scale validation.
//
// The input has usually been normalized as a safe relative path already; this function still accepts both slash directions and several "./" layers,
// so a Linux process judges a Windows-shaped string the same way.
//
// Returns false for:
//   - an empty path or the current directory;
//   - the repository root's .aoci directory itself and any path below it;
//   - the aoci.txt index body at the repository root.
//
// Returns true for:
//   - same-prefix directories such as .aoci2;
//   - same-named non-root directories such as nested/.aoci;
//   - same-named non-root files such as nested/aoci.txt;
//   - similar names such as aoci.txt.backup;
//   - ordinary repository files.
func ShouldCheckEScalePath(
	rel string,
) bool {
	normalized := normalizeEScalePath(
		rel,
	)

	if normalized == "" ||
		normalized == "." {
		return false
	}

	if normalized == "aoci.txt" {
		return false
	}

	return normalized != ".aoci" &&
		!strings.HasPrefix(
			normalized,
			".aoci/",
		)
}

// normalizeEScalePath normalizes a path to the repository-relative forward-slash form.
//
// filepath.ToSlash converts only the Separator the current operating system defines; on Linux a backslash is an ordinary
// character, so it must still be replaced explicitly for a Windows-shaped string to be judged the same way in Linux tests.
func normalizeEScalePath(
	rel string,
) string {
	normalized := strings.TrimSpace(
		filepath.ToSlash(rel),
	)

	normalized = strings.ReplaceAll(
		normalized,
		"\\",
		"/",
	)

	for strings.HasPrefix(
		normalized,
		"./",
	) {
		normalized = strings.TrimPrefix(
			normalized,
			"./",
		)
	}

	return strings.TrimRight(
		normalized,
		"/",
	)
}
