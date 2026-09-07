package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/hooks"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/mcptools"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
	"github.com/aoci-spec/aoci-code/textassets"
)

// GuideFunc assembles the Volumes Guide for one repository. The CLI owns the
// Guide builder; injecting it here keeps ui free of an import cycle and keeps
// the page reading the same Guide the CLI prints.
type GuideFunc func(root string, cfg *config.Config, set *cognition.Set) (any, error)

// Options configure one page process.
type Options struct {
	Roots         []string
	Discover      bool
	Host          string
	Port          int
	Locale        string
	BinaryVersion string
	Guide         GuideFunc
	RegistryDir   string
}

// AssetInfo describes one formal cognition asset as the page shows it.
type AssetInfo struct {
	Path    string `json:"path"`
	State   string `json:"state"`
	SHA256  string `json:"sha256,omitempty"`
	Bytes   int    `json:"bytes"`
	Lines   int    `json:"lines"`
	Objects int    `json:"objects"`
}

// Suggestion is one next step the page offers: a command to run, a prompt to
// send to an agent, or a note. Labels are localized server-side.
type Suggestion struct {
	Label   string `json:"label"`
	Command string `json:"command,omitempty"`
	Prompt  string `json:"prompt,omitempty"`
	Note    string `json:"note,omitempty"`
}

// Snapshot is the complete read-only picture of one repository.
type Snapshot struct {
	Root              string                      `json:"root"`
	Name              string                      `json:"name"`
	GeneratedAt       string                      `json:"generated_at"`
	BinaryVersion     string                      `json:"binary_version"`
	Layout            string                      `json:"layout,omitempty"`
	Error             string                      `json:"error,omitempty"`
	CompositeIdentity string                      `json:"composite_identity,omitempty"`
	Facts             *volumegovernance.Facts     `json:"facts,omitempty"`
	Assets            map[string]AssetInfo        `json:"assets,omitempty"`
	RootText          string                      `json:"root_text,omitempty"`
	MetaText          string                      `json:"meta_text,omitempty"`
	OverviewText      string                      `json:"-"`
	ChunkPlan         *mcptools.OverviewChunkPlan `json:"chunk_plan,omitempty"`
	Guide             any                         `json:"guide,omitempty"`
	Integrations      map[string]bool             `json:"integrations"`
	Running           []Instance                  `json:"running"`
	Coverage          *Coverage                   `json:"coverage,omitempty"`
	Suggestions       map[string][]Suggestion     `json:"suggestions"`
}

// buildSnapshot reads the repository exactly as the read-only CLI commands do:
// configuration, the cognition set, the shared governance facts, and the
// Guide. It takes no lock and writes nothing, so it may run beside a live MCP
// server at any time.
func buildSnapshot(root string, options Options, running []Instance) (Snapshot, []EntryView) {
	snapshot := Snapshot{Root: root, Name: filepath.Base(root), GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		BinaryVersion: options.BinaryVersion, Integrations: map[string]bool{}, Running: running, Suggestions: map[string][]Suggestion{}}
	snapshot.Integrations = map[string]bool{
		"claude_mcp": hooks.IsClaudeMCPInstalled(root), "claude_hook": hooks.IsClaudeHookInstalled(root),
		"codex_mcp": hooks.IsCodexMCPInstalled(root), "opencode_mcp": hooks.IsOpenCodeMCPInstalled(root),
		"agents_block": hooks.IsAgentsBlockPresent(root),
	}
	cfg, err := config.Load(root)
	if err != nil {
		snapshot.Error = "config_unavailable: " + err.Error()
		return snapshot, nil
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		snapshot.Error = "cognition_unavailable: " + err.Error()
		if set != nil {
			snapshot.Layout = set.LayoutMode
		}
		return snapshot, nil
	}
	snapshot.Layout = set.LayoutMode
	snapshot.CompositeIdentity = set.CompositeIdentity
	if set.LayoutMode != cognition.LayoutVolumesV1 {
		snapshot.Error = "layout_not_volumes_v1"
		return snapshot, nil
	}
	snapshot.RootText = string(set.Root.Raw)
	snapshot.MetaText = string(set.Meta.Raw)
	snapshot.Assets = map[string]AssetInfo{"root": assetInfo(set.Root), "meta": assetInfo(set.Meta)}
	for _, id := range []string{cognition.ScopeCode, cognition.ScopeDatabase} {
		if asset := set.Volumes[id]; asset != nil {
			snapshot.Assets[id] = assetInfo(*asset)
		}
	}
	facts, err := volumegovernance.Assess(root, cfg, set)
	if err != nil {
		snapshot.Error = "governance_unavailable: " + err.Error()
		return snapshot, entryViews(set)
	}
	snapshot.Facts = facts
	if plan, planErr := mcptools.PlanOverviewChunks(root, set, cognition.ScopeAll, cfg.OverviewDelivery.ChunkTokens); planErr == nil {
		snapshot.ChunkPlan = &plan
	}
	if body, bodyErr := mcptools.OverviewBody(set, cognition.ScopeAll); bodyErr == nil {
		snapshot.OverviewText = body
	}
	if options.Guide != nil {
		if guide, guideErr := options.Guide(root, cfg, set); guideErr == nil {
			snapshot.Guide = guide
		}
	}
	indexTokens := facts.Budget.WholeIndexTokens
	if snapshot.ChunkPlan != nil && snapshot.ChunkPlan.EstimatedTokens > 0 {
		indexTokens = snapshot.ChunkPlan.EstimatedTokens
	}
	snapshot.Coverage = measureCoverage(root, set, indexTokens)
	// Every official locale gets its own suggestion set so the page can switch
	// language without a round trip; the server's locale is only the default.
	snapshot.Suggestions = map[string][]Suggestion{}
	for _, locale := range pageLocales() {
		snapshot.Suggestions[locale] = suggestions(locale, facts, snapshot.Guide)
	}
	return snapshot, entryViews(set)
}

