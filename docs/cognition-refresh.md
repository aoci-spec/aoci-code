# Cognition refresh in long-running tasks

Start a normal task with one complete AOCI Overview, then keep using it while
it remains useful and reliable. AOCI can report checkpoint facts when the host
reports context compaction, the current deduplicated semantic-change count
reaches the project threshold, or the work crosses a genuinely major phase
boundary. Semantic-threshold and phase-transition facts advise the AI Agent;
known Host context compaction requires the complete reload described below.
None of these facts vetoes an explicit Overview.

This is a refresh gate around the existing Overview and Maintain tools. It is
not an AI Agent runtime, watcher, daemon, or second maintenance workflow.

## The short rule

Use `check_only=true` when only compact checkpoint facts are needed. An
ordinary call (`check_only` absent or false) is an explicit cognition request
and returns the complete requested scope whenever AOCI can form a coherent
snapshot. A matching receipt, a below-threshold count, or no pending reason
does not suppress that body.

If the repository has semantic drift, finish the nearest stable work unit and
run the normal formatting, lint, and tests first. Then run `aoci_maintain` once,
resolve `repair_required`, classify any `stopped` attempt from its formal-write
and Recovery evidence, and use Verify, Check, and Guide
to prove alignment. An ordinary Overview can still deliver the formal body
before alignment, but it marks that cognition Dirty and unreliable; current
source and tests remain implementation truth.

## Project threshold

The team-owned setting lives in `.aoci/config.json`:

```json
{
  "cognition_refresh_threshold": 30
}
```

Use the CLI rather than editing the file by hand:

```bash
aoci --repo . config get cognition_refresh_threshold
aoci --repo . config set cognition_refresh_threshold 50
```

The default is 30. Values from 1 through 100000 are accepted. Older repositories
without the field use the default. Invalid values stop configuration loading;
they never become a zero threshold. This is team policy, so
`.aoci/config.local.json` cannot override it.

The count is the number of distinct current paths that are real semantic Stale,
Actionable Missing, or Pending Curation. Repeated edits to one file count once.
Format-only and line-ending-only changes, Curation exclusions, technical skips,
Git ignores, AOCI runtime files, Baseline restorations, and out-of-scope objects
do not count. Orphans keep their existing explicit governance and are not added
to this count.

## Declaring host events

Hosts declare events through the existing `aoci_overview` input. AOCI does not
claim to detect context compaction itself.

```json
{
  "check_only": true,
  "refresh_reasons": ["context_compaction"],
  "refresh_event_id": "host-run-42-compaction-1",
  "stable_checkpoint": false
}
```

For a major phase change, use `phase_transition`. Do not use it for a completed
function, a single test run, or a small checklist item. The same event ID may be
retried safely. When context compaction, the semantic threshold, and a phase
change coincide, AOCI merges all three reasons.

A known Host context-compaction event is not a local-recall uncertainty. Its
compacted handoff must not retain or summarize the formal Whole-Index or any
Overview Header, Entry, Chunk, Challenge, or Attestation body. It may retain
only receipt identity, unfinished write or Recovery state needed for safe
continuation, and an instruction to reload immediately. Index semantics or a
receipt copied into the handoff cannot prove that the resumed model's current
cognition is reliable.

If `aoci_rules` is no longer reliably present, read it first. Before continuing
the business task, declare `context_compaction` with a fresh
`refresh_event_id` and make an ordinary complete Whole-Index Overview request,
not `check_only` and not a cognition probe. Follow every exact `next_cursor`
through `completed=true`, confirm delivery, and submit one Attestation from the
newly delivered body. After that fresh complete transport, a partial or failed
Attestation still consumes the generation and permits the existing source-bound
continuation without an automatic second Overview.

`check_only` is the inexpensive probe: it always returns compact JSON. Calling
without `check_only` asks AOCI to deliver the complete selected scope. The
AI Agent decides when it needs that view except after known Host compaction,
where the ordinary complete Overview is mandatory.

In the Volumes v1 layout, `aoci_overview` also accepts `project`,
`code`, and `database` scopes. Scope selection changes delivery identity, not
the three-reason refresh model. Receipt v2 uses scope identity for partial
reliability and reports composite identity separately; only a complete `all`
delivery can establish full repository cognition.

