# Changelog

All notable public changes to AOCI-CODE will be documented in this file.

## Unreleased

- Add zero-model Code target finalization through the existing
  `aoci_update_entry` tool. The exclusive `target_index` mode reads complete
  `aoci.code.target.txt`, binds changed/new and explicit `#Target-Reuse`
  Entries to final source hashes, applies one existing atomic batch, and
  synchronizes the target; unsafe or incomplete plans fall back to Maintain.
- Raise the default and maximum Overview Chunk budget to 600,000 tokens. A
  fitting Overview now returns only the exact body in model-visible `content`,
  keeps transport and state fields in Host-private top-level `_meta`, and needs
  no model confirmation or Attestation reply; explicitly smaller multi-Chunk
  configurations retain the compatible Host-driven cursor and proof path.
- Let the existing project `locale` setting govern UI text, managed Locale
  migration, and future Entry authoring in both Legacy and Volumes layouts
  without bulk-translating unchanged Entries. Historical Entry bytes remain
  valid; only newly created or genuinely updated Entries use the new Locale.
- Reuse one governance assessment within each strict Overview call and replace
  the duplicate tail assessment with an exact lightweight input-identity
  recheck. Strict call boundaries, cross-call drift rejection, Chunk and body
  integrity, Host confirmation, and model Attestation remain unchanged.
- Reduce the default Overview cognition Challenge from ten ordinals to one.
  Chunk ordering, per-Chunk and aggregate integrity checks, the final body marker,
  Host delivery confirmation, and the generic Attestation grading rules remain
  unchanged.
- Report one blocker per root cause. A Managed Scope policy that no longer
  matches its receipt already reports `scope_change_required`, but the business
  source manifest refuses to build in that state, and every manifest failure
  collapsed into one generic `business_source_manifest_invalid` whichever cause
  produced it. A reported project received both codes, and the second one named
  a subsystem that was working — so the operator investigated it and found
  nothing, because there was nothing to find. The derived report is dropped, and
  every other cause now carries its exact machine token instead of being erased.
  The cause is recognised through a sentinel rather than message text, so the
  rule survives a wording change.
- Hand back a remediation the repository can actually run. The Volumes Guide
  reaches its scope-change instruction only after the baseline-missing branch has
  returned, so a Baseline always exists there and `scan` always refuses — yet the
  instruction ended by offering `scan` anyway. It now states that a Baseline is
  present and names the governed Scope Change as the path.

## v0.1.0-rc5

- Stop treating a line-ending rewrite as a reason to stop governing a
  repository. `internal/volumegovernance` compared formal Volumes by raw
  SHA-256, making it the one consumer in the repository that bypassed
  `baseline.EquivalentFingerprints` — the function whose own contract declares
  it the single entry point for that judgement — and therefore the one place
  that ignored `line_ending_tolerance`, which defaults to true. `core.autocrlf`
  is the Git for Windows default, so an ordinary Windows checkout rewrote every
  Volume and hard-blocked the Guide over a difference the team policy already
  calls equivalent, while identical rewrites of business sources stayed
  authorable. Such a difference is now reported as
  `code_volume_line_ending_only` (with `root_`, `meta_`, and `database_`
  siblings) carrying the repair, and does not block.
- Refuse a `scan` that would publish a Baseline without the Volumes it governs.
  Scan takes its inventory from Git, so a formal cognition asset covered by
  `.gitignore`, `.git/info/exclude`, or `core.excludesFile` was silently absent
  from the Baseline; the repository then failed far away on a blocked Guide that
  named neither the rule nor the file. The refusal names both, and `--dry-run`
  reports it rather than promising a scan that would fail.
- Report Root and Meta drift. Nothing checked them, so a repository was aligned
  according to Verify and Guide while every Scope Change refused over the same
  bytes, and only one of those authorities named a file. Both now use the
  vocabulary the Code and Database Volumes already use, under the same condition
  a Scope Change refuses on.
- Give new repositories the line-ending protection AOCI applies to itself.
  `init` writes a `.gitattributes` normalizing text to LF, and never rewrites an
  existing one.
