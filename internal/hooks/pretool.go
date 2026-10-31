// The PreToolUse runtime decision kernel, independent of any particular Agent.
// Index entry: pretool.go[Hook.Pretool.9.S]
//
// Semantics:
//   - the default mode never blocks; it only injects the current entry or a reminder to record one;
//   - a short warning is attached when the entry is really Stale;
//   - in strict mode, only a really Stale entry blocks the write;
//   - the drift quick check consumes the team line_ending_tolerance, and a pure CRLF/LF representation difference
//     produces no STALE warning, let alone a strict block;
//   - a path resolution failure, a missing index, or a configuration fault all pass through; a fault in the hook itself must not jam the workflow;
//   - the injected text comes from the current Locale's UI message catalog (hook.pretool.*), and a catalog fault passes through as well;
//   - a hit records the ledger op=hook_trigger.
//
// This kernel returns only text and the block decision; exit-code mapping belongs to internal/cli/hook.go and
// each Agent's adapter layer.
package hooks

import (
	"os"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/textassets"
)

// PretoolResult is the hook kernel's output.
type PretoolResult struct {
	// Text is the entry, warning, or explanation injected into the Agent; it may be empty.
	Text string

	// Block=true recommends blocking this write; it arises only under strict plus a real Stale.
	Block bool
}

// HandlePreTool handles one pre-write hook.
//
// tool is the Agent-side tool name, such as Edit, Write, or MultiEdit;
// rawPath is the target path the Agent is about to write.
func HandlePreTool(
	root string,
	tool string,
	rawPath string,
) PretoolResult {
	start := time.Now()

	// An illegal path is only explained and passed through.
	// The hook is not a safety gate and must not jam the user's workflow because its own argument parsing failed.
	relativePath, err := afs.NormalizeRelPath(
		rawPath,
	)
	if err != nil {
		text, ok := pretoolMessage(
			"hook.pretool.path_unresolved",
			pretoolRelPathReason(err),
		)
		if !ok {
			return PretoolResult{}
		}
		return PretoolResult{Text: text}
	}

	// Any environment load failure passes through silently.
	configValue, err := config.LoadReadOnly(root)
	if err != nil {
		return PretoolResult{}
	}

	paths := config.AOCIPaths(
		root,
		configValue.IndexPath,
	)

	document := hookDocument(root, configValue.IndexPath, paths.IndexPath)
	if document == nil || len(document.Sections) == 0 {
		return PretoolResult{}
	}

	baselineValue, _, _ := baseline.Load(root)

	result := PretoolResult{}

	entry := index.FindEntry(
		document,
		relativePath,
	)

	stale := false

	if entry == nil {
		text, ok := pretoolMessage(
			"hook.pretool.not_indexed",
			relativePath,
		)
		if !ok {
			return PretoolResult{}
		}
		result.Text = text
	} else {
		// The judgement is tolerant while write safety is unchanged:
		// this place only decides whether to hint or block, and takes no part in the CAS or the Stage source digest binding.
		currentStale, unbaselined, _ :=
			baseline.IsStaleFileWith(
				root,
				relativePath,
				baselineValue,
				configValue.LineEndingTolerance,
			)

		stale = currentStale

		headerKey := "hook.pretool.entry_header"

		if stale {
			headerKey = "hook.pretool.stale_header"
		} else if unbaselined {
			headerKey = "hook.pretool.unbaselined_header"
		}

		header, ok := pretoolMessage(headerKey)
		if !ok {
			return PretoolResult{}
		}

		result.Text = header +
			entry.FullLine

		if stale &&
			configValue.HookStrict {
			suffix, ok := pretoolMessage(
				"hook.pretool.strict_blocked",
			)
			if !ok {
				return PretoolResult{}
			}
			result.Block = true
			result.Text += suffix
		}
	}

	ledger.Append(
		root,
		configValue.LedgerEnabled,
		ledger.Event{
			Op:          "hook_trigger",
			PathsCount:  1,
			TagFilter:   tool,
			DurationMs:  time.Since(start).Milliseconds(),
			DriftWarned: stale,
			Source:      ledger.SourceAgent,
		},
	)

	return result
}

// hookDocument resolves the Entry document the hook reads. Under Volumes v1
// aoci.txt is a Root manifest with no Entry sections; the Entries live in the
// Code Volume, so the hook loads the layout read-only and takes that Volume's
// document, already resolved against the root. A Legacy index keeps the direct
// parse. Any load failure returns nil and the hook stays fail-open (#85).
func hookDocument(root, configuredIndexPath, resolvedIndexPath string) *index.Document {
	if set, err := cognition.Load(root, configuredIndexPath); err == nil && set != nil {
		if set.LayoutMode == cognition.LayoutVolumesV1 {
			code := set.Volumes[cognition.ScopeCode]
			if code == nil || code.Document == nil {
				return nil
			}
			return code.Document
		}
		if set.Root.Document != nil {
			return set.Root.Document
		}
	}
	indexData, err := os.ReadFile(resolvedIndexPath)
	if err != nil {
		return nil
	}
	document, _ := index.Parse(string(indexData))
	if len(document.Sections) == 0 {
		return nil
	}
	index.ResolveRelPaths(document, root)
	return document
}

// pretoolMessage renders one hook text from the UI message catalog of the
// active locale, so an en-US repository is not handed Chinese prose. The hook
// is fail-open by contract: a catalog failure (unknown key, argument mismatch,
// damaged bundle) reports ok=false, and HandlePreTool then injects nothing and
// blocks nothing, exactly as it treats its other environment failures.
// pretoolRelPathReason localizes a NormalizeRelPath refusal; an unknown
// error keeps its own text.
func pretoolRelPathReason(err error) string {
	var text string
	var ok bool
	// Literal keys: the locale consumer contract scans them statically.
	switch key, _ := afs.RelPathMessageKey(err); key {
	case "path.rejected.empty":
		text, ok = pretoolMessage("path.rejected.empty")
	case "path.rejected.absolute":
		text, ok = pretoolMessage("path.rejected.absolute")
	case "path.rejected.drive":
		text, ok = pretoolMessage("path.rejected.drive")
	case "path.rejected.unc":
		text, ok = pretoolMessage("path.rejected.unc")
	case "path.rejected.no_target":
		text, ok = pretoolMessage("path.rejected.no_target")
	case "path.rejected.escape":
		text, ok = pretoolMessage("path.rejected.escape")
	}
	if ok {
		return text
	}
	return err.Error()
}

func pretoolMessage(key string, args ...any) (string, bool) {
	text, err := textassets.Message(textassets.ActiveLocale(), key, args...)
	if err != nil {
		return "", false
	}
	return text, true
}
