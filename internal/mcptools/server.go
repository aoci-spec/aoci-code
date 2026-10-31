// MCP server assembly / tool registration / error classification / stdio run
// Index entry: server.go[MSV9OS]
//
// Iron rule: in stdio mode stdout carries only the JSON-RPC protocol stream, and every log goes to stderr;
// any leftover println poisons the protocol stream and the client disconnects silently (the main.go entry already fixes log→stderr).
//
// Discipline:
//   - the nine tool names are fixed (aoci_rules/aoci_overview/aoci_get_entries/aoci_search/
//     aoci_update_entry/aoci_report/aoci_remove_entry/aoci_header/aoci_maintain);
//     renaming breaks the AGENTS.md template and experiment reproducibility;
//   - every tool call reads index/baseline/config fresh; a V2 continuation reuses only an immutable delivery confirmed by the live guard, since the index
//     is changed through channels outside MCP (hand edits in an editor, git pull), and a cached view breaks the first commandment "a stale index is more dangerous than none";
//   - parameters are all string/[]string and the return is plain text;
//   - a panic inside a handler is recovered by guard: recorded on stderr and returned as an MCP error result, never crashing the process;
//   - an error response holds the next-step suggestion and no Go stack trace.
//
// Error model (the review refactor): Fail{Code,Msg,Hint} is the single source of truth for classified errors;
// loadRepoCtx natively returns *Fail, the MCP side renders it as error text through failResult, and the CLI side renders it itself,
// a one-way "structure → wording"; any reverse coupling that "parses wording back into structure" is forbidden.
package mcptools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/codebatch"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
)

// Error classification codes (the values of Fail.Code; they open the first line of the error wording for telemetry and diagnosis, and route the CLI exit code)
const (
	errNotInitialized               = "not_initialized"  // 索引不存在: 未 init
	errIndexInvalid                 = "index_invalid"    // 索引不可解析
	errPathUnsafe                   = "path_unsafe"      // 路径校验拒绝
	errWriteConflict                = "write_conflict"   // 替换定位 0处/2+处
	errVolumeReadOnly               = "volume_read_only" // unsupported Volume writes remain read-only
	errCrossVolumeWriteNotSupported = "cross_volume_write_not_supported"
	errCrossVolumeGuardRequired     = "cross_volume_guard_required"
	errImpactResolutionFailed       = "impact_resolution_failed"
	errCandidateInvalid             = "candidate_invalid"
	errDirectoryUnspellable         = "directory_unspellable" // no candidate edit clears it: stopped, not repair_required
	errCognitionSnapshotUnavailable = "cognition_snapshot_unavailable"
	errBadArgs                      = "bad_args" // 参数不合法
	errInternal                     = "internal" // 兜底
)

// Fail is a classified error (classification code + actionable explanation + next-step suggestion), the structured source of truth for error information,
// rendered separately by the MCP handler and the CLI caller; cross-package comparisons use the Code string literal
// (its values are the constants above, a stable contract that appears on the first line of the outward error wording).
type Fail struct {
	Code                string
	Msg                 string
	Hint                string
	Findings            []cognition.RepairFinding
	Repairable          bool
	FormalWritesStarted bool
	GlobalStop          *GlobalStopFacts
	CodePlan            *codebatch.Plan
}

// GlobalStopFacts describes an asset-level prerequisite failure that no
// candidate in the submitted batch can repair. It deliberately remains
// separate from cognition.RepairFinding so candidate indexes are never faked.
type GlobalStopFacts struct {
	AffectedAsset  string `json:"affected_asset"`
	Field          string `json:"field"`
	RuleCode       string `json:"rule_code"`
	Expected       string `json:"expected"`
	Actual         string `json:"actual"`
	Cause          string `json:"cause"`
	SafeNextAction string `json:"safe_next_action"`
}

// textResult is a plain-text success result
func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

// errResult is a plain-text error result (IsError=true; holds the classification code and the next-step suggestion)
func errResult(code, msg, hint string) *mcp.CallToolResult {
	text := "[" + code + "] " + msg
	if hint != "" {
		text += "\n" + mcpMessage("mcp.error.hint_prefix") + hint
	}
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

// failResult is the single rendering point from Fail to an MCP error result
func failResult(f *Fail) *mcp.CallToolResult {
	return errResult(f.Code, f.Msg, f.Hint)
}

// guard wraps a handler body: a panic is recovered into an MCP error result, recorded on stderr, and the process does not crash
func guard(fn func() *mcp.CallToolResult) (res *mcp.CallToolResult) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(os.Stderr, mcpMessage("mcp.error.panic_recovered_log"))
			res = errResult(
				errInternal,
				mcpMessage("mcp.error.internal_recovered"),
				mcpMessage("mcp.error.internal_recovery"),
			)
		}
	}()
	return fn()
}