- Say when the base Go toolchain is older than `go.mod` requires, instead of
  reporting it as a supply-chain fault. The `licenses` and
  `opengauss-connector` gates pin `GOTOOLCHAIN=local` so the audit reports one
  Go identity, and under that pin an older base install stops them — but
  `check-opengauss-connector.sh` wraps every `go mod download` failure into
  "could not download the pinned upstream module", so a base install one patch
  behind read as a compromised or unreachable upstream. `make full` now
  compares the base toolchain against the `go` directive first and, when it is
  older, names both versions, explains why a plain `go version` can disagree,
  and offers either installing the newer Go or pointing `GO_BIN` at one already
  on the machine. The comparison is `>=`, so a newer base install is never
  blocked.
- Show the confirmation prompt before the phrase is read, not after the command
  has exited. The library entry point buffers both writers into memory and
  flushes them once Execute returns, so every TTY digest confirmation wrote its
  prompt into a buffer and then blocked on stdin with nothing on screen. The
  prompt carries the exact phrase the operator has to type, so the one thing
  they needed was the one thing they could not see: they typed blind or gave up,
  and the phrase appeared only alongside the failure. The prompt now goes
  straight to the process stderr the confirmed branch has already proven is a
  terminal. Every `scope approve`, `scope safety approve`, `baseline scope
  approve`, `cognition bootstrap`, and `cognition migration` confirmation is
  affected.
- Let an approval land in a file instead of being carried by hand. `scope
  approve`, `scope safety approve`, and `baseline scope approve` take
  `--out-file`, so the artifact `scope apply --approval-file` needs is written
  where it is wanted rather than printed to stdout for the operator to
  redirect. Forgetting that redirect silently discarded a confirmation that
  cannot be reused. The path is checked before the human is asked, so a bad
  path never costs a confirmation; the file is created only when nothing is
  already there, and is readable by its owner alone, because until the change is
  applied anything that can read it can stand in for the human who typed the
  phrase.
- Give a Managed Scope posture relaxation the reviewer it always needed. Auto
  authorization correctly refuses to let a posture ratify its own weakening, but
  refusing was only half the contract: `interaction_required` was derived from
  the weaker desired mode, so desiring `auto` also decided that no reviewer was
  needed and the blocked change had no approver at all. Once left, `auto` could
  not be re-entered by any governed path. A relaxation now reports
  `interaction_required` and is authorized under the posture the current
  Baseline receipt proves, so exactly one `scope approve` ratifies it while
  `policy_bound_auto` still refuses it and ordinary auto plans stay fully
  automatic.
- Stop counting a deleted file's stale Baseline record as a cognition coverage
  reduction. A path the current Safe Inventory no longer sees has neither an
  Entry nor source bytes left to lose, so retiring its record is bookkeeping;
  excluded paths remain in the evaluation, so an existing source that still owes
  an Entry is unaffected and keeps raising P1. Previously one such record — a
  file deleted three weeks earlier — forced a pure policy change to high risk and
  demanded a human approval that protected nothing.
- Say what a Managed Scope confirmation actually approves. The prompt now leads
  with the effects that matter — cognition Entries removed, files losing Index
  coverage, a weakened posture, a relaxed budget, high-risk content admitted —
  and states plainly when a change carries none of them. A confirmation phrase
  binds the approval to one exact plan; it was never meant to be the only thing
  the approver could read.
- Index the three MCP black-box harnesses. They are executable contracts run by
  name with their own preconditions, which is exactly what the admission rule
  admits, and each Entry now carries the precondition a reader needs: conformance
  expects an established repository and zero formal writes, scenarios writes only
  disposable fixtures, and lifecycle needs Docker for the database suite while
  `repo-c` reaches aligned without drift. The earlier exclusion rested on a wrong
  cost model — an Entry is about 110 tokens of FRAS, never the Python body — so
  its recorded reason is corrected rather than deleted. Three Entries cost about
  700 tokens against a 120k target.
