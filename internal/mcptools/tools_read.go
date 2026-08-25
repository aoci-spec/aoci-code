// 四个只读工具: aoci_rules / aoci_overview / aoci_get_entries / aoci_search
// 索引条目: tools_read.go[MRD9L]
//
// 纪律:
//   - ordinary Overview delivers the complete requested scope either inline or
//     through the deterministic Entry-boundary continuation chain;
//   - check_only returns checkpoint facts and never vetoes an explicit body request;
//   - Dirty formal text is delivered without a reliable receipt, while pending
//     recovery and mixed snapshots fail closed;
//   - valid cognition can be reused; local uncertainty prefers source and local recall;
//   - get_entries每条返回前实时计算当前指纹并按团队line_ending_tolerance
//     与Baseline比较；纯CRLF/LF表示差异静默视为等价，不向Agent注入假STALE;
//   - 基线缺失用UNBASELINED措辞区分，绝不误报STALE;
//   - dir模式上限50条，search结果上限30条;
//   - 全部path入参先过NormalizeRelPath;
//   - 四工具全落Ledger。
//
// Overview transport never selects, summarizes, compresses, or rewrites FRAS.
// Chunking is only a transport frame over the same ordered Whole-Index bytes.
package mcptools

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/textassets"
)

const (
	maxDirEntries    = 50
	maxSearchResults = 30
)

type rulesIn struct {
	ModulePath *string `json:"module_path,omitempty"`
}

type overviewIn struct {
	Scope                 string                    `json:"scope,omitempty"`
	CognitionStateVersion string                    `json:"cognition_state_version,omitempty"`
	Receipt               *cognitionReceipt         `json:"cognition_receipt,omitempty"`
	ModelState            string                    `json:"model_cognition_state,omitempty"`
	ScopeCovered          *bool                     `json:"scope_covered,omitempty"`
	CheckOnly             bool                      `json:"check_only,omitempty"`
	RefreshReasons        []string                  `json:"refresh_reasons,omitempty"`
	RefreshEventID        string                    `json:"refresh_event_id,omitempty"`
	StableCheckpoint      *bool                     `json:"stable_checkpoint,omitempty"`
	HostConfirmation      *overviewHostConfirmation `json:"host_delivery_confirmation,omitempty"`
	ModelAttestation      *overviewModelAttestation `json:"model_cognition_attestation,omitempty"`
	Cursor                string                    `json:"cursor,omitempty"`
	Probe                 bool                      `json:"probe,omitempty"`
	ProbeAnswers          *overviewProbeAnswers     `json:"probe_answers,omitempty"`
}

type getEntriesIn struct {
	Paths      []string `json:"paths,omitempty"`
	Dir        string   `json:"dir,omitempty"`
	VolumeID   string   `json:"volume_id,omitempty"`
	ObjectRefs []string `json:"object_refs,omitempty"`
}

type searchIn struct {
	Keyword   string `json:"keyword,omitempty"`
	TagFilter string `json:"tag_filter,omitempty"`
	Scope     string `json:"scope,omitempty"`
}