The threshold cannot be lost if the Host reaches a stable checkpoint and calls
`aoci_maintain` directly. Maintain records the machine-derived reason before
Apply advances the Baseline, and the aligned Apply result returns
`refresh_ready_for_overview` plus `semantic_threshold`. That result means: do
not maintain again; complete the required alignment proof, then let the AI Agent
decide whether its next phase needs an ordinary Overview.

| Status | Meaning |
| --- | --- |
| `refresh_not_required` | Checkpoint recommends reusing current cognition; explicit Overview remains available |
| `refresh_deferred_until_stable` | Finish the nearest stable work unit before maintenance |
| `refresh_required` | Run the existing Maintain and alignment gates |
| `refresh_ready_for_overview` | Governance is ready for a reliable explicit Overview after alignment proof |

Refresh status does not change the explicit delivery mode. An ordinary call
still requests the complete selected scope even when refresh is not required
or a matching receipt already exists. Only `check_only=true` requests a compact
checkpoint. Full-body, Host-confirmation, compatibility-fallback, blocked, and
chunk-recovery semantics are defined in
[`overview-delivery.md`](overview-delivery.md).

## Receipts and repeated calls

A complete, Host-confirmed, governance-aligned Attestation attempt advances
`refresh_generation`, records the last event ID, clears merged reasons, and
starts the next cycle. A passing Attestation records a reliable receipt. A
partial or failed Attestation records an uncertain receipt and disables a
full-system claim, but it does not automatically request another Overview in
the same generation. Repeated
ordinary calls still return the complete requested scope; repeated check_only
calls stay compact. The server keeps only small session state and never caches
the complete index text.

For the legacy host assessment, `model_cognition_state=invalid` implies
context compaction only when it accompanies a previously reliable complete
receipt. Initial establishment or an uncertain post-governance receipt does not
invent a compaction event.

Pending recovery, partial cross-Volume state, damaged declared assets, or a
concurrent formal-asset change stops ordinary delivery without a mixed body.
If the body exceeds `overview_delivery.chunk_tokens`, the ordinary call starts
deterministic Entry-boundary delivery at Chunk 1. The AI Agent follows each exact
cursor automatically to completion; a partial chain never establishes reliable
cognition.

Chunk Receipt and Attestation ordinals come from one canonical 1-based formal
Entry/object sequence. Header content, comments, blank lines, markers, receipts,
and Metadata are excluded. Host truncation, missing/duplicate/reordered Chunks,
or cursor or snapshot/configuration changes stop the cognition chain.
Attestation failure stops complete-cognition acceptance and semantic retry,
not an honest answer or source-bound task. Until Attestation completes, Memory, source, Spec, direct
`aoci.txt`, historical sessions, Scope, Search, and Entry reads cannot be used
to repair or supplement cognition.

## Initial establishment versus compaction refresh

Initial cognition keeps the strict acceptance gate. Complete transport,
100-percent object coverage, a passing Challenge (at least 80 percent of
ordinals fully correct with at most one object identity miss), passing
Attestation, aligned
governance, and sufficient framework mastery are all required before
`model_full_cognition_reliable=true`. A partial or failed initial Attestation
cannot be repaired from source or local retrieval and cannot authorize
Root/Meta, Migration, layout-wide, or other unbound system decisions. It may,
however, be reported honestly and followed by task-bound source and test
inspection.

The additive cognition level keeps that distinction visible: `no_cognition`
means there is no valid Index, `index_loaded` means content exists without
confirmed complete delivery, `delivery_verified` means delivery is confirmed
while strict Attestation is unfinished, `cognition_verified` adds a passing
Challenge, and `cognition_governed` adds governance alignment. These labels do not
replace `model_full_cognition_reliable`, `cognition_assimilation`, or
`governance_aligned`. A confirmed delivery with failed Attestation is therefore
reported as loaded and delivery-verified, not as absence of system cognition.