- Managed Scope approval mode is now `review`: applying the harness change
  required independent human review, because any Scope Change re-evaluates every
  role and a Baseline record for a file deleted in `82120db` therefore appears as
  a coverage reduction. Policy-bound auto authorization correctly refused it.
- State the repository's verification obligations and its Whole-Index admission
  rule in `AGENTS.md`, where every Agent session reads them. The gate table maps
  a kind of change to the gate that actually covers it, and it records the fact
  no gate output carries: `clean-room-smoke`, `licenses`, `race`, `vuln`, and
  `database-integration` run only under `make full`, so a green `make fast`
  proves nothing about them. The admission rule states that a test earns an
  Entry only when it is an executable contract run by name with its own
  preconditions; ordinary package tests stay `observe`, and a test that locks a
  fact is recorded in the locked object's `S`. A cross-layer test now fails when
  any `*_test.go` acquires an Entry and when either rule is dropped from
  `AGENTS.md`.
- Have `aoci init` add the agent host configuration it just wrote to the
  repository's `.gitignore`. Those files carry machine-bound absolute paths, and
  a committed copy silently no-ops another machine's `init` because every
  installer detects an existing entry by key presence. The timing is what made
  this worth fixing in `init` rather than in prose: Managed Scope roles are fixed
  by the first `scan`, `scan --force` cannot advance them, and removing a path
  afterwards is a coverage reduction that needs an approved Scope Change — so a
  default `init` then `scan` used to leave a machine-bound file as a permanent
  authoring target. `init` appends under its own marked block, never rewrites
  maintainer content, skips any path Git already tracks, and does nothing when no
  host configuration was written.
- Split the one-step setup instruction in both public READMEs into the two turns
  it always was. The index is authored through AOCI's MCP tools, and the server
  `init` writes is not loaded in the session that wrote it, so the old single
  prompt asked the Agent to finish work it could not reach. The first
  instruction now runs `init` and `scan`, asks for any configuration a host does
  not write itself, and stops with an explicit restart hand-off; the second
  confirms the MCP server and builds the index. `scan`, which establishes the
  Baseline every later step needs, was missing from the one-step path entirely.
- Say in both READMEs how to tell which AOCI a host is actually connected to:
  `cognition_receipt.mcp_service_version` and `runtime_repository_root` from any
  Overview `check_only` response or any Maintain response, and the `command` in
  the project's `.mcp.json` or the equivalent host configuration for the binary
  path. Replacing bytes on disk does not change a running MCP process.
- Carry the `aoci scope acknowledge` remediation in a blocked Volumes Guide whose
  Observe evidence is pending review. `observe_change_policy` defaults to
  `review_required`, so an ordinary edit to any Observe-role file blocks
  authoring; the Legacy Plan already routed that state to acknowledgement, while
  the Volumes Guide reported a bare `observed_pending` finding with no command.
  The response now adds the scope status and acknowledge commands, an
  `observed_pending` stop with cause and safe next action, and the instruction to
  review the reported evidence before acknowledging.
- Show the index header both public READMEs previously only described: the Root
  manifest that declares each participating Volume with its kind, path, format,
  dependency, and activation state, and the Meta header that carries the object
  protocol, the FRAS discipline, the machine limits authority, S admission, and
  the S quota. Each locale quotes what its own `aoci init` writes, and a
  cross-layer test binds every quoted line to the rendered template and the
  machine S quota, so a template or limits change cannot leave the published
  example describing a header the binary no longer produces.
- Correct published facts in the release-facing READMEs: the status badge named
  the superseded `v0.1.0-rc3`, the black-box suites were advertised as 44
  conformance checks, 22 fault-injection scenarios, and two lifecycle fixtures
  when they are 46, 30, and three, the English documentation map linked the
  Chinese Windows host-agent original instead of its English rendering, and the
  one worked FRAS example separated `A` items with semicolons where the public
  contract and every shipped Entry use commas.