// repoCtx is the repository context of one tool call (read fresh per call, discarded after use)
type repoCtx struct {
	cfg   *config.Config
	paths config.Paths
	text  string          // the raw index text
	doc   *index.Document // ResolveRelPaths already applied
	bl    *baseline.Baseline
	// dict is the text that holds the tag dictionary and the E-scale thresholds. In Volumes v1 the dictionary lives in the Meta volume,
	// while the Code volume's own header is a single marker line, and extracting a header from text yields no threshold; Legacy keeps the
	// dictionary and the entries in one file, and leaving this empty falls back to the index header.
	dict string
}

// dictionaryText returns the text whose dictionary lines govern threshold
// checks for this context: the Meta Volume for a Volumes Code context, the
// index header for Legacy.
func (rc *repoCtx) dictionaryText(header string) string {
	if rc != nil && rc.dict != "" {
		return rc.dict
	}
	return header
}

type cognitionRepoCtx struct {
	cfg   *config.Config
	paths config.Paths
	set   *cognition.Set
	bl    *baseline.Baseline
}

func (loaded *cognitionRepoCtx) legacyRepo() *repoCtx {
	return &repoCtx{cfg: loaded.cfg, paths: loaded.paths, text: string(loaded.set.Root.Raw), doc: loaded.set.Root.Document, bl: loaded.bl}
}

// loadCognitionCtx is the only physical-layout loader used by MCP callers.
// Legacy is adapted to CognitionSet; Volumes v1 never falls through to the
// monolithic parser.
func loadCognitionCtx(root string) (*cognitionRepoCtx, *Fail) {
	// Only a layout transaction stops the loader: an MCP write receipt is
	// closed by the tool that wrote it, and that tool loads the repository
	// here. Delivery (Overview, Header, Search) still refuses on every receipt
	// kind through pendingCognitionDeliveryFail.
	if pending, err := cognitiontxn.PendingLayout(root); err != nil {
		return nil, &Fail{Code: errCognitionSnapshotUnavailable, Msg: mcpMessage(
			"overview.delivery.recovery_inspection_failed", localeSafeMCPDetail(err.Error()),
		)}
	} else if len(pending) > 0 {
		return nil, &Fail{
			Code: errCognitionSnapshotUnavailable,
			Msg:  mcpMessage("overview.delivery.pending_recovery", pending[0].Filename),
			Hint: mcpMessage("overview.delivery.pending_recovery_hint"),
		}
	}
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return nil, &Fail{Code: errIndexInvalid, Msg: mcpMessage("mcp.error.invalid_config", localeSafeMCPDetail(err.Error())), Hint: mcpMessage("mcp.error.invalid_config_hint")}
	}
	paths := config.AOCIPaths(root, cfg.IndexPath)
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &Fail{Code: errNotInitialized, Msg: mcpMessage("mcp.error.index_missing", paths.IndexPath), Hint: mcpMessage("mcp.error.index_missing_hint")}
		}
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			return nil, &Fail{Code: errInternal, Msg: mcpMessage("mcp.error.index_read", localeSafeMCPDetail(err.Error()))}
		}
		fail := &Fail{Code: errIndexInvalid, Msg: mcpMessage("mcp.error.cognition_invalid", localeSafeMCPDetail(err.Error())), Hint: mcpMessage("mcp.error.cognition_invalid_hint")}
		var validationErr *cognition.ValidationError
		if errors.As(err, &validationErr) {
			for _, finding := range validationErr.Findings {
				if finding.AssetID != cognition.ScopeMeta || !strings.HasPrefix(finding.Code, "meta_tag_dictionary_") {
					continue
				}
				fail.GlobalStop = &GlobalStopFacts{
					AffectedAsset: "aoci.meta.txt", Field: "tag_dictionary", RuleCode: finding.Code,
					Expected:       "one_parseable_conflict_free_A_B_C_optional_D_E_dictionary_per_enabled_object_volume",
					Actual:         "asset=meta;rule_code=" + finding.Code + ";detail=" + finding.Message,
					Cause:          mcpMessage("mcp.error.meta_tag_dictionary_unavailable", finding.Code, localeSafeMCPDetail(finding.Message)),
					SafeNextAction: mcpMessage("mcp.error.meta_tag_dictionary_repair_action"),
				}
				break
			}
		}
		return nil, fail
	}
	if set.LayoutMode == cognition.LayoutLegacyMonolithic && len(set.Root.Document.Sections) == 0 {
		return nil, &Fail{Code: errIndexInvalid, Msg: mcpMessage("mcp.error.no_sections"), Hint: mcpMessage("mcp.error.no_sections_hint", paths.IndexPath)}
	}
	bl, _, _ := baseline.Load(root)
	return &cognitionRepoCtx{cfg: cfg, paths: paths, set: set, bl: bl}, nil
}