After a previously accepted cognition cycle, known Host context compaction
first takes the mandatory ordinary Overview path above. It then uses the same
Overview and Attestation protocols but a slimmer post-attempt control rule. When all Chunks
arrive, the Host confirms the body, the cognition identity is unchanged,
governance is aligned, and neither Recovery nor third-party conflict is
pending, the Attestation attempt consumes that refresh generation. A passing
Challenge (8/10 or better with at most one identity miss) restores a reliable
complete-system claim; 7/10 or 0/10 leaves the receipt uncertain and returns
`delivery_guidance=full_system_claim_disabled_source_bound_task_continuation_allowed`.
The AI Agent continues the existing source-bound task without a second automatic
Overview. If repository semantics changed, it first stabilizes code and tests,
runs Maintain once, resolves governance, and proves Verify/Check/Guide aligned.

## Long-running Host continuity

A question about progress, design, architecture, timing, or an error is a
non-control message. The AI Agent answers concisely, preserves the current Plan,
Run, candidate, commit, Push, and CI identities, and resumes the existing
`next_action`; the user does not need to say “continue.” Only an explicit stop,
pause, cancel, rollback, scope change, or prohibition on commit or Push changes
the task. This is a Host-Agent interpretation rule, not a persistent Task
Session.

`repair_required` is an automatic bounded candidate correction. A `stopped`
write attempt is classified using the existing Draft Run, Intent, Receipt,
Ledger, Archive, CAS, and Recovery evidence: proven zero-write closes and
replans; a provable postimage resumes; an exact preimage rolls back when policy
selects it. Unprovable state, third-party formal bytes, non-converging CAS,
missing semantic evidence, approval, or external action remains a real stop.

## Auto interruption audit

This table is documentation and test traceability, not runtime state.

| Trigger | Previous auto behavior | Real risk | Current behavior | Authority or fix | Test evidence |
| --- | --- | --- | --- | --- | --- |
| User progress, timing, explanation, or architecture question | Host could end the turn and lose the task | None | continue; answer then resume `next_action` | Runtime Rules and AGENTS Host contract | textasset/cross-layer contract tests; longrun black box |
| Explicit stop, pause, cancel, rollback, or scope change | Inconsistent Host inference | User changed authority | user action or replan | Runtime Rules and AGENTS Host contract | contract anchors and black-box scenario |
| Context compaction, Challenge passing (8/10 or better) | Full refresh then continue | Lost framework recall | continue with reliable receipt | Existing refresh session | Overview refresh tests |
| Context compaction, Challenge 7/10 or 0/10 | Absolute task stop and repeated ready status | Only complete-system claim is unproven | continue source-bound; uncertain receipt; consume generation | `recordAttestedDelivery` and delivery guidance | `TestOverviewPartialAttestationDisablesFullClaimButConsumesRefreshAttempt` |
| Host truncation, missing/reordered Chunk, cursor, Index, or config change | Stop | Incomplete or mixed formal body | hard block cognition chain | Existing Chunk and snapshot validation | Overview delivery and cursor tests |
| Dirty repository during refresh | Could refresh repeatedly before stable work | Formal cognition is stale | finish work, Maintain once, Verify/Check/Guide, then optional refresh | Existing semantic facts and Guide | refresh threshold tests; longrun black box |
| `repair_required` | Sometimes surfaced as user stop | Candidate semantics or shape is repairable; zero formal writes | bounded auto repair of failed candidates only | Existing findings and full-batch resubmission | Entries Auto tests; longrun black box repair injection |
| Plan or Manifest expires before formal write | `stopped` surfaced directly | Candidate binding changed | zero-write closure, close old Run, fresh Plan, reuse only unchanged SHA-bound semantics | Existing run manifest and Generation Plan | Entries Auto stale-plan and zero-write tests |
| Draft files, Ledger growth, Overview reads, mtime, or runtime excludes change | Risk of self-invalidating Plan | None; operation-created runtime facts | excluded from formal identity | Existing Plan/Envelope digest inputs | plan-identity tests and source audit |
| Observe review plus indexed Stale/Missing | Could appear mutually blocked | Observe fingerprint must not absorb production Baseline | acknowledge only observe evidence, then continue authoring | Existing Managed Scope acknowledge path | `TestScopeAcknowledgePreservesIndexAuthoringDebt` and clean-room smoke |
| Write failure before formal bytes | Generic stop | None after zero-write proof | auto close and replan | Existing zero-write closure | Entries Auto closure tests |
| Partial formal write with complete Intent and postimage | Generic stop | Duplicate write if replayed blindly | auto Resume same transaction | Existing Recovery/CAS | atomic recovery fault tests |
| Policy-selected Rollback with exact preimage | Generic stop | Must restore exact bytes | auto Rollback, verify, replan | Existing Recovery/Archive | rollback fault tests |
| Unprovable Recovery or broken Receipt chain | Generic stop | Formal state cannot be established | hard blocked | Existing Recovery validation | negative recovery tests |
| Third-party formal byte or persistent CAS conflict | Generic stop | Overwriting another writer | hard blocked | Existing CAS and snapshot confirmation | concurrency and snapshot tests |
| Approval or TTY required | Generic stop | Human authority boundary | user action required | Existing interaction contract | Migration/approval tests |
| Secret, unsafe path, device object, P0/P1, or missing model semantics | Generic stop | Security or unsupported cognition | hard blocked | Safe Inventory, validation, candidate binding | safety and authoring negative tests |
| Database Evidence or credential configuration required | Generic stop | External evidence/authority absent | user action required; no connection | Existing Database Evidence boundary | database evidence tests |
| Expected governance exit code under `set -e` | Shell exited before assertion | None if the status is contractually expected | capture and assert exit before restoring `set -e` | clean-room smoke | release smoke fixture |
| Installed tool omitted from non-login PATH | Reported as product failure | Environment discovery only | bind trusted absolute toolchain and retry once | Makefile, smoke, Host runtime contract | Make and release tests |
| CI cancelled for runner/network infrastructure | Could become product block or noisy polling | External transient fault | retry same SHA once; never retry compile/test/race/security failures | Development/release Host contract | workflow policy review |
| Response stream or Host disconnect | Could replay writes, commit, Push, or gates | Need identity proof | re-read formal state/Receipt and continue same transaction or SHA | Existing Receipt, Git, and CI identity | longrun and recovery tests |
| Root/Meta/Volume destructive lifecycle, public Tag or release | Could be inferred from auto mode | High-impact authority boundary | user action required | Existing lifecycle and release contracts | approval and capability tests |