- Make the conformance check count a property of the suite rather than of the
  repository it is pointed at. The Overview chain is tallied as two aggregate
  checks instead of two per chunk, and the probe pair is graded unconditionally,
  so the published number no longer moves when an index crosses a chunk boundary
  or a probe is not issued, and `AOCI_REPO` runs against a foreign target no
  longer look for this repository's documents.
- Carry the issue #8 `aoci cognition bootstrap` correction into the Chinese
  README, which had kept the pre-fix wording: bootstrap never targets an
  initialized Volumes v1 repository, and a Volumes skeleton with zero Entries is
  established through `aoci scan`, then Guide and no-argument `aoci_maintain`.
- Make the black-box suites enforce their own published numbers. Conformance and
  scenarios now fail when the count printed by the run disagrees with any
  document that advertises it, and the lifecycle suite fails when a committed
  fixture is not named where the suites are advertised, so these counts cannot
  drift silently again.

The first public availability date for v0.1.0-rc5 is 2026-08-17.

## v0.1.0-rc4

- Add a narrow openGauss 6.0.5 LTS A/PG Schema Evidence collector, backed by a
  reproducibly patched official Connector v1.0.8, strict remote `verify-full`
  TLS, fail-closed unsupported catalog handling, and disposable real-engine
  acceptance without changing Evidence v1 or the nine-tool MCP surface.
- Expand the fixed general-purpose Code and Database starter A/B dictionaries,
  make all C importance digits from 1 through 9 available, retain optional D
  grammar and the existing E scale, and use `EG7T` as the starter Code example.
- Reserve starter `G` for genuinely cross-domain objects and `Z` for understood
  objects that fit no named category; evidence gaps do not become `Z` or S
  constraints. The new defaults affect only fresh initialization and do not
  migrate or retag repositories with an existing formal Meta.
- Report exact Code candidate binding mismatches and distinguish
  `code_plan.batch_id` from the cross-domain `authoring_batch.batch_identity`
  without changing the nine-tool MCP surface or request Schema; actual source
  drift remains a stopped replan condition instead of a copied-field repair.
- Expose the aggregate Check command in an authoring-required Volumes Guide and
  close the final successful batch through Verify, Aggregate Check, and Guide
  while preserving intermediate-batch and Legacy Entries Stage behavior.
- Return the same Verify, Aggregate Check, and Guide closure directly from a
  successful final Volumes Apply, while leaving paged, Legacy, and Cognition
  Optimization actions unchanged.
- Advance an already-managed Root fingerprint in the same Database Cognition
  Bootstrap Baseline postimage as the Root descriptor update, and narrowly
  reconcile the canonical historical state left by earlier Bootstrap versions.
- Add a session cognition line to `aoci_search`, `aoci_get_entries`, and
  `aoci_header`, and an optional two-question cognition probe on a
  `check_only` Overview, so a Host can tell whether it still holds the
  delivered Whole-Index instead of re-requesting it. The line is computed from
  session facts with no repository scan, receipt identity drift never raises
  it, and the probe measures recall without advancing the refresh generation.
- Grade the Whole-Index Attestation as assimilation rather than verbatim
  recall: the Challenge passes at 80 percent or better with at most one object
  identity miss, core F is judged by normalized token similarity that splits
  Han, Kana, and Hangul into character bigrams so every Locale meets the same
  floor, and object identity and Tag stay exact. `fail` is reserved for a
  foreign envelope or no correct ordinal; every other shortfall records
  `partial`, so an honest complete-coverage claim can no longer grade below
  the same answers submitted with a hedged one.
- Accept `host_delivery_confirmation` and `model_cognition_attestation` in one
  call or in separate calls in either order. Both halves bind the same
  delivered body, so the session remembers each half per body and grades the
  merged evidence; an explicitly carried half supersedes the remembered one,
  and a fresh complete delivery resets that memory.
- Size one authoring batch for the Host tool-result window. The team key
  `code_cognition_batch_entries` (machine default 20, wire ceiling 200) sets
  how many candidates one Maintain asks the model to author inline, and
  Maintain keeps per-item governance enumerations to a leading sample with
  complete counts under `governance.list_truncation` and `sets.review_total`.
  Candidates, plans, and receipts stay complete, `aoci verify` and `aoci check`
  still list every item, and the Database Evidence byte gate defaults to
  64 KiB.