// pageLocales lists the locales the page ships. Every official locale carries
// the complete catalog, so a missing one would blank a live panel.
func pageLocales() []string {
	if manifest, err := textassets.ReadManifest(); err == nil && len(manifest.OfficialLocales) > 0 {
		return manifest.OfficialLocales
	}
	return []string{textassets.DefaultLocale}
}

// Coverage measures the source the Code Volume stands for: each Code object's
// file on disk, lines counted and tokens estimated the way the index itself is
// measured, so the ratio between the two compares like with like.
type Coverage struct {
	Files           int     `json:"files"`
	Lines           int     `json:"lines"`
	Bytes           int     `json:"bytes"`
	EstimatedTokens int     `json:"estimated_tokens"`
	Unreadable      int     `json:"unreadable"`
	IndexTokens     int     `json:"index_tokens"`
	Ratio           float64 `json:"ratio"`
}

// measureCoverage reads only: it stats and streams each indexed source. A file
// that cannot be read is counted as unreadable rather than guessed, so the
// published line count is never larger than what was actually measured.
func measureCoverage(root string, set *cognition.Set, indexTokens int) *Coverage {
	asset := set.Volumes[cognition.ScopeCode]
	if asset == nil {
		return nil
	}
	cov := &Coverage{IndexTokens: indexTokens}
	for _, object := range asset.Objects {
		full := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(object.CanonicalRef, "code:")))
		info, err := os.Stat(full)
		if err != nil || !info.Mode().IsRegular() {
			cov.Unreadable++
			continue
		}
		lines, err := fs.CountFileLines(full)
		if err != nil {
			cov.Unreadable++
			continue
		}
		cov.Files++
		cov.Lines += lines
		cov.Bytes += int(info.Size())
	}
	cov.EstimatedTokens = cognitionbudget.EstimateTokensOfSize(cov.Bytes)
	if indexTokens > 0 {
		cov.Ratio = float64(int(float64(cov.EstimatedTokens)/float64(indexTokens)*10+0.5)) / 10
	}
	return cov
}

func assetInfo(asset cognition.Asset) AssetInfo {
	info := AssetInfo{Path: asset.Descriptor.Path, State: asset.State, SHA256: asset.SHA256,
		Bytes: len(asset.Raw), Objects: asset.ObjectCount}
	if len(asset.Raw) > 0 {
		info.Lines = bytes.Count(asset.Raw, []byte{'\n'})
		if asset.Raw[len(asset.Raw)-1] != '\n' {
			info.Lines++
		}
	}
	return info
}

func message(locale, key string, args ...any) string {
	text, err := textassets.Message(locale, key, args...)
	if err != nil {
		return key
	}
	return text
}