func registerReadTools(
	server *mcp.Server,
	root string,
	mcpServiceVersion string,
	descriptions mcpToolDescriptions,
	inputSchemas mcpInputSchemas,
	refreshSession *cognitionRefreshSession,
) {
	if refreshSession == nil {
		refreshSession = newCognitionRefreshSession()
	}
	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "aoci_rules",
			Description: descriptions[textassets.ContractMCPRulesDescription],
			InputSchema: inputSchemas["aoci_rules"],
		},
		func(
			ctx context.Context,
			request *mcp.CallToolRequest,
			input rulesIn,
		) (
			*mcp.CallToolResult,
			any,
			error,
		) {
			return guard(
				func() *mcp.CallToolResult {
					start := time.Now()

					loaded, fail := loadCognitionCtx(root)
					if fail != nil {
						return failResult(fail)
					}

					output, resourceErr := index.BuildRuntimeRules(
						loaded.set.Root.Document,
						loaded.cfg.CognitionRefreshThreshold,
						loaded.cfg.OverviewDelivery.ChunkTokens,
					)
					if resourceErr != nil {
						return errResult(
							errInternal,
							resourceErr.Error(),
							mcpMessage("mcp.asset.retry_hint"),
						)
					}

					// 磁盘二进制已替换而进程未重启时, 在会话契约末尾附上机器事实,
					// 让新会话第一时间知道自己连着一个过时的服务进程。
					if serviceBinaryReplacedOnDisk() {
						output += "\nservice_binary_replaced_on_disk: true\n"
					}
					if input.ModulePath != nil {
						projection, projectionErr := cognition.BuildProjectModuleCognition(loaded.set, *input.ModulePath)
						if projectionErr != nil {
							return errResult(
								errBadArgs,
								mcpMessage("mcp.rules.module_invalid", localeSafeMCPDetail(projectionErr.Error())),
								"",
							)
						}
						if len(projection.Objects) > maxDirEntries {
							return errResult(errBadArgs, mcpMessage("mcp.rules.module_limit", maxDirEntries), "")
						}
						if snapshotFail := confirmCognitionSnapshot(root, loaded.set); snapshotFail != nil {
							return failResult(snapshotFail)
						}
						encoded, marshalErr := json.Marshal(projection)
						if marshalErr != nil {
							return errResult(errInternal, marshalErr.Error(), "")
						}
						output += "\nAOCI Project Module Cognition JSON:\n" + string(encoded) + "\n"
					}

					ledger.Append(
						root,
						loaded.cfg.LedgerEnabled,
						ledger.Event{
							Op: "rules",
							DurationMs: time.
								Since(start).
								Milliseconds(),
							Source: ledger.SourceAgent,
						},
					)

					return textResult(output)
				},
			), nil, nil
		},
	)

	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "aoci_overview",
			Description: descriptions[textassets.ContractMCPOverviewDescription],
			InputSchema: inputSchemas["aoci_overview"],
		},
		func(
			ctx context.Context,
			request *mcp.CallToolRequest,
			input overviewIn,
		) (
			*mcp.CallToolResult,
			any,
			error,
		) {
			return guard(
				func() *mcp.CallToolResult {
					start := time.Now()
					if err := validateOverviewCognitionStateVersion(input.CognitionStateVersion); err != nil {
						return errResult(
							errBadArgs,
							mcpMessage("overview.cognition_state.version_invalid", input.CognitionStateVersion),
							"",
						)
					}
					if (input.Probe || input.ProbeAnswers != nil) && !input.CheckOnly {
						return errResult(errBadArgs, "cognition_probe_requires_check_only", "")
					}
					if input.Probe && input.ProbeAnswers != nil {
						return errResult(errBadArgs, "cognition_probe_request_and_answers_conflict", "")
					}
					if result, handled := handleFrozenOverviewContinuation(
						root, input, refreshSession, start,
					); handled {
						return result
					}

					loaded, fail := loadCognitionCtx(root)
					if fail != nil {
						return failResult(fail)
					}
					if loaded.set.LayoutMode == cognition.LayoutVolumesV1 {
						return handleVolumeOverview(root, mcpServiceVersion, input, loaded, refreshSession, start)
					}
					repository := loaded.legacyRepo()

					semantic, semanticFail := inspectSemanticChanges(root, repository)
					if semanticFail != nil {
						return failResult(semanticFail)
					}
					hostReasons, eventID, inputFail := normalizeRefreshInput(input)
					if inputFail != nil {
						return failResult(inputFail)
					}

					currentReceipt := newCognitionReceipt(
						root, mcpServiceVersion, repository.text, cognitionScopeRepositoryFull,
					)
					refreshSession.mu.Lock()
					defer refreshSession.mu.Unlock()
					assessment := refreshSession.evaluate(
						input,
						currentReceipt,
						semantic,
						hostReasons,
						eventID,
					)
					if input.CheckOnly {
						var probe *cognitionProbe
						var probeResult *cognitionProbeResult
						if input.Probe || input.ProbeAnswers != nil {
							sequence, sequenceErr := legacyOverviewSequence(repository.doc, repository.text)
							if sequenceErr != nil {
								return errResult(errInternal, sequenceErr.Error(), "")
							}
							if input.Probe {
								probe = buildCognitionProbe(currentReceipt.IndexSHA256, assessment.Receipt.RefreshGeneration, sequence)
							} else {
								probeResult = gradeCognitionProbe(input.ProbeAnswers, currentReceipt.IndexSHA256, assessment.Receipt.RefreshGeneration, sequence)
							}
						}
						compact := renderOverviewCheckpoint(assessment, input, true, probe, probeResult)
						ledger.Append(root, repository.cfg.LedgerEnabled, ledger.Event{
							Op:              "cognition_check",
							DurationMs:      time.Since(start).Milliseconds(),
							Source:          ledger.SourceAgent,
							AOCIToolCalls:   1,
							OverviewReads:   0,
							IndexSHA256:     currentReceipt.IndexSHA256,
							PathsCount:      semantic.Count,
							SemanticFiles:   semantic.Count,
							FormatOnlyFiles: semantic.FormatOnly,
							DriftWarned:     !semantic.GovernanceAligned,
							OutputBytes:     len(compact),
							EstimatedTokens: len(compact) / 3,
						})
						return textResult(compact)
					}
					if deliveryFail := pendingCognitionDeliveryFail(root, loaded.set); deliveryFail != nil {
						return textResult(renderOverviewBlocked(root, "recovery_pending", cognitionStateV2Requested(input)))
					}
					deliveredReceipt, markDelivered := refreshSession.explicitDeliveryReceipt(
						currentReceipt, assessment, eventID, true,
					)
					sectionCount, entryCount := countOverviewDimensions(repository.doc)
					sequence, sequenceErr := legacyOverviewSequence(repository.doc, repository.text)
					if sequenceErr != nil {
						return errResult(errInternal, sequenceErr.Error(), "")
					}
					delivery, resourceErr := renderOverviewDelivery(overviewRenderContext{
						Root: root, MCPServiceVersion: mcpServiceVersion,
						LayoutMode:     string(loaded.set.LayoutMode),
						RequestedScope: cognitionScopeRepositoryFull,
						EffectiveScope: cognitionScopeRepositoryFull,
						ScopeIdentity:  currentReceipt.IndexSHA256,
						Content:        repository.text, ContentBytes: len(repository.text),
						EntryCount: entryCount, SectionCount: sectionCount,
						EstimatedTokens: len(repository.text) / 3,
						Receipt:         deliveredReceipt, Assessment: assessment,
						Sequence: sequence, Session: refreshSession,
					}, input, loaded.cfg.OverviewDelivery.ChunkTokens)
					if resourceErr != nil {
						return errResult(errBadArgs, resourceErr.Error(), "")
					}
					if snapshotFail := confirmCognitionSnapshot(root, loaded.set); snapshotFail != nil {
						return failResult(snapshotFail)
					}
					refreshSession.recordAttestedDelivery(
						deliveredReceipt, delivery.Attestation, markDelivered,
					)

					ledger.Append(
						root,
						repository.cfg.LedgerEnabled,
						delivery.Facts.ledgerEvent(
							time.Since(start).
								Milliseconds(),
						),
					)

					return textResult(delivery.Output)
				},
			), nil, nil
		},
	)

	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "aoci_get_entries",
			Description: descriptions[textassets.ContractMCPGetEntriesDescription],
			InputSchema: inputSchemas["aoci_get_entries"],
		},
		func(
			ctx context.Context,
			request *mcp.CallToolRequest,
			input getEntriesIn,
		) (
			*mcp.CallToolResult,
			any,
			error,
		) {
			return guard(
				func() *mcp.CallToolResult {
					return handleGetEntries(
						root,
						mcpServiceVersion,
						input,
						refreshSession,
					)
				},
			), nil, nil
		},
	)

	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "aoci_search",
			Description: descriptions[textassets.ContractMCPSearchDescription],
			InputSchema: inputSchemas["aoci_search"],
		},
		func(
			ctx context.Context,
			request *mcp.CallToolRequest,
			input searchIn,
		) (
			*mcp.CallToolResult,
			any,
			error,
		) {
			return guard(
				func() *mcp.CallToolResult {
					return handleSearch(
						root,
						mcpServiceVersion,
						input,
						refreshSession,
					)
				},
			), nil, nil
		},
	)
}