// loadRepoCtx reads the repository context fresh. Failure returns (nil, *Fail).
func loadRepoCtx(root string) (*repoCtx, *Fail) {
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		return nil, fail
	}
	if loaded.set.LayoutMode == cognition.LayoutVolumesV1 {
		return nil, &Fail{Code: errVolumeReadOnly, Msg: mcpMessage("mcp.error.volume_read_only"), Hint: mcpMessage("mcp.error.volume_read_only_hint")}
	}
	return loaded.legacyRepo(), nil
}

func mcpMessage(key string, args ...any) string {
	value, err := textassets.Message(textassets.ActiveLocale(), key, args...)
	if err != nil {
		return fmt.Sprintf("[text_asset_error:%s]", key)
	}
	return value
}

func localeSafeMCPDetail(detail string) string {
	hasHan := strings.ContainsFunc(detail, func(character rune) bool {
		return unicode.Is(unicode.Han, character)
	})
	hasASCII := strings.ContainsFunc(detail, func(character rune) bool {
		return (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z')
	})
	if (textassets.ActiveLocale() == textassets.DefaultLocale && hasHan) ||
		(textassets.ActiveLocale() == textassets.LegacyLocale && !hasHan && hasASCII) {
		if facts := textassets.DiagnosticFacts(detail); facts != "" {
			return mcpMessage("mcp.error.localized_detail_with_facts", facts)
		}
		return mcpMessage("mcp.error.localized_detail_unavailable")
	}
	return detail
}

// RunStdio starts the stdio MCP server (blocking until ctx is cancelled or the peer closes).
// root is fixed at startup (from the working directory of aoci mcp or --repo); version is injected by the CLI.
func RunStdio(ctx context.Context, root, version string) error {
	// Record the on-disk identity of our own binary; the governance surface later uses it to expose "the disk was replaced, the process not restarted".
	// When os.Executable fails the self-check shuts off silently and never affects server startup.
	if executable, err := os.Executable(); err == nil {
		RecordServiceBinaryIdentity(executable)
	}
	srv, err := NewServer(root, version)
	if err != nil {
		return err
	}
	return srv.Run(ctx, &mcp.StdioTransport{})
}

// NewServer assembles the protocol handshake and the nine runtime tools
// for one repository under the version the caller injects. RunStdio uses it
// for the aoci mcp command; the embeddable engine uses it to host the same
// server over an in-memory transport. The version comes only from the
// caller: the tool layer never infers it from Git, PATH, the index, or
// configuration.
func NewServer(root, version string) (*mcp.Server, error) {
	// The server only reads configuration: Load would write the legacy Locale
	// upgrade into a repository that has none, drifting the configuration
	// bytes a prepared migration or Scope preview is bound to.
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return nil, err
	}
	if err := textassets.SetActiveLocale(cfg.Locale); err != nil {
		return nil, err
	}
	if err := textassets.ValidateRuntime(); err != nil {
		return nil, fmt.Errorf("%s", mcpMessage(
			"mcp.catalog_invalid",
			localeSafeMCPDetail(err.Error()),
		))
	}
	descriptions, err := loadMCPToolDescriptions()
	if err != nil {
		return nil, err
	}
	inputSchemas, err := loadMCPInputSchemas()
	if err != nil {
		return nil, err
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: machinecontract.MCPServerName, Version: version},
		&mcp.ServerOptions{SupportedProtocolVersions: machinecontract.MCPSupportedProtocolVersions()})
	srv.AddReceivingMiddleware(
		overviewSchemaDiagnostics(root, cfg.LedgerEnabled),
		compactToolListing(advertisedMCPInputSchemas(inputSchemas)),
	)
	refreshSession := newCognitionRefreshSession()
	registerReadTools(srv, root, version, descriptions, inputSchemas, refreshSession)
	registerWriteTools(srv, root, version, descriptions, inputSchemas, refreshSession)
	registerRemoveTool(srv, root, descriptions, inputSchemas)
	registerHeaderTool(srv, root, version, descriptions, refreshSession)
	registerMaintainTool(srv, root, version, descriptions, inputSchemas, refreshSession)
	return srv, nil
}