- Return structured repair findings instead of an internal error when an
  enforce-mode cognition budget rejects an Entry: every violated field carries
  `candidate_index`, `field`, `actual_tokens`, and `max_tokens`, whole-index
  excess and violations outside the batch stay a batch-level stop, and a CJK
  `S` field that crosses its token band is now a locatable repair.
- Accept model-authored `R` exactly as written: AOCI never checks one Entry's
  relations against another Entry, so a relation whose target is missing,
  unmanaged, scheduled for a later batch, or ambiguous by bare name is
  persisted unchanged and produces no Finding. `aoci_update_entry` no longer
  returns `impact_relation_unresolved`, `impact_relation_ambiguous`, or
  `impact_relation_invalid`; per-Entry FRAS structure, the tag dictionary, the
  C-driven S quota, source binding, and the projected budget stay enforced,
  and a newly authored or changed relation must still use a canonical `code:`
  or `database://` identity.
- Stop letting `R` reschedule a Code authoring batch: a submitted batch is no
  longer answered with a zero-write `code_candidate_relation_replan_required`
  replacement plan, so a repository whose Entries reference each other across
  batches, including a mutually-referencing cluster larger than the machine
  Code batch size, now reaches `aligned` in the ordinary rolling batches
  instead of replanning. Receipts issued before this change still load, so a
  plan already in flight survives the upgrade.
- Keep `R` references from other Entries from blocking ordinary
  `aoci_remove_entry` orphan removal, which no longer fails with
  `remove_orphan_relation_still_valid`, and from making a legacy `aoci.txt`
  Index `ineligible` in `aoci cognition migration snapshot` when one relation
  names a path that is gone. Any dangling annotation left behind stays
  model-owned until the model's next Whole-Index read; orphan proof, Guard,
  exact preimages, and the existing Entries recovery path are unchanged.
- Report `R` problems only from the Entry line itself: `aoci index entries
  check` still warns about that line's own form, such as an empty item, a
  placeholder mixed with real targets, or a full-width comma, and those
  warnings still never reject an Entry; it no longer resolves targets on disk
  to warn that one is missing, duplicated, self-referencing, or not a regular
  file, and `aoci cognition system relations` reports no relation `findings`.
  The hard safety gate on the actual write `path` is unchanged.
- Carry the `aoci scan` remediation in a blocked Volumes Guide that has no
  Baseline: the response adds the `scan` command, a `baseline_missing` stop
  with cause and safe next action, and the instruction to author nothing
  before a Baseline exists. An initialized Volumes repository with zero
  Entries is completed through scan, Guide, and no-argument Maintain;
  `aoci cognition bootstrap` governs only an uninitialized repository or the
  exact zero-Entry Legacy minimal skeleton.
- Roll a partially written `[code,database]` receipt batch forward when the
  identical evidence-bound candidates are resubmitted, so an interruption
  between the Code and Database writes finishes the remaining Volume instead
  of stopping at `code_candidate_plan_stale` behind a pending
  `.aoci/transactions` receipt; the roll-forward still requires a version-4
  recovery receipt proving that Volume's own postimage, and the fixed
  Code-then-Database order, `[code]`, and `[database]` batches are unchanged.
- Carry the plan-time Curation exclusions in the managed-scope-change envelope
  as `curation_exclusions` and replay them when verifying after publication,
  so a reviewed `curation.json` exclude decision for an already baselined path
  applies and archives as one transaction instead of leaving a complete
  transaction that can never be archived; envelopes without the field keep
  recomputing exclusions from the current Baseline, and `envelope_version` is
  unchanged.
- Keep the partition facts of mid-level tables when linking multi-level
  PostgreSQL partitioning: a table that is both a partition and a partition
  parent now records `parent_object` and `bound` while retaining
  `partitioned`, `method`, `expression`, and any `child_objects` already
  linked to it, instead of being rewritten as a non-partitioned leaf; the
  narrow non-partitioned openGauss profile and Evidence v1 are unchanged.