func overviewCursorOnly(input overviewIn) bool {
	return input.Cursor != "" && input.Receipt == nil && strings.TrimSpace(input.ModelState) == "" &&
		input.ScopeCovered == nil && !input.CheckOnly && len(input.RefreshReasons) == 0 &&
		strings.TrimSpace(input.RefreshEventID) == "" && input.StableCheckpoint == nil &&
		input.HostConfirmation == nil && input.ModelAttestation == nil &&
		!input.Probe && input.ProbeAnswers == nil
}

// handleFrozenOverviewContinuation reuses only immutable transport planning.
// It still confirms the exact formal and governance inputs bound by the first
// Chunk. A cache miss deliberately preserves the existing stateless path.
func handleFrozenOverviewContinuation(
	root string,
	input overviewIn,
	session *cognitionRefreshSession,
	start time.Time,
) (*mcp.CallToolResult, bool) {
	if !overviewCursorOnly(input) {
		return nil, false
	}
	frozen, hit, err := session.frozenMiddleOverview(input.Cursor)
	if err != nil {
		return errResult(errBadArgs, err.Error(), ""), true
	}
	if !hit {
		return nil, false
	}

	cfg, err := config.Load(root)
	if err != nil {
		return errResult(
			errIndexInvalid,
			mcpMessage("mcp.error.invalid_config", localeSafeMCPDetail(err.Error())),
			mcpMessage("mcp.error.invalid_config_hint"),
		), true
	}
	if cfg.IndexPath != frozen.indexPath {
		return errResult(errBadArgs, "overview_snapshot_changed", ""), true
	}
	if cfg.OverviewDelivery.ChunkTokens != frozen.plan.ChunkTokens {
		return errResult(errBadArgs, "overview_chunk_tokens_changed", ""), true
	}

	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil || string(set.LayoutMode) != frozen.plan.Context.LayoutMode {
		return errResult(errBadArgs, "overview_snapshot_changed", ""), true
	}
	if set.CompositeIdentity != frozen.plan.Context.Receipt.CompositeIdentity {
		return errResult(errBadArgs, "overview_snapshot_changed", ""), true
	}
	currentIdentity := set.Root.SHA256
	if set.LayoutMode == cognition.LayoutVolumesV1 {
		requestedScope := frozen.plan.Context.RequestedScope
		if strings.TrimSpace(input.Scope) != "" {
			requested, scopeErr := set.Scope(input.Scope)
			if scopeErr != nil || requested.RequestedScope != requestedScope {
				return errResult(errBadArgs, "overview_snapshot_changed", ""), true
			}
		}
		view, scopeErr := set.Scope(requestedScope)
		if scopeErr != nil || !view.Available ||
			view.EffectiveScope != frozen.plan.Context.EffectiveScope ||
			view.AssetState != frozen.plan.Context.AssetState {
			return errResult(errBadArgs, "overview_snapshot_changed", ""), true
		}
		currentIdentity = view.ScopeIdentity
	}
	if currentIdentity != frozen.plan.Context.ScopeIdentity {
		return errResult(errBadArgs, "overview_snapshot_changed", ""), true
	}

	output, err := renderOverviewChunkWithSpans(
		frozen.plan.Context, frozen.plan.Body, frozen.plan.Challenge,
		frozen.plan.Spans, input.Cursor, frozen.plan.ChunkTokens,
		frozen.plan.Attestation, cognitionStateV2Requested(input),
	)
	if err != nil {
		return errResult(errBadArgs, err.Error(), ""), true
	}
	if snapshotFail := confirmCognitionSnapshot(root, set); snapshotFail != nil {
		return failResult(snapshotFail), true
	}
	loaded := &cognitionRepoCtx{cfg: cfg, set: set}
	if snapshotFail := confirmVolumeGovernanceSnapshot(root, loaded, frozen.governance); snapshotFail != nil {
		return failResult(snapshotFail), true
	}
	facts := frozen.plan.Facts
	facts.OutputBytes = len(output)
	ledger.Append(root, cfg.LedgerEnabled, facts.ledgerEvent(time.Since(start).Milliseconds()))
	return textResult(output), true
}

