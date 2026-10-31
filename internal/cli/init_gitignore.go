// The in-repository Git asset boundary of .aoci that aoci init uses.
//
// Design principles:
//
//   - an unknown asset in .aoci is treated as a local runtime artifact and ignored by default;
//   - only formal cognition and governance assets on the explicit allowlist enter Git;
//   - .gitignore itself must be trackable;
//   - an existing user file is never overwritten;
//   - a new file must go through fs.AtomicWrite, so there is no half-written file.
package cli

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/textassets"
)

// aociRuntimeGitignoreContent defines the default-deny, formal-asset allowlist of the .aoci directory.
//
// aoci.txt lives at the repository root and is not managed by this file.
// reports.jsonl, ledger.jsonl, drafts, verify_history, lock, hooks, tmp,
// backup files, and unknown future files are all ignored by default through the leading * line.
// gitattributesContent is the line-ending normalization AOCI gives a new repository, the same line this repository uses itself.
//
// AOCI builds its Baseline on raw bytes, while core.autocrlf=true is the default of Git for Windows:
// one Windows checkout rewrites the line endings of the whole tree and judges the three formal cognition volumes as drift with it. This repository
// has long protected itself with this line without handing the same protection to user repositories.
const gitattributesContent = "" +
	"# AOCI baselines exact raw bytes, so a checkout must be byte-identical on\n" +
	"# every platform: text always checks out LF, overriding any autocrlf rewrite\n" +
	"# (the Windows default) that would otherwise mark the whole tree stale.\n" +
	"# Binaries are auto-detected and left untouched.\n" +
	"* text=auto eol=lf\n"

// ensureGitattributes writes the line-ending normalization when the repository root has no .gitattributes yet.
//
// An existing file is skipped unconditionally. A maintainer's file is never rewritten, a discipline above this protection: a repository with its own
// .gitattributes has its own rules, and AOCI has no right to infer which one should give way. A missing protection
// is no longer fatal: a line-ending difference is now a readable non-blocking finding, not a hard block.
func ensureGitattributes(root string) (string, error) {
	targetPath := filepath.Join(root, ".gitattributes")
	if _, err := os.Lstat(targetPath); err == nil {
		return "", nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", errors.New(cliMessage("init.gitattributes_error", err))
	}
	if err := fs.AtomicWrite(targetPath, []byte(gitattributesContent)); err != nil {
		return "", errors.New(cliMessage("init.gitattributes_error", err))
	}
	return cliMessage("init.gitattributes_written"), nil
}

// ensureAOCIRuntimeGitignore makes sure .aoci/.gitignore is in place at first initialization.
//
// An existing file is skipped unconditionally, so a maintainer's custom policy is not overwritten.
func ensureAOCIRuntimeGitignore(
	root string,
) (
	string,
	error,
) {
	targetPath := filepath.Join(
		root,
		config.DirName,
		".gitignore",
	)

	if _, err := os.Stat(targetPath); err == nil {
		return cliMessage("init.gitignore_exists"), nil
	} else if !errors.Is(
		err,
		os.ErrNotExist,
	) {
		return "", errors.New(cliMessage("init.gitignore_stat_error", err))
	}

	if err := os.MkdirAll(
		filepath.Dir(targetPath),
		0o755,
	); err != nil {
		return "", errors.New(cliMessage("init.gitignore_mkdir_error", err))
	}
	content, err := textassets.Load(textassets.ActiveLocale(), textassets.TemplateAOCIGitignore)
	if err != nil {
		return "", err
	}

	if err := fs.AtomicWrite(
		targetPath,
		[]byte(content),
	); err != nil {
		return "", errors.New(cliMessage("init.gitignore_create_error", err))
	}

	return cliMessage("init.gitignore_created"), nil
}

// hostConfigIgnoreMarker marks the root .gitignore block init manages itself. Other lines the maintainer wrote are
// never read or rewritten; a second init only adds new paths under the same marker.
const hostConfigIgnoreMarker = "# AOCI agent host integration (machine-bound; do not commit)"