- Name the cause when `aoci init` stops with
  `managed_scope_auto_authorization_blocked`: the bilingual message now
  separates tracked paths excluded by a built-in safety rule (up to five
  named, the rest as a +N remainder) from a profile that assigns no path to
  the index role and from configured exact high-risk opt-ins, pointing the
  first two at `aoci scope safety` and `--scope-profile production`; `--json`
  reports `error_code` as that machine code instead of the generic `config`,
  and the exit code is unchanged.
- Record the effective apply authorization mode in the Managed Scope Baseline
  receipt as `apply_authorization_mode`, and raise
  `approval_policy_relaxation` when a Scope Change runs under a mode weaker
  than the one that receipt records; that risk is `high`, blocks
  `policy_bound_auto`, and forces interaction under `legacy`, so a team's
  review posture can no longer be lowered and self-ratified inside the same
  transaction. A receipt written before this field is not retroactively
  blocked, so the guarantee starts at the first receipt that records a mode;
  an unrecognized recorded mode fails closed.
- Accept a Managed Scope Baseline receipt recorded under the opposite
  filesystem case semantics when every managed path provably takes the same
  role and the same fingerprint participation under both, so a Baseline
  established on a case-sensitive checkout no longer stops a case-insensitive
  one with `scope_change_required`. The receipted value becomes the reported
  `desired_policy_identity`; any real case divergence leaves
  `alternate_policy_identity` empty and keeps the stop.
- Route every read-only Git query against a scanned repository through one
  hardened invocation that disables `core.fsmonitor`, `core.hooksPath`, and
  `core.pager` on the command line, so a target repository's own `.git/config`
  can no longer point those reads at a program for Git to run while it walks
  the working tree. Safe Inventory collection and business-source manifest
  building both use it; file listing still passes `core.quotepath=false`,
  every call still sets `GIT_OPTIONAL_LOCKS=0`, and the reported tracked,
  untracked, and ignored facts are unchanged.
- Stop the Windows-only non-atomic overwrite fallback of `AtomicWrite` from
  destroying the previous content by renaming the target to a same-directory
  backup before the retry, restoring that backup when the retry fails, and
  keeping both copies with both paths named in the error only when even the
  restore fails; the fallback still runs only after a normal atomic rename
  has failed and never on other platforms.
- Pin `* text=auto eol=lf` in `.gitattributes` so a source checkout is
  byte-identical on every platform: the Windows default `core.autocrlf=true`
  can no longer rewrite tracked text to CRLF, so a Windows checkout still
  matches the raw-byte Baseline instead of leaving `aoci_maintain` blocked on
  the whole tree. Every tracked text file is already LF, so the
  repository-wide renormalization changes no committed bytes.
- Report `service_binary_replaced_on_disk: true` in the Volumes
  `aoci_maintain` result, `aoci_rules`, and the final Overview metadata when
  the service binary's on-disk size or mtime no longer matches what the
  running process started from, so restarting the host MCP integration
  becomes a machine fact instead of manual diagnosis. The fact is advisory:
  it blocks nothing, is absent when there is no drift, counts a vanished
  binary as replaced, stays off when the startup probe records no identity,
  and does not change the nine-tool MCP surface.
- State where the running version and binary path come from in the Runtime
  Rules and the AGENTS integration block: the service version is
  `cognition_receipt.mcp_service_version` in any `check_only` Overview or
  Maintain response, the binary path is the `command` in the project's
  `.mcp.json`, and the CLI need not be on `PATH`, so a missing shell command
  never means AOCI is absent.