func handleGetEntries(
	root, mcpServiceVersion string,
	input getEntriesIn,
	refreshSession *cognitionRefreshSession,
) *mcp.CallToolResult {
	start := time.Now()

	if len(input.Paths) == 0 &&
		strings.TrimSpace(input.Dir) == "" &&
		len(input.ObjectRefs) == 0 {
		return errResult(
			errBadArgs,
			mcpMessage("mcp.entries.missing_input"),
			mcpMessage("mcp.entries.missing_input_hint"),
		)
	}

	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		return failResult(fail)
	}
	if deliveryFail := pendingCognitionDeliveryFail(root, loaded.set); deliveryFail != nil {
		return failResult(deliveryFail)
	}
	if loaded.set.LayoutMode == cognition.LayoutVolumesV1 {
		return handleVolumeGetEntries(root, mcpServiceVersion, input, loaded, refreshSession, start)
	}
	repository := loaded.legacyRepo()

	var builder strings.Builder

	drift := false
	count := 0

	render := func(entry *index.Entry) {
		stale, unbaselined, _ :=
			baseline.IsStaleFileWith(
				root,
				entry.RelPath,
				repository.bl,
				repository.cfg.
					LineEndingTolerance,
			)

		if stale {
			builder.WriteString(mcpMessage("mcp.entries.stale"))

			drift = true
		} else if unbaselined {
			builder.WriteString(mcpMessage("mcp.entries.unbaselined"))
		}

		builder.WriteString(
			entry.FullLine + "\n",
		)

		count++
	}

	if len(input.Paths) > 0 {
		for _, rawPath := range input.Paths {
			relativePath, err :=
				afs.NormalizeRelPath(
					rawPath,
				)
			if err != nil {
				builder.WriteString(mcpMessage(
					"mcp.entries.unsafe_path",
					errPathUnsafe,
					rawPath,
					localeSafeMCPDetail(err.Error()),
				))

				continue
			}

			entry := index.FindEntry(
				repository.doc,
				relativePath,
			)

			if entry == nil {
				builder.WriteString(mcpMessage("mcp.entries.not_indexed", relativePath))

				hints :=
					index.FindEntriesByFilename(
						repository.doc,
						relBase(relativePath),
					)

				if len(hints) > 0 {
					paths := []string{}

					for _, hint := range hints {
						if hint.RelPath != "" {
							paths = append(
								paths,
								hint.RelPath,
							)
						}
					}

					if len(paths) > 0 {
						builder.WriteString(mcpMessage(
							"mcp.entries.same_name",
							strings.Join(paths, ", "),
						))
					}
				}

				continue
			}

			render(entry)
		}
	} else {
		directory, err :=
			afs.NormalizeRelPath(
				input.Dir,
			)
		if err != nil {
			return errResult(
				errPathUnsafe,
				mcpMessage("mcp.entries.unsafe_dir", input.Dir, localeSafeMCPDetail(err.Error())),
				mcpMessage("mcp.entries.unsafe_dir_hint"),
			)
		}

		directory = strings.TrimSuffix(
			directory,
			"/",
		)

		hits := []*index.Entry{}

		for _, section := range repository.doc.Sections {
			for _, entry := range section.Entries {
				if entry.RelPath == "" {
					continue
				}

				if strings.HasPrefix(
					entry.RelPath,
					directory+"/",
				) ||
					entry.RelPath ==
						directory {
					hits = append(
						hits,
						entry,
					)
				}
			}
		}

		if len(hits) > maxDirEntries {
			return errResult(
				errBadArgs,
				mcpMessage("mcp.entries.dir_limit", directory, len(hits), maxDirEntries),
				mcpMessage("mcp.entries.dir_limit_hint"),
			)
		}

		for _, entry := range hits {
			render(entry)
		}

		if len(hits) == 0 {
			builder.WriteString(mcpMessage("mcp.entries.empty_dir", directory))
		}
	}

	ledger.Append(
		root,
		repository.cfg.LedgerEnabled,
		ledger.Event{
			Op:            "get_entries",
			PathsCount:    count,
			DurationMs:    time.Since(start).Milliseconds(),
			DriftWarned:   drift,
			Source:        ledger.SourceAgent,
			AOCIToolCalls: 1,
			LocalRecalls:  1,
		},
	)

	return textResult(mcpMessage("mcp.entries.summary", count) + builder.String())
}