// ensureHostConfigGitignore adds the host configuration init just wrote to the repository root .gitignore.
//
// Why it must happen before the first scan: these files carry local absolute paths; once committed, someone else's init judges only
// by whether the key exists, returns early as idempotent, and silently spins. And the roles of the Managed Scope are fixed at the first scan,
// after which --force cannot advance them, and removal goes through the coverage-reduction Scope Change approval; so missing this
// moment costs one human approval, not one changed line of configuration.
//
// Boundary: only paths that really exist this time are appended; a path already tracked by Git is always skipped, since that means the maintainer
// has decided to commit it and init does not change their mind; an equivalent entry already in the root .gitignore is not written twice.
func ensureHostConfigGitignore(
	root string,
	candidatePaths []string,
) (
	string,
	error,
) {
	present := make([]string, 0, len(candidatePaths))
	for _, relativePath := range candidatePaths {
		if _, err := os.Lstat(filepath.Join(root, relativePath)); err != nil {
			continue
		}
		present = append(present, relativePath)
	}
	if len(present) == 0 {
		return "", nil
	}

	tracked, err := trackedRepositoryPaths(root)
	if err != nil {
		// When the Git facts are unavailable, do not guess: better not to write than to change the maintainer's commit intent for them.
		return "", nil
	}

	targetPath := filepath.Join(root, ".gitignore")
	existing, readErr := os.ReadFile(targetPath)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return "", errors.New(cliMessage("init.host_gitignore_read_error", readErr))
	}
	current := string(existing)

	added := make([]string, 0, len(present))
	for _, relativePath := range present {
		if tracked[relativePath] {
			continue
		}
		if gitignoreAlreadyCovers(current, relativePath) {
			continue
		}
		added = append(added, relativePath)
		current = appendGitignoreLine(current, relativePath)

		// The installers back a host config up before rewriting it, and the
		// backup carries a timestamp, so it cannot be listed as an exact path.
		// Left uncovered it is the one machine-bound artifact that reaches a
		// commit: the config itself is ignored, its backup is not, and the
		// backup contains the same absolute host paths.
		backups := relativePath + ".backup.*"
		if !gitignoreAlreadyCovers(current, backups) {
			current = appendGitignoreLine(current, backups)
		}
	}
	if len(added) == 0 {
		return "", nil
	}

	if err := fs.AtomicWrite(targetPath, []byte(current)); err != nil {
		return "", errors.New(cliMessage("init.host_gitignore_write_error", err))
	}
	return cliMessage("init.host_gitignore_updated", strings.Join(added, ", ")), nil
}

// trackedRepositoryPaths reads the set of paths Git currently tracks.
func trackedRepositoryPaths(root string) (map[string]bool, error) {
	command := fs.UntrustedRepositoryGitCommand(
		root, "-c", "core.quotepath=false", "ls-files", "-z", "--cached",
	)
	output, err := command.Output()
	if err != nil {
		return nil, err
	}
	tracked := map[string]bool{}
	for _, entry := range strings.Split(string(output), "\x00") {
		if entry != "" {
			tracked[entry] = true
		}
	}
	return tracked, nil
}

// gitignoreAlreadyCovers judges whether the existing content already wrote this path.
//
// This is a conservative line-by-line equality comparison that does not interpret gitignore pattern semantics: better to miss and produce one
// duplicate entry than to misjudge as covered and let a machine-bound configuration enter the Managed Scope.
func gitignoreAlreadyCovers(content, relativePath string) bool {
	directoryForm := relativePath + "/"
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		trimmed = strings.TrimPrefix(trimmed, "/")
		switch trimmed {
		case relativePath, directoryForm:
			return true
		}
		if directory := path.Dir(relativePath); directory != "." {
			if trimmed == directory || trimmed == directory+"/" {
				return true
			}
		}
	}
	return false
}

// appendGitignoreLine appends one path inside the init marker block.
func appendGitignoreLine(content, relativePath string) string {
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if !strings.Contains(content, hostConfigIgnoreMarker) {
		if content != "" {
			content += "\n"
		}
		content += hostConfigIgnoreMarker + "\n"
		return content + relativePath + "\n"
	}
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	for index, line := range lines {
		if strings.TrimSpace(line) != hostConfigIgnoreMarker {
			continue
		}
		insertAt := index + 1
		for insertAt < len(lines) && strings.TrimSpace(lines[insertAt]) != "" {
			insertAt++
		}
		rebuilt := append([]string{}, lines[:insertAt]...)
		rebuilt = append(rebuilt, relativePath)
		rebuilt = append(rebuilt, lines[insertAt:]...)
		return strings.Join(rebuilt, "\n") + "\n"
	}
	return content + relativePath + "\n"
}