- Add three standalone black-box suites under `scripts/blackbox/` that drive a
  built `aoci` binary as a real stdio MCP client and never import internal
  packages: `mcp_conformance.py` makes 58 read-only checks of the handshake,
  the nine-tool registry, input Schemas, response shapes, and malformed input;
  `mcp_scenarios.py` runs 30 fault-injection scenarios for cursor replay and
  tampering, write-lifecycle rejection, crash injection, and racing writers
  over disposable fixtures; and `mcp_lifecycle.py` takes three frozen fixture
  projects, including a 453-object one, from `init` through incremental
  maintenance, Database Evidence acceptance, schema drift, multi-batch
  authoring, and re-alignment, with an optional real-agent model track. Every
  suite now also asserts that no non-Overview tool response exceeds 64 KB
  under default configuration, so a response can never grow past what an
  ordinary Host displays inline. All three need only Python 3, git, and a
  binary, honor `AOCI_BIN`, and ship with a repository clone rather than with
  Release archives.
- Correct the public delivery contract in
  `spec/public/aoci-overview-delivery-v1.txt` and `docs/overview-delivery.md`:
  an exact replay of a genuine cursor idempotently re-serves the identical
  Chunk bytes, and because a cursor is re-derivable from its bound facts
  alone, an unchanged Index and `chunk_tokens` accept the same cursor across
  MCP process restarts. Invalid, missing, reordered, and cross-chain use
  still fails closed, `overview_snapshot_changed` and
  `overview_chunk_tokens_changed` still require a restart at Chunk 1, no
  delivery Session or transaction is persisted, and the Volumes governance
  binding remains in-memory session state that does not carry across
  processes.
- Correct the `aoci_remove_entry` description and the Volumes and system
  cognition Specs to state the behavior the machine actually has: an R
  reference from another Entry never blocks orphan removal, a dangling
  annotation left behind is model-owned semantics handled on the next
  Whole-Index read, and relation content produces no Finding, so `findings`
  stays in the public shape and stays empty. Removal and projection behavior
  is unchanged.
- Document the shipped `meta` scope of `aoci_overview` and the additive
  `aoci_maintain` input `intent=cognition_optimization` with its optional
  `object_refs` filter of canonical `code:` references in
  `aoci-cognition-volumes-v1.txt`, and cross-reference that intent from
  `aoci-database-cognition-authoring-v1.txt`, where it never applies to
  Database assessment. `project` and `meta` each deliver Root + Meta, no MCP
  tool is added, and ordinary no-argument Maintain is unchanged.
- Renumber the first-section items of the `aoci_rules` runtime-rules contract
  from `5.`-`8.` to `4a.`-`4d.` and prefix the `en-US` section headings with
  `Section `, so an item number no longer collides with the deliberate global
  `5.`-`18.` sequence of the later sections or with a section number. Item
  bodies stay byte-identical in both official locales.
- Stop the `aoci cognition` and `aoci database` group help from claiming
  read-only behavior neither group has: `cli.short.cognition` now names
  governed Apply workflows next to layout planning, since the group carries
  `aoci cognition bootstrap apply` and `aoci cognition onboard apply`, and
  `cli.short.database` scopes its read-only claim to database access, since
  those workflows write local Schema Evidence and advance the evidence
  Baseline. Both official locales change together; no command, flag, or
  runtime behavior changes.
- Correct the Git claim in `docs/install.md` and `docs/supply-chain.md`: the
  binary starts and non-Git directories are scanned without it, but Safe
  Inventory invokes the host `git` executable in any root holding `.git` for
  tracked and ignored authority and fails closed with
  `safe_inventory_git_unavailable` when that executable is absent. GitHub CLI,
  Cosign, Go, and SBOM readers remain verification-only with no runtime
  dependency.
- Document that the host configuration written by `aoci init --agent <name>`
  (`.mcp.json`, `.claude/settings.json`, `.codex/config.toml`,
  `opencode.json`) embeds machine-bound absolute paths and belongs in
  `.gitignore`, and add a moved-binary-or-repository troubleshooting entry:
  the Claude and Codex installers detect an existing entry by key presence,
  so re-running `init` keeps the stale paths and `aoci doctor` still reports
  those integrations as installed while the server fails to start, whereas
  OpenCode fails closed on an `mcp.aoci` conflict; recovery is to remove the
  stale `mcpServers.aoci`, `[mcp_servers.aoci]`, `mcp.aoci`, and `PreToolUse`
  entries and re-run `init` from the new location.