func handleVolumeGetEntries(root, mcpServiceVersion string, input getEntriesIn, loaded *cognitionRepoCtx, refreshSession *cognitionRefreshSession, start time.Time) *mcp.CallToolResult {
	volumeID := strings.ToLower(strings.TrimSpace(input.VolumeID))
	if volumeID == "" && (len(input.Paths) > 0 || input.Dir != "") {
		volumeID = "code"
	}
	if volumeID != "code" && volumeID != "database" {
		return errResult(errBadArgs, mcpMessage("mcp.entries.volume_missing_input"), mcpMessage("mcp.entries.volume_missing_input_hint"))
	}
	if volumeID == "database" && (len(input.Paths) > 0 || strings.TrimSpace(input.Dir) != "") {
		return errResult(errBadArgs, mcpMessage("mcp.entries.volume_missing_input"), mcpMessage("mcp.entries.volume_missing_input_hint"))
	}
	if volumeID == "database" {
		for _, ref := range input.ObjectRefs {
			if !cognition.IsCanonicalDatabaseRef(strings.TrimSpace(ref)) {
				return errResult(errBadArgs, mcpMessage("mcp.entries.object_ref_invalid", ref), mcpMessage("mcp.entries.volume_missing_input_hint"))
			}
		}
	}
	asset := loaded.set.Volumes[volumeID]
	if asset == nil {
		ledger.Append(root, loaded.cfg.LedgerEnabled, ledger.Event{Op: "get_entries", DurationMs: time.Since(start).Milliseconds(), Source: ledger.SourceAgent, AOCIToolCalls: 1, LocalRecalls: 1})
		return textResult("AOCI Entries: volume_id=" + volumeID + " asset_state=absent scope_available=false count=0\n")
	}
	refs := append([]string{}, input.ObjectRefs...)
	if volumeID == "code" {
		refs = append(refs, input.Paths...)
	}
	var hits []cognition.Object
	if strings.TrimSpace(input.Dir) != "" {
		directory, err := afs.NormalizeRelPath(input.Dir)
		if err != nil {
			return errResult(errPathUnsafe, mcpMessage("mcp.entries.unsafe_dir", input.Dir, localeSafeMCPDetail(err.Error())), mcpMessage("mcp.entries.unsafe_dir_hint"))
		}
		directory = strings.TrimSuffix(directory, "/")
		for _, object := range asset.Objects {
			path := strings.TrimPrefix(object.CanonicalRef, "code:")
			if path == directory || strings.HasPrefix(path, directory+"/") {
				hits = append(hits, object)
			}
		}
		if len(hits) > maxDirEntries {
			return errResult(errBadArgs, mcpMessage("mcp.entries.dir_limit", directory, len(hits), maxDirEntries), mcpMessage("mcp.entries.dir_limit_hint"))
		}
	} else {
		for _, rawRef := range refs {
			ref := strings.TrimSpace(rawRef)
			if volumeID == "code" {
				normalized, err := afs.NormalizeRelPath(strings.TrimPrefix(ref, "code:"))
				if err != nil {
					return errResult(errPathUnsafe, mcpMessage("mcp.entries.unsafe_path", errPathUnsafe, rawRef, localeSafeMCPDetail(err.Error())), mcpMessage("mcp.entries.volume_missing_input_hint"))
				}
				ref = "code:" + normalized
			}
			found := false
			for _, object := range asset.Objects {
				if object.CanonicalRef == ref {
					hits = append(hits, object)
					found = true
					break
				}
			}
			if !found {
				hits = append(hits, cognition.Object{VolumeID: volumeID, CanonicalRef: ref})
			}
		}
	}
	var builder strings.Builder
	count := 0
	for _, object := range hits {
		if object.Entry == nil {
			builder.WriteString(mcpMessage("mcp.entries.object_ref_not_indexed", object.VolumeID, object.CanonicalRef))
			continue
		}
		builder.WriteString("[volume_id=" + object.VolumeID + " object_ref=" + object.CanonicalRef + "] " + object.Entry.FullLine + "\n")
		count++
	}
	ledger.Append(root, loaded.cfg.LedgerEnabled, ledger.Event{Op: "get_entries", PathsCount: count, DurationMs: time.Since(start).Milliseconds(), Source: ledger.SourceAgent, AOCIToolCalls: 1, LocalRecalls: count})
	return textResult(mcpMessage("mcp.entries.summary", count) + builder.String() +
		sessionCognitionSuffix(root, mcpServiceVersion, loaded.set, refreshSession))
}
