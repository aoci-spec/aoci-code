// The read-only header tool (v2.8 P2): fetch the index header (rules + tag dictionary) without reading the whole file.
// Index entry: tools_header.go (to be recorded, entering the index with this batch)
//
// Position: when an agent needs the dictionary (drafting tags, understanding the layers) it previously had to read the whole aoci.txt,
// wasting tokens on a large index; this tool returns only the header block (before the first === section header), with
// the same boundary definition as index.ExtractHeader (the third consumer of "the parsing side and the editing side name the same text",
// never writing a second boundary judgement of its own).
//
// source is an argument (the MCP handler passes agent, the CLI passes human); the first version carried the default source in a
// package-level variable, which was not concurrency-safe and mislabelled the ledger, so it became an argument as soon as both sides were wired.
package mcptools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/textassets"
)

// BuildHeaderText returns the raw index header (pure read, shared by the MCP and CLI sides).
// An empty header (zero # lines) returns an empty string and the caller supplies the hint wording; source enters the ledger's source classification.
func BuildHeaderText(root, source string) (string, *Fail) {
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		return "", fail
	}
	if deliveryFail := pendingCognitionDeliveryFail(root, loaded.set); deliveryFail != nil {
		return "", deliveryFail
	}
	if loaded.set.LayoutMode == cognition.LayoutVolumesV1 {
		ledger.Append(root, loaded.cfg.LedgerEnabled, ledger.Event{Op: "header_show", Source: source})
		return mcpMessage("mcp.volume.header_identity", loaded.set.Root.SHA256, loaded.set.Meta.SHA256) + string(loaded.set.Meta.Raw), nil
	}
	header, _ := index.ExtractHeader(string(loaded.set.Root.Raw))
	ledger.Append(root, loaded.cfg.LedgerEnabled, ledger.Event{Op: "header_show", Source: source})
	return header, nil
}

// registerHeaderTool registers aoci_header (mounted from RunStdio in server.go).
// A zero-argument tool: In is any, so go-sdk generates an empty object schema.
func registerHeaderTool(
	srv *mcp.Server,
	root string,
	mcpServiceVersion string,
	descriptions mcpToolDescriptions,
	refreshSession *cognitionRefreshSession,
) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "aoci_header",
		Description: descriptions[textassets.ContractMCPHeaderDescription],
	}, func(ctx context.Context, req *mcp.CallToolRequest, in any) (*mcp.CallToolResult, any, error) {
		res := guard(func() *mcp.CallToolResult {
			header, fail := BuildHeaderText(root, "agent")
			if fail != nil {
				return failResult(fail)
			}
			suffix := ""
			if loaded, loadFail := loadCognitionCtx(root); loadFail == nil {
				suffix = sessionCognitionSuffix(root, mcpServiceVersion, loaded.set, refreshSession)
			}
			if header == "" {
				if err := validateMCPContracts(textassets.ContractMCPHeaderEmptyMessage); err != nil {
					return errResult(errInternal, err.Error(), mcpMessage("mcp.asset.retry_hint"))
				}
				return textResult(mcpContract(textassets.ContractMCPHeaderEmptyMessage) + suffix)
			}
			return textResult(header + suffix)
		})
		return res, nil, nil
	})
}
