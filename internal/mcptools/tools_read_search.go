// AOCI索引局部检索与路径名辅助。
package mcptools

import (
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

func handleSearch(root, mcpServiceVersion string, input searchIn, refreshSession *cognitionRefreshSession) *mcp.CallToolResult {
	start := time.Now()
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		return failResult(fail)
	}
	if deliveryFail := pendingCognitionDeliveryFail(root, loaded.set); deliveryFail != nil {
		return failResult(deliveryFail)
	}
	if loaded.set.LayoutMode == cognition.LayoutVolumesV1 {
		return handleVolumeSearch(root, mcpServiceVersion, input, loaded, refreshSession, start)
	}
	repository := loaded.legacyRepo()
	matched, skipped, err := index.Search(repository.doc, input.Keyword, input.TagFilter)
	if err != nil {
		return errResult(
			errBadArgs,
			localeSafeMCPDetail(err.Error()),
			mcpMessage("mcp.search.bad_query_hint"),
		)
	}
	var builder strings.Builder
	shown := matched
	if len(shown) > maxSearchResults {
		shown = shown[:maxSearchResults]
	}
	for _, entry := range shown {
		builder.WriteString(entry.FullLine + "\n")
	}
	if len(matched) > maxSearchResults {
		builder.WriteString(mcpMessage("mcp.search.truncated", len(matched), maxSearchResults))
	}
	if skipped > 0 {
		builder.WriteString(mcpMessage("mcp.search.skipped_untagged", skipped))
	}
	if len(matched) == 0 {
		builder.WriteString(mcpMessage("mcp.search.no_matches"))
	}
	ledger.Append(root, repository.cfg.LedgerEnabled, ledger.Event{
		Op:            "search",
		PathsCount:    len(matched),
		TagFilter:     input.TagFilter,
		DurationMs:    time.Since(start).Milliseconds(),
		Source:        ledger.SourceAgent,
		AOCIToolCalls: 1,
		LocalRecalls:  1,
	})
	return textResult(mcpMessage("mcp.search.summary", len(matched)) + builder.String())
}

func handleVolumeSearch(root, mcpServiceVersion string, input searchIn, loaded *cognitionRepoCtx, refreshSession *cognitionRefreshSession, start time.Time) *mcp.CallToolResult {
	if _, _, err := index.Search(&index.Document{}, input.Keyword, input.TagFilter); err != nil {
		return errResult(errBadArgs, localeSafeMCPDetail(err.Error()), mcpMessage("mcp.search.bad_query_hint"))
	}
	view, err := loaded.set.Scope(input.Scope)
	if err != nil {
		return errResult(errBadArgs, mcpMessage("mcp.scope.invalid", input.Scope), "")
	}
	if !view.Available {
		ledger.Append(root, loaded.cfg.LedgerEnabled, ledger.Event{Op: "search", TagFilter: input.TagFilter, DurationMs: time.Since(start).Milliseconds(), Source: ledger.SourceAgent, AOCIToolCalls: 1, LocalRecalls: 1})
		return textResult("AOCI Search: scope_available=false asset_state=absent matches=0\n")
	}
	doc := &index.Document{Sections: []*index.Section{{AbsPath: "/volumes"}}}
	lookup := map[*index.Entry]cognition.Object{}
	for _, asset := range view.Assets {
		for _, object := range asset.Objects {
			doc.Sections[0].Entries = append(doc.Sections[0].Entries, object.Entry)
			lookup[object.Entry] = object
		}
	}
	matched, skipped, err := index.Search(doc, input.Keyword, input.TagFilter)
	if err != nil {
		return errResult(errBadArgs, localeSafeMCPDetail(err.Error()), mcpMessage("mcp.search.bad_query_hint"))
	}
	shown := matched
	if len(shown) > maxSearchResults {
		shown = shown[:maxSearchResults]
	}
	var builder strings.Builder
	for _, entry := range shown {
		object := lookup[entry]
		builder.WriteString("[volume_id=" + object.VolumeID + " object_ref=" + object.CanonicalRef + "] " + entry.FullLine + "\n")
	}
	if len(matched) > maxSearchResults {
		builder.WriteString(mcpMessage("mcp.search.truncated", len(matched), maxSearchResults))
	}
	if skipped > 0 {
		builder.WriteString(mcpMessage("mcp.search.skipped_untagged", skipped))
	}
	if len(matched) == 0 {
		builder.WriteString(mcpMessage("mcp.search.no_matches"))
	}
	ledger.Append(root, loaded.cfg.LedgerEnabled, ledger.Event{Op: "search", PathsCount: len(matched), TagFilter: input.TagFilter, DurationMs: time.Since(start).Milliseconds(), Source: ledger.SourceAgent, AOCIToolCalls: 1, LocalRecalls: 1})
	return textResult(mcpMessage("mcp.search.summary", len(matched)) + builder.String() +
		sessionCognitionSuffix(root, mcpServiceVersion, loaded.set, refreshSession))
}

func relBase(relativePath string) string {
	relativePath = strings.TrimSuffix(relativePath, "/")
	if position := strings.LastIndex(relativePath, "/"); position >= 0 {
		return relativePath[position+1:]
	}
	return relativePath
}