There is no production Taskrun package or persistent conversation file. The
`scripts/blackbox/longrun-refresh` program remains an isolated test harness;
onboarding and refresh continue to reuse their existing state and transaction
systems.

## Host boundary

The ordinary Codex, Claude Code, and Cursor integrations configure AOCI as an
MCP stdio server. For Codex, `aoci init --agent codex --hooks` additionally
installs a project `compact_prompt` and a `SessionStart` hook for the `compact`
source. The compact prompt limits the handoff to receipt identity, unfinished
write or Recovery state, and the immediate reload instruction; the session hook
reasserts that instruction when Codex resumes. Project hooks run only after the
user reviews and trusts them through Codex `/hooks`.

A `PreCompact` hook cannot inject text into, or delete history from, the Host's
compaction input. It therefore cannot enforce this boundary by itself and is
not a substitute for the installed `compact_prompt` plus `SessionStart(compact)`
pair. Where a Host does not expose a deterministic compaction event, the AI
Agent must explicitly declare that it knows the complete repository view was
lost. Test harnesses may simulate that declaration, but their reports must
label it as a simulation.

The exact additive schema and compatibility rules are in
[`spec/aoci-cognition-refresh-v1.txt`](../spec/public/aoci-cognition-refresh-v1.txt).
The isolated end-to-end runner and its evidence fields are documented in
[`scripts/blackbox/longrun-refresh`](../scripts/blackbox/longrun-refresh/README.md).

## Session line and probe

Volumes read tools (`aoci_search`, `aoci_get_entries`, `aoci_header`) end with
one advisory line such as `cognition: refresh_status=refresh_not_required
generation=1`, built from in-process session facts at zero scan cost. Its
default is a license *not* to recall; it escalates only on real signals, and
when the index has moved past everything the session assessed it recommends a
cheap `check_only` checkpoint instead of fabricating a verdict.

`check_only` additionally accepts `probe=true` — a deterministic two-question
sample of the current Entry sequence — and `probe_answers` for memory-only
answers graded with the Attestation's criteria. Passing proves retained
cognition without re-reading ~50k tokens; failing is the machine evidence for
declaring `context_compaction`. The probe never advances the refresh
generation and never records a reason. It is available only when no Host
compaction event is known. After known compaction, do not issue or answer it;
answers reconstructed from a compacted handoff or summary are not retained
cognition.