- Scope the Windows Host-Agent guide to the layout it describes, marking its
  Entries/Header/Curation Stage sections Legacy, routing Volume-first
  repositories to the live Guide with ordinary no-argument `aoci_maintain` and
  `aoci_update_entry`, replacing the old `aoci_overview` size-threshold
  fallback with the current `continuation_required` chunked delivery, and
  recording the evidence-driven automatic replan, Resume, and Rollback
  closures for a `stopped` write. The "F length is never machine-blocked" rule
  now covers Legacy v1 Entries only, because FRAS v2 hard-limits a Volumes v1
  object's `F` to 160 runes.
- Add English renderings of three Chinese-only documents:
  `docs/contract-authority.md`, `spec/public/s-field-discipline.en.txt`, and
  `docs/windows-host-agent.en.md`, reachable from `AGENTS.md`,
  `spec/public/README.md`, `docs/troubleshooting.md`, and a new pointer in
  `docs/windows-host-agent.md`. The Chinese files remain the normative
  originals; the renderings carry no machine-scanned declaration and open no
  second rule authority.
- Expand both public READMEs with the Volume-first layout, the three-stage
  workflow, cross-Agent and cross-session reuse of one Whole-Index, and a
  verbatim excerpt of the starter tag dictionary with a worked reading of one
  compact tag; each repository's formal Meta remains authoritative.
- Build every gate and the signed release from the single `go.mod` toolchain
  directive, now `go1.26.6`, so release archives carry the same patched
  standard library that fast CI, full confidence, and the rehearsal verify.
- Preserve the nine MCP tool names and their stable identity, FRAS v2, and the
  existing Index and Baseline formats; every response change in this release
  is an additive field.

The first public availability date for v0.1.0-rc4 is 2026-08-15.

## v0.1.0-rc3

- Add an explicit `cognition_optimization` intent to `aoci_maintain` so a user
  can ask the model to review already-aligned Code Entries without source
  drift, while keeping ordinary maintenance behavior unchanged.
- Select optimization candidates deterministically from current Entry cost and
  C-band budget facts, require complete Entry submissions, and preserve the
  rule that AOCI itself does not generate, truncate, or compress semantics.
- Keep unchanged optimization submissions free of formal Index or Baseline
  writes, and reuse the existing atomic update, Baseline, Managed Scope,
  Recovery, and checkpoint paths for replacements and retries.
- Fix explicit Volumes Code and all-scope maintenance routing without changing
  Legacy or Database Cognition boundaries.
- Improve the bilingual public README, release-first one-step installation
  guidance, and self-contained Release archive branding.
- Preserve the nine MCP tool names and their stable identity, FRAS v2, and the
  existing Index and Baseline formats.

The first public availability date for v0.1.0-rc3 is 2026-08-10.

## v0.1.0-rc2

- Improve evidence-backed S-field authoring guidance for high-importance
  cognition objects while preserving `S:-` when no qualifying constraint exists.
- Prevent neighboring Entries from determining the current object's S field,
  and add bilingual authoring-contract and compatibility coverage.
- Make signed-package installation and verification links usable from Release
  archives, and clarify source-build versus signed-binary version identity.
- Preserve the existing public Specs, FRAS v2 machine contract, and nine-tool
  MCP surface.

The first public availability date for v0.1.0-rc2 is 2026-08-10.

## v0.1.0-rc1

- Establish the public AOCI-CODE CLI and MCP runtime under the canonical Go
  module `github.com/aoci-spec/aoci-code`.
- Preserve the `aoci` binary and the reviewed nine-tool MCP contract.
- Publish the public runtime contracts under `spec/public/`.
- Add public build, test, security, integration, and supply-chain guidance.
- Prepare the FSL-1.1-MIT legal assets for authorized public distribution.

v0.1.0-rc1 was first made publicly available on 2026-08-08.