// suggestions turns the governance result into the next step a user can
// take. Commands come from the Guide, which already carries the runtime-bound
// absolute spellings; prompts are the same three the README publishes.
func suggestions(locale string, facts *volumegovernance.Facts, guide any) []Suggestion {
	list := []Suggestion{}
	commands, stop, batch := guideProjection(guide)
	switch {
	case facts.RecoveryPending:
		list = append(list, Suggestion{Label: message(locale, "ui.suggest.recovery"), Note: stop["safe_next_action"]})
	case facts.Result == volumegovernance.ResultBlocked:
		for _, key := range []string{"scan", "scope_status", "scope_acknowledge", "scope_budget"} {
			if command := commands[key]; command != "" {
				list = append(list, Suggestion{Label: message(locale, "ui.suggest.blocked"), Command: command})
			}
		}
		if stop["safe_next_action"] != "" {
			list = append(list, Suggestion{Label: message(locale, "ui.suggest.blocked"), Note: stop["safe_next_action"]})
		}
	case facts.Result == volumegovernance.ResultEvidenceRequired:
		for _, source := range facts.DatabaseCognition.Sources {
			for _, suffix := range []string{machinecontract.RemediationCommandDatabaseSnapshot, machinecontract.RemediationCommandDatabaseBaselineAccept} {
				list = append(list, Suggestion{Label: message(locale, "ui.suggest.evidence"),
					Command: "aoci " + strings.ReplaceAll(suffix, "{source}", source.SourceID)})
			}
		}
	case facts.Result == volumegovernance.ResultAuthoringRequired:
		note := ""
		if batch != nil {
			note = message(locale, "ui.suggest.batch", batch.Included, batch.TotalTargets, batch.Remaining)
		}
		list = append(list, Suggestion{Label: message(locale, "ui.suggest.build_index"), Prompt: message(locale, "ui.prompt.build_index"), Note: note})
	}
	if facts.DatabaseCognition.ConfiguredSources == 0 {
		list = append(list, Suggestion{Label: message(locale, "ui.suggest.database"), Prompt: message(locale, "ui.prompt.database")})
	}
	list = append(list, Suggestion{Label: message(locale, "ui.suggest.recall"), Prompt: message(locale, "ui.prompt.recall")})
	return list
}

type guideBatch struct {
	TotalTargets int `json:"total_targets"`
	Included     int `json:"included"`
	Remaining    int `json:"remaining"`
}

// guideProjection reads the few Guide fields the page acts on through JSON,
// so ui depends on the Guide's public shape rather than on the CLI's types.
func guideProjection(guide any) (commands map[string]string, stop map[string]string, batch *guideBatch) {
	commands, stop = map[string]string{}, map[string]string{}
	if guide == nil {
		return commands, stop, nil
	}
	raw, err := json.Marshal(guide)
	if err != nil {
		return commands, stop, nil
	}
	var projected struct {
		Commands       map[string]string `json:"commands"`
		Stop           map[string]string `json:"stop"`
		AuthoringBatch *guideBatch       `json:"authoring_batch"`
	}
	if err := json.Unmarshal(raw, &projected); err != nil {
		return commands, stop, nil
	}
	if projected.Commands != nil {
		commands = projected.Commands
	}
	if projected.Stop != nil {
		stop = projected.Stop
	}
	return commands, stop, projected.AuthoringBatch
}

// String renders a one-line summary for logs and tests.
func (s Snapshot) String() string {
	if s.Error != "" {
		return fmt.Sprintf("%s: %s", s.Root, s.Error)
	}
	return fmt.Sprintf("%s: %s aligned=%v", s.Root, s.Facts.Result, s.Facts.GovernanceAligned)
}

// entryViews projects every Code and Database object into the table shape.
func entryViews(set *cognition.Set) []EntryView {
	views := []EntryView{}
	for _, id := range set.DeclaredOrder {
		asset := set.Volumes[id]
		if asset == nil {
			continue
		}
		for _, object := range asset.Objects {
			view := EntryView{Path: strings.TrimPrefix(object.CanonicalRef, "code:"), Kind: object.Kind, Section: object.SourceSection}
			if entry := object.Entry; entry != nil {
				view.Tag, view.F, view.R, view.A, view.S = entry.TagsRaw, entry.F, entry.R, entry.Api, entry.S
				view.C, view.E = entry.TagsParsed["C"], entry.TagsParsed["E"]
			}
			views = append(views, view)
		}
	}
	return views
}
