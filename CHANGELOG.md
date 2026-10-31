# Changelog

All notable public changes to AOCI-CODE will be documented in this file.

## Unreleased

The 0.2.0 line. Product changes land here while v0.1.0 soaks; an entry that
changes a persisted preimage names the binary every machine then needs.

- `aoci --help` lists the five everyday commands first (`init`, `scan`,
  `mcp`, `verify`, `check`) under Everyday Commands and the rest under
  Additional Commands, and `aoci scope --help` sorts its seventeen
  subcommands into Policy, Inspect, and Transactions; the group titles are
  locale text assets like every other help string. `docs/getting-started.md`
  is rewritten as the first week in three steps: install and initialize,
  connect the Agent and build the index, keep it aligned.
- Incremental Overview delivery (L5) is implemented. `aoci_overview` takes
  `delta_since`, the `scope_identity` of the delivery the Host holds: after a
  passing Attestation and a probe pass over the current identity, the server
  answers `delivery_mode=delta` (or `chunked_delta`) with only the objects
  changed or added since that body, at their current ordinals, the removed
  identities in Metadata, and a Challenge drawn from the delivered objects
  that still echoes the complete identity facts; a passing delta Attestation
  re-anchors the base and a partial or failed one drops it; an unchanged base
  answers an empty delta without a proof. Without a base in the process, an
  unreliable base, a pending `context_compaction`, no probe pass, a delta
  past half the objects, or a delivery the server cannot retain, the call
  answers the complete scope with a named `fallback_reason`. The capability
  manifest lists `overview_delta`; scenario group M (six scenarios) and two
  conformance checks pin the contract's list; the suites now publish 92 and
  62.
- Spec: incremental Overview delivery (L5) was specified in the delivery
  contract first: an ordinary `aoci_overview` carrying
  `delta_since=<scope_identity>`, after a passing Attestation and a probe
  pass over the current identity, delivers only the objects changed, added,
  or removed since that body under the same framing and Chunk rules, is
  attested over exactly that delta, re-anchors on a pass and drops the base
  on a partial or failed Attestation, and answers the complete scope with a
  named `fallback_reason` without a base in the process, without the probe
  pass, during a compaction reload, or past half the objects; the
  conformance checks the implementation must ship are listed. No behaviour
  changes in this version.
- Parallel current batches (L4) are implemented. Under the new team key
  `code_cognition_parallel_batches` (absent or 1 is the one-batch flow, byte
  for byte; ceiling 4; `config set` validates it; the capability manifest
  lists `parallel_batches`) one logical plan keeps up to N sibling batches
  outstanding: Maintain delivers the first as the current batch and lists the
  others under `code_plan.siblings`, `aoci_maintain` takes a sibling's
  `batch_id` and delivers it whole, and `aoci_update_entry` accepts the
  siblings in any order. A sibling's Apply is accepted when the plan preimage
  plus the applied siblings, replayed through the editor from the records
  each Apply leaves under `.aoci/drafts/code-cognition`, reproduce the
  current Code Volume; a second Apply arriving under the lock reprojects its
  Entries onto the first's postimage and succeeds. Any other change to the
  Volume, or to the Root, Meta, Database Volume or Scope identity, invalidates
  every outstanding sibling, and one target's source drift invalidates only
  the sibling holding it. `remaining` counts the targets no sibling holds.
  Scenario group L (six scenarios) and two conformance checks pin the
  contract's conformance list; the suites now publish 86 and 60.
- The Legacy `baseline scope` refresh is gone: `aoci baseline scope plan`,
  `preview`, `approve`, `apply`, `status`, and `resume`, the
  `internal/scoperefresh` package, and the six `baseline-scope-*/v1` artifact
  versions, which the capability manifest and the release manifest no longer
  list (`baseline_scope_refresh` leaves the planner, apply, and lifecycle
  capabilities; `ordinary_legacy_scope_reduction` leaves the terminal-approval
  lists). Every repository with a Managed Scope Baseline has answered
  `baseline_scope_managed_scope_unsupported` since rc20 and `scope activate`
  establishes Managed Scope for one without it, so no working route is lost.
  A `scope-<id>.json` intent of the retired family left under
  `.aoci/transactions` reads as an invalid scope receipt; move it out.
- The repository's own index has quality floors. `scripts/check-index-quality.sh`
  takes `aoci --json index score` and holds it to
  `scripts/index-quality-floors.txt`: no violation in any of the nine
  dimensions, at most 8 percent of C7+ Entries without an S, a healthy
  Whole-Index budget. CI runs it on every push (`contracts-and-safety`), a
  closure runs it after governance, and the ladder records it as
  `index-quality`. A floor only tightens without a reason in the commit
  message.
- `index score` reads a Cognition Volumes repository. Its nine dimensions
  keep their names and JSON shape; on Volumes they are projected from the
  governance facts `verify` and `check` report, so coverage and freshness
  count the Code drift (a held source is reported, never counted), token is
  the Whole-Index budget, escale is the E scale audit, and Code and Database
  objects are judged under their own Meta dictionaries. The JSON adds
  `layout`, `volumes`, and `budget`; a Legacy score is byte for byte what it
  was. `status --deep` stays Legacy-only.
- A quarterly dependency review joins the run-by-name ladder
  (`dependency-review`, the new `quarterly` cadence, overdue after 92 days).
  `scripts/dependency-review.sh` is a read-only report: the go directive
  against go.dev, every direct Go module against the module proxy, the
  openGauss connector pin against the tags upstream publishes, every GitHub
  Action pin against its newest release and the commit its comment claims,
  and the open dependabot proposals. It upgrades nothing; a bump stays a
  decision taken with its gate.
- A Maintain round issues more candidates on the same transport budget
  (L3; #127 in part). Measured on real trees under the default 24 KiB budget,
  candidates a round before and after: RuoYi 22 to 28 on a process's first
  round and 41 after it, excalidraw 24 to 34 and 47, a 13,850-file tree 20 to
  26 and 39; a first build of RuoYi (512 targets) takes 15 rounds where it
  took 29, excalidraw (798 targets) 19 where it took 34, no round over the
  budget. Four changes to the response and none to what a batch
  binds: a Code candidate no longer repeats `batch_id` (the batch is
  `code_plan.batch_id`, which `aoci_update_entry.code_batch_id` already
  names; Database candidates keep theirs, and a reader of
  `candidates[].batch_id` for Code reads `code_plan.batch_id`); the authoring
  contract (`instructions`, `authoring_meta`) travels once per server process
  and refresh generation and later rounds carry `instructions_ref`, with the
  additive `aoci_maintain` input `include_instructions: true` bringing it
  back and a completed context-compaction reload bringing it back by itself;
  the governance block leaves the four per-asset records off the wire, which
  `cognition_receipt` and `code_plan` already carry; and the budget reserves
  `file_lines` and `expected_e` at the width they will have, taken from the
  fingerprint cache, instead of the widest form. A candidate now binds the
  digest the governance verdict's own snapshot holds instead of reading every
  target again each round. The Guide and the `scan` estimate project the
  first round's shape, so a later round in the same process may issue more
  than the Guide showed.
- Every governance call on a large repository is about three times faster
  (#127 in part; measured by the new `perf-track` contract, 13,850 files: a
  Maintain 3.8 s to 1.3 s, `verify` 3.6 s to 1.1 s, the Guide 3.8 s to 1.5 s;
  1,297 files: a Maintain 0.80 s to 0.59 s, `verify` 0.79 s to 0.42 s). Three causes, none of them the file reads
  everyone assumed: the Managed Scope policy was evaluated over the whole
  tree twice per call, once for the verdict and once more inside the
  business-source manifest, which now takes the state the verdict already
  loaded; glob rules were matched through the regular-expression engine, whose
  backtracking took half of every call on a starter policy (20 rules times
  every path), and are now matched by a token program judged against the
  regexp definition by a differential test over 100,000 pattern-path pairs
  and a fuzz target; and the curation profile re-read every candidate to
  classify it, and now takes the binary verdict and the line count from the
  fingerprint it already had. A stat cache under `.aoci/fingerprints.json`,
  git-ignored and outside every identity, answers an unchanged file by size
  and mtime under git's racily-clean rule; `AOCI_FINGERPRINT_CACHE=off`
  disables it, and the status page and `scan --dry-run` never write it.
- Spec: parallel current batches (L4) are specified in the Cognition Volumes
  contract for implementation in 0.2.0-rc2: a team key
  `code_cognition_parallel_batches` (absent or 1 is today's flow, ceiling 4),
  sibling batches cut from one logical plan against one plan preimage and
  delivered whole by `batch_id`, an Apply preimage rule relaxed exactly to
  "the plan preimage or an applied sibling's postimage" and proven by
  replaying the applied receipts onto the preimage bytes, invalidation of
  every outstanding sibling by any non-sibling change, and the conformance
  checks the implementation must ship. No behaviour changes in this version.
- Code drift of a Volumes repository is computed in one place,
  `volumegovernance.CodeDrift`, and every reader consumes it: Verify, Check,
  the Guide, Maintain, Overview, the post-Apply inspector that each
  `aoci_update_entry` result reads, and `scope status`. Before, `scope status`
  detected on its own with a trimmed set of walk options and counted held
  sources (empty, binary, oversize, unreadable, curation-excluded) as
  authoring targets, so a tree whose only Entry-less index files were images
  read `authoring_required` there and `aligned` everywhere else. Its `drift`
  now lists them as `skipped` and `curation_excluded`, its `authoring_targets`
  counts what Verify counts, and `drift_warnings` carries the detector's
  warnings. A contract test lists the files allowed to call the detector
  directly, each with its reason (the Legacy readers, the writer that binds
  the active business Baseline), so a new private reader fails the suite;
  two more pin `scope status` and the inspector to the projection on a tree
  with stale, missing, orphan, and held sources.
- Within one server process, a governance call over an unchanged tree no
  longer evaluates the Managed Scope policy or decodes the Baseline and the
  fingerprint cache again: the evaluation is remembered by a digest of every
  input (policy, walk options, curation exclusions, root layout, the walked
  paths), the two documents by the digest of their bytes, so a stale answer
  is impossible and the memo is bounded by the roots a process serves.
  13,850 files: a Maintain 1.09 s on a process's first call, 0.50 s once the
  process has seen the tree; an Overview check 0.27 s. `AOCI_PROCESS_MEMO=off` switches it
  off; `perf_calls.py` now times a third, steady Maintain, since the first
  one creates `.aoci/drafts` and the second therefore sees a new path.
- Black-box: `scripts/blackbox/perf_calls.py` times every governance call a
  user waits on (scan, cold and warm Maintain, Overview check, verify, check,
  Guide, status) against a tree's file count, and `realrepo.py` reports the
  wall time of each tool inside its authoring rounds and of the terminal
  proof; the `perf-track` ladder contract runs the former over five local
  clones from 700 to 13,850 files, so a performance change is read against
  the previous release's numbers rather than claimed.
- Black-box and CI: the conformance suite reports the four receipt-bound
  attestation checks as SKIP, counted but not failing, when the host
  repository is not governance-aligned, instead of four false reds on every
  dirty tree; the scenarios page probe retries a timed-out connection once;
  the PostgreSQL integration job binds a free loopback port instead of 5432;
  `fast-ci` runs on pushes to `v*-line` branches; `full-confidence` splits
  the 55-minute `linux-full` job into `linux-full` (the gate and coverage),
  `linux-blackbox` (suites, upgrade axis, migration drill), `linux-nightly`
  (longrun, fuzz) and `linux-bench`.
- `logs` and `run` are no longer built-in runtime exclusions (#158, the
  second finding). The built-in rule keeps only names that never denote
  source (#100), and both do: `internal/logs` is a package, `cmd/run` a
  command, yet through v0.1.0 every path component of either name was hard
  excluded with no door. Log, PID and socket files stay excluded by suffix,
  `pids` and the PM2 and database runtime names stay built in. New
  repositories exclude a root `logs/` or `run/` through `exclude_root_dirs`,
  which `init` writes; an existing repository keeps its lists and its scope
  identity, and a tracked module of either name becomes a Missing Entry after
  the upgrade, authored like any new source or kept out by adding the name
  to `exclude_dirs` and running `aoci scope activate`. A git-ignored `logs/`
  stays excluded as before.
- SQLite's explicit file source can collect read-only catalog Evidence for
  ordinary rowid tables, columns, primary/foreign keys, and basic indexes
  (Refs #83). Inspect, snapshot, verify, Baseline acceptance, and onboarding
  use the existing Evidence lifecycle; enabled SQLite sources now require
  Evidence instead of remaining declaration-only. Unsupported schema features
  fail closed. No MCP tool or Evidence JSON version changes. All hosts using
  SQLite need this collector-capable binary; earlier 0.2.0 builds skip SQLite
  Evidence requirements, and rc22 and older reject the source declaration.
  The ncruces driver adds three release-linked modules without CGO. Read-only
  WAL collection may update SQLite's shared-memory coordination file.

- Built-in sensitive-name rules are a selectable set (#148). `v1` is the rule
  every release through rc22 applied: `.env` and `.env.<name>`, named
  credential files, private keys, `secrets.*`. `v2` adds the shapes
  operational repositories use for credentials: a file name with an `env`
  component after the first (`prod.env`, `manager-login.env`,
  `api.env.local`) and a credential word (`credential`, `secret`,
  `password`, `passwd`, `apikey`, `token`, `api-key`, and their plurals) as a
  whole word of a data or configuration file name (`api-credentials.yaml`,
  `token.txt`, `passwords.json`). Under `v2` a name ending in `.example`,
  `.sample`, `.template`, or `.dist` is a template whatever precedes the
  suffix, a source file such as `tokenizer.go` is never
  matched, and a directory keeps the `v1` classification. The set is the
  team configuration key `sensitive_name_rules`: `init` persists `v2` for a
  new repository; an absent key is `v1` and keeps every existing
  repository's scope identity; `aoci config set sensitive_name_rules v2` on
  an initialized repository is a governed Scope Change whose coverage
  reduction `scope activate` pauses for approval; `aoci doctor` lists the
  indexed paths `v2` would take out. A repository that carries the key needs
  this release on every machine: an older binary evaluates it under `v1`
  and answers `scope_change_required`. The Safe Inventory summary gains
  `sensitive_name_rules`; no MCP tool name, input schema, or formal-asset
  format changes.
- Overview continuation reuses a guarded immutable delivery (#132, whloong).
  The first Volumes request runs the full governance assessment once, frames
  the body, and records a guard over its inputs: configuration, Baseline and
  Curation bytes, the composite CognitionSet identity, Safe Inventory
  membership and Git status, the rules identity and blockers, and every
  index and observe source's raw digest and size. Continuation re-checks the
  guard instead of repeating policy evaluation and rebuilding the body, and
  the proof halves reuse the same delivery. Such cursors start with `v2:`
  and are bound to the MCP Host session: drift answers
  `overview_snapshot_changed`; expiry, eviction, a foreign Host, or a process
  restart answers `overview_session_unavailable_restart_required`, and the
  Host starts again at Chunk 1. Database-enabled repositories, a pending
  scope change, source warnings, and a delivery the server cannot retain
  keep the live path with the reconstructable v1 cursor. The chunk receipt
  stays `overview-chunk-receipt/v1`; Volumes responses gain
  `_meta.aoci_work`; an attestation whose answers use `object` or
  `object_ref` is refused by schema with a message naming `object_identity`;
  the Ledger records the delivery and proof events.
- The Legacy single-file layout is read-only. Its authoring surfaces are
  retired: the `ai` command group and the model endpoint client (the `ai`
  configuration block is still read for compatibility and used by nothing),
  `index build`, `index update`, `index entries`, `index header
  draft|diff|apply`, `index agent plan|stage|header|curation`, `doctor --net`,
  and the Legacy drafting, prompt, workflow, and run-manifest packages. On a
  Legacy repository `aoci_maintain` stops with one finding whose cause starts
  with `legacy_read_only_migrate_required`,
  `next_action=migrate_legacy_repository`, and `next_commands` carrying
  `cognition onboard start --json` and the Guide; `aoci_update_entry`,
  `aoci_remove_entry`, `aoci_report`, `update-entry`, and `remove-entry`
  answer that token and write nothing; `index agent guide` returns a blocked
  guide at that stage, naming any pending transaction receipt and its
  closure; `config set locale` and `init --locale` refuse to change the
  Locale a Legacy index was written in. Reads (`verify`, `check`,
  `status --deep`, `index score`, `index inventory`, `index header show`,
  Overview, search, get_entries, header) and the Legacy-to-Volumes migration
  keep their contracts, and a Managed Scope transaction still retires an
  Entry by disposition or carries the source-bound Entry candidate a stale
  Legacy source needs. A pending `header-*.json` receipt from a Legacy Header
  write is closed with the 0.1.x binary that wrote it; unfinished Legacy
  draft runs are inert. The nine-tool MCP surface, input schemas, and
  formal-asset formats are unchanged; `docs/upgrading.md` has the operator
  notes.
- The engine kernel is importable: `github.com/aoci-spec/aoci-code/engine`.
  `Open` resolves a repository the way the CLI does; `Cognition`,
  `Overview`, `Governance`, and `Identity` project the loader, the delivery
  framer, and the shared governance facts, so a Host's confirmed
  `body_sha256` and the library's digest are one computation; `Connect`
  hosts the nine MCP tools in process over an in-memory transport and
  `ServeStdio` serves them as `aoci mcp` does. `Version` reports the
  build-injected or module version, which now lives in `internal/buildinfo`,
  the ldflags target of every build. `examples/engine-embed` shows the
  embedding, including the openGauss connector replace an importer must
  carry until that connector is published as a module. Contract:
  `spec/public/aoci-engine-library-v1.txt`. No tool, schema, or asset
  format changes.
- A context-compaction reload that cannot fit no longer loops (#114). The MCP
  server watches for a fresh complete delivery started under a pending
  `context_compaction` that is followed by another `context_compaction`
  before it was proven; that interruption is the Host-capacity verdict.
  From then on, in that server session, a fresh request for a scope at least
  that large, made while a compaction refresh is pending, answers
  `delivery_mode=capacity_stop` with
  `fallback_reason=host_context_capacity_exceeded`: no body, the refresh
  generation consumed with an uncertain receipt and the source-bound delivery
  guidance, so the task continues without an unqualified complete-system
  claim. Explicit requests with no compaction pending, smaller scopes, and
  chains the Host already holds still deliver; scoped reads never count; the
  verdict is never persisted. `check_only` gains `delivery_plan` (the whole
  chain's estimated tokens, framed-body bytes, and chunk count) and
  `host_capacity`. Overview Ledger events gain `delivery_chain_id`,
  `chunk_index`, `chunk_count`, `delivery_completed`, `refresh_reasons`, and
  `host_delivery_status`, plus `overview_delivery_interrupted` and
  `overview_capacity_stop` events. The runtime rules, the AGENTS block, the
  Codex post-compaction instruction, and the Overview description state the
  stop; no tool, input schema, or receipt format changes.
- `tools/list` is 45% smaller (en-US 26.0 KB to 14.3 KB, zh-CN 23.7 KB to
  13.6 KB), a cost every session pays on Hosts that load tool schemas up front.
  Descriptions keep what a tool does, when to call it, its write boundary, and
  every contract phrase the tests lock; the procedure they repeated stays in
  `aoci_rules`, the Guide, and the final Chunk's `attestation_prompt`. The
  Overview members `cognition_receipt` and `probe_answers` are listed as plain
  objects while calls are still validated against their closed shapes, so a
  malformed receipt or probe answer is refused exactly as before; the nested
  attestation field descriptions moved to the prompt they duplicated, and the
  single-Entry compatibility fields of `aoci_update_entry` share the `entries`
  item descriptions. The remove-entry description no longer sends a
  non-orphan decision to `aoci_report`, which records nothing since 0.2.0. No
  tool, field, type, or validation rule changes.
- The session contract `aoci_rules` returns is 55% smaller (en-US about
  18.4 KB to 8.3 KB, zh-CN 15.7 KB to 7.7 KB), the other text every session
  reads before its task. Each rule keeps its effect and every phrase and token
  the tests and the manifest lock; what goes is repetition (the ordinal
  definition, the per-reason compaction narrative, the semantic-count
  breakdown, the projection's compatibility notes) whose authority already sits
  in the attestation prompt, the Overview responses, and the specs. The items
  are renumbered 1 to 11. The prescribed success and failure sentences, the
  capacity stop, and every stop condition of a stopped write stay.
- A new production repository starts with thirteen more starter `observe`
  rules, so its first build stops authoring Entries for material that gives no
  system cognition: `third_party/` and `third-party/` trees, `*.svg`, public
  certificates (`*.crt`, `*.cer`; private keys were already a built-in
  sensitive exclusion), and the lockfiles `package-lock.json`, `yarn.lock`,
  `pnpm-lock.yaml`, `Cargo.lock`, `poetry.lock`, `Gemfile.lock`,
  `composer.lock`, and `go.sum`, whose manifests keep the index role. Like the
  first seven, `aoci init` persists them only when no config exists, so an
  existing repository's rules, roles, and scope identity do not change; the
  first seven keep their ids and orders. The lifecycle suite's lockfile
  characterization becomes an assertion that `package-lock.json` is observed
  and never an authoring candidate.
- Migrating a large Legacy index no longer takes hours. Since the Legacy layout
  is read-only, migration to Volumes is how every Legacy repository continues,
  and its mapping template compared each Entry's field atoms by rescanning every
  range, so its cost grew with the cube of the Entry count: one
  prepare-apply-status pass took 47 s at 1,000 Entries, and a 10,000-Entry run
  was still in the template after seven and a half hours. One indexed pass with
  the same attribution rules takes 2.6 s and 28 s; no mapping, receipt, or
  result changes. The nightly benchmark comparison now covers the 10,000-Entry
  migration too.
- A new index no longer publishes the machine it was written on (#76, first
  proposed in #81 by dayebishouji). The first
  directory section of a Code Volume (or Legacy index) recorded the author's
  absolute repository path, often with a user name, and every later section
  continued it into the committed file. A writer creating that first section
  now records the neutral root `/<repository>`; every other section continues
  under it. Readers need no change: no checkout can sit under that root, so it
  resolves the way a clone at another path resolves its author's root, and the
  rc22 reader resolves it and its writer appends under it the same way. A
  repository whose path the header grammar could not spell back is no longer
  refused for that reason. An existing index keeps the root it records; the
  format spec and troubleshooting notes say which indexes carry which root.
- A person can approve a protected change in the agent's window. Approvals
  used to need a terminal: people did not know they had to open one and type
  a 64-character phrase, and saying no left them with an error and no next
  step. `aoci_maintain` with `intent=human_approval` and `approval_request`
  (`kind=scope_change` with the preview a paused `scope activate` saved, or
  `kind=onboarding` for the prepared Bootstrap or Legacy Migration review)
  shows the change through MCP form elicitation, described by AOCI from the
  saved preview or envelope, and records the person's Approve or Reject with
  an optional note for the agent. Approve writes an approval bound exactly as
  a terminal approval is, under the new mechanism
  `host_elicitation_confirmation`, where `scope apply` or `onboard apply`
  reads it, and returns that command; nothing is applied. Reject writes
  nothing and says what to do next. A stale preview is never shown; a host
  without dialogs, a dismissed dialog, and a terminal-only repository get the
  terminal commands. `scope activate`, a non-terminal `scope approve`, and
  `onboard prepare` now carry the call as `host_dialog_request`. The terminal
  prompt says Enter rejects and, on a rejection, what to do next;
  `cognition migration approve` and `cognition bootstrap approve` gain
  `--out-file`. The team key `human_approval_channel` (`tty_or_host_dialog`
  by default, or `tty_only`) decides whether a dialog answer counts, judged
  when Apply starts a transaction and never on Resume; it enters no scope or
  authorization identity. A repository with `safe_inventory_high_risk_opt_in`
  keeps Scope Change approvals in the terminal, and Migration reversal and
  rollback, the Legacy baseline scope refresh, and the high-risk pre-read
  approval stay terminal-only. The `aoci_maintain` input schema gains the
  `human_approval` intent and the optional `approval_request` object; the
  capability manifest gains `host_dialog`. An approval recorded in a dialog
  needs this release to apply.

- The Ledger says why a governed call ended where it did (#150). Maintain
  events wrote only `repair_required` or `error`, so waiting for authoring,
  waiting for an observe review, a refused candidate, and a recovery stop
  could not be told apart after the fact, and a failing `check` read as
  `result=ok` beside `exit_code=1`. Ledger schema 9 adds `governance_result`,
  `blocker`, `next_action`, `formal_writes_started`, and `batch_id` to
  Maintain, `update_entries_batch` (and `update_entries_batch_recover`, which
  reconciles Entries already in place), and `human_approval` events, and
  `check_passed` with the verdict to `check` and `verify`, whose `result` now
  only says the command ran. A refused candidate is recorded as
  `repair_required`. The fields are machine tokens; no source, Entry text, or
  connection detail is recorded, and no MCP response changes.
- A Host reviews changed Observe evidence without leaving MCP (#151). A
  Maintain blocked by `observed_pending` carries `observe_review`: each new,
  changed, or removed Observe path with the SHA-256 of its current bytes, a
  `review_digest` binding the complete list, and `confirm_request`, the exact
  `aoci_maintain` call that confirms it. After reading the files the Host
  calls `aoci_maintain` with `intent=observe_review` and
  `observe_review: {review_digest, reviewed_by}`. AOCI recomputes the list;
  when a listed file's bytes moved or the list changed, the call answers
  `status=stale` with the current listing and writes nothing. A current
  confirmation runs the transaction `scope acknowledge` runs, with the same
  Baseline, Ledger, CAS, Recovery, and approval rules, and answers
  `acknowledged` with the confirmed `review_digest`, the count, and
  `acknowledged_paths`; a posture that needs a person saves the preview under
  `.aoci/scope-change/` and answers `human_approval_required` with the dialog
  request and the terminal commands. A list longer than Maintain shows in full
  lists its leading items with `total` and `truncated` and has no MCP
  confirmation, since nothing may be confirmed unseen and the transaction
  acknowledges only the complete set; it keeps the `scope status` and
  `scope acknowledge` route. `scope acknowledge` is unchanged. The
  `aoci_maintain` input schema gains the `observe_review` intent and object;
  no tool is added and no formal-asset format changes.
- A final Volumes Apply whose Entries were already in place no longer calls
  the batch repeated. Resubmitting a stale Entry's identical text is a first
  submission that writes no formal bytes and advances the Baseline, yet
  `aoci update-entry` printed "Repeated batch" and the `aoci_update_entry`
  result's `next_action` named "the repeated final Volumes batch". Both now
  say the Entries were already in place; the `next_action` adds that they
  come from an earlier Apply or an unchanged resubmission and that the
  Baseline binds the current sources. Status, counts, `duplicate_applies`,
  and Ledger events are unchanged.
- The MCP server no longer writes the team configuration. Started on a
  repository whose configuration predates the Locale field, or that has a
  Legacy index and no configuration, it wrote `"locale": "zh-CN"` into
  `.aoci/config.json`, which drifted the bytes a migration envelope or Scope
  Change preview prepared before the server started is bound to, so that
  approval or Apply answered stale. The server now resolves the same `zh-CN`
  without writing it, and an ordinary CLI command still records it. A
  repository that already has its configuration, which `init` writes, sees
  no change.
- A compaction reload that the Host failed to confirm still reaches the
  capacity stop (#154, dayebishouji). A Host delivery confirmation with a
  wrong body hash, a wrong byte count, or no end marker answers
  `host_delivery_incomplete`, and that call also closed the open reload
  attempt, so the compaction that followed restarted the reload at Chunk 1
  instead of answering `capacity_stop`. An incomplete confirmation without a
  model proof half now leaves the attempt open; a model proof half, passing
  or failing, and a complete confirmation close it as before.
- `aoci status` reports an unreadable Baseline instead of a missing one
  (#153, dayebishouji). A corrupt `.aoci/baseline.json`, or a directory in
  its place, read as `baseline_exists=false` with exit 0 and a hint to create
  a Baseline. It now exits 2 with the error envelope under `--json` and a
  message, in the repository's Locale, that names the file, says it was left
  unchanged, and asks for it to be inspected before it is rebuilt. A missing
  Baseline is still a successful status result.
- A SQLite source can be declared (#109, dayebishouji; first slice of #83).
  `aoci database source add --engine sqlite --path data/app.db` saves one
  explicit file, relative to the repository root or absolute, in the `main`
  namespace, with no database name and no credential environment variable;
  URI filenames, `:memory:`, and server-only fields are refused. Nothing opens
  the file yet: SQLite access, inspect, snapshot, and verify answer
  `database_engine_not_implemented` until the read-only collector lands. Until
  then such a source neither feeds nor blocks a Database Volume: governance,
  Database Cognition Bootstrap, the policy-bound auto check, Database Entry
  removal, and onboarding collection wait for Evidence only from sources a
  collector exists for, and Verify, Check, and Guide report the SQLite source
  as the informational `database_source_engine_not_implemented`. The
  team configuration's `database` and `credential_env` fields become
  `omitempty`: a SQLite declaration writes neither, and a server declaration
  is written exactly as before.
- The MCP server runs on go-sdk 1.8.0 (#66) and negotiates the same
  revisions as before: `2025-11-25`, `2025-06-18`, `2025-03-26`, and
  `2024-11-05`. The SDK's newer `2026-07-28` revision removes the
  `initialize` handshake and blocks the server-initiated elicitation the
  host-dialog approval uses, so a client offering it in `initialize` is
  answered with `2025-11-25` and a sessionless request carrying it is refused
  with `-32022`; serving it is a later step with its own harness arm. On the
  wire, `tools/list` adds `ttlMs: 0` and `cacheScope: public`, an unknown
  method answers `-32601` instead of code 0, and one inbound frame is bounded
  at 16 MiB. The release binary gains one module, `golang.org/x/time` v0.15.0
  (BSD-3-Clause, from the Go project), for 23 in all; `THIRD-PARTY-NOTICES`
  and the supply-chain guide list it.
- The MCP server serves the sessionless `2026-07-28` revision (#66). A client
  on it sends its revision, client information, and capabilities in every
  request's `_meta` and may start with `server/discover`; the `initialize`
  handshake still answers at most `2025-11-25`, and older clients are served
  as before. That revision forbids a server's own requests, so the host-dialog
  approval travels as an input request there: the first
  `aoci_maintain intent=human_approval` call returns the form and a
  `requestState` and writes nothing, and the client calls again with the
  person's answer beside that state. An answer beside any other state is
  `stale` and records nothing, and an accepted form without approve or reject
  is `dismissed`; earlier revisions keep the server's own elicitation request.
  The capability manifest's `mcp_protocol` becomes `2026-07-28` and the new
  `mcp_protocols` lists the five negotiated revisions. The conformance suite
  gains a sessionless lane (58 checks) and the scenarios a sessionless dialog
  (80 scenarios).
- `aoci init` under an existing configuration with no index starts the
  Volume-first layout. It used to write the Legacy minimal skeleton there, for
  example after the aoci files were deleted to start over or under a
  configuration written before `init`, and with the Legacy layout read-only
  the Guide could only route that empty index to a migration. The
  configuration's own settings stay as they were. Only a custom `index_path`,
  which cannot hold a Volumes Root, still gets the Legacy skeleton.
- The Legacy-to-Volumes migration is drilled end to end. `make
  legacy-migration` has the v0.1.0-rc22 release build and align a Legacy
  repository in en-US and one in zh-CN; the binary under test then migrates
  each through onboarding, running only the commands each result names, with
  the approval given in the host dialog, and Verify, Check, and the Guide must
  report the result aligned with every Code Entry carried byte for byte. The
  full-confidence line runs it on every trigger, and `mcp_upgrade.py --fetch
  <tag>` caches one checksum-verified release binary for it.
- A Legacy migration can be followed from its own output. Under `--json`
  each `cognition onboard` result names its continuation in `next_commands`,
  bound to the running binary: the batch command, the receipt command, for a
  Migration the mapping template over the session's own Plan and Snapshot
  and then Preview, then Prepare, and after Apply Verify, Check, and the
  Guide. A batch with tasks carries `completion_template`, the receipt that
  completes it. A Migration's v1 receipt was refused for carrying the
  `batch_id` that `next` printed; it may now name the batch it completes,
  and a receipt that does not fit is refused as
  `onboarding_completion_contract_mismatch field=<field>`. `cognition
  migration mapping template` gains `--out-file`, which writes the template
  to a new file instead of through a shell redirect that could re-encode it,
  and `docs/upgrading.md` walks through the migration, the candidate and
  mapping files included. Existing fields keep their shape.

## v0.1.0-rc23

The rc22 soak's one finding: a Managed Scope rule could reach a formal
Volume and leave the repository with no command that moved it. Formal assets
are now outside policy. No MCP tool name, input schema, formal-asset format,
or scope identity changes.

- A scope rule that reached a formal Volume no longer deadlocks Managed Scope
  (#158, Loren-0101). A rule could give `aoci.code.txt` the observe role, and
  the Baseline then carried the Volume as Observe evidence that status, the
  Guide, and a Scope Change Plan each read differently: the Guide reported it
  `observed_removed`, `scope acknowledge` answered
  `observed_evidence_review_not_pending`, and every `scope preview` answered
  `observed_evidence_review_required`, with no command that moved the
  repository. Formal assets are now outside policy: evaluation gives the Root
  and its declared Volumes the index role whatever rules match them (a
  git-hidden one still stops `scan`), the three readers strip them through one
  projection, a Volume's Baseline record carries the index role however it was
  recorded, and `scope rule add` or `update` refuses an exact file rule naming
  one as `managed_scope_rule_targets_formal_asset` (switching such a rule
  off is allowed). A repository already in that state is aligned again on
  upgrade, and its next Scope Change writes the role back. No identity, tool,
  schema, or format changes.
- The pre-tag audit of that fix caught two readers it had missed. The pin is
  decided by the Root marker, so it never reaches a Legacy index file: there
  `aoci.txt` is an ordinary rule-governed candidate whose Entry Maintain never
  plans, and pinning it left an rc22-aligned Legacy repository with a custom
  profile or a `*.txt` rule answering `managed_scope_authoring_candidate_required`
  for its own index with no command that cleared it. The post-Apply alignment
  inspector that every `aoci_update_entry` result reads now takes the same
  projection as status and the Guide, so a Root or Meta record a rule once
  observed no longer makes each batch report `observed_removed` while
  governance says aligned.

## v0.1.0-rc22

The rc21 soak's findings and nine contributed fixes: the S hard limit is the
Meta rune quota, a blocked Maintain names its blocker, `scope acknowledge`
retires an excluded Observe fingerprint, Maintain candidates carry source-bound
scale evidence, hooks fail open on configuration errors, a symlinked `--repo`
is its own Git root, and the base toolchain moves to Go 1.26.10. No MCP tool
name, input schema, or formal-asset format changes; two additive candidate
fields and one wider `next_action` vocabulary.

- Toolchain: Go 1.26.10. govulncheck reported GO-2026-6809 (crypto/x509
  trust in compromised intermediate certificates, reached through the status
  page's HTTP server) against 1.26.9 from 2026-10-21 on; the nightly Full
  gate was red on the released tree until the base toolchain moved. No
  source, contract, or asset change.
- The S hard limit is the Meta rune quota (#146). The per-C S token band is
  floored by the effective rune quota, so an S within the quota the authoring
  contract states is never `entry_field_budget_exceeded`; a CJK Entry at C7
  was being refused at 80 of its declared 200 characters. The S maximum
  reported in Maintain and optimization candidates is the floored value. R
  bands are unchanged. Public contract: Managed Scope and Budget v1, section 8.
- A blocked Volumes Maintain names its blocker in `next_action` (#147):
  `explicit_orphan_remove_or_resolve_blocker` only when
  `orphan_remove_candidates` lists something, otherwise
  `close_pending_recovery`, `scan_to_establish_baseline`,
  `activate_scope_change`, `acknowledge_observed_evidence`,
  `resolve_cognition_budget`, or `resolve_blocker`. `next_commands` is
  unchanged. Public contract: Cognition Volumes v1, next_commands section.
- `scope acknowledge` retires an Observe fingerprint that an aligned policy
  now excludes, instead of refusing the whole review set with
  `observed_evidence_review_required` once another Observe path changes
  (#137, reported in #134).
- Volumes Maintain and `cognition_optimization` Code candidates carry
  advisory `file_lines` and `expected_e`, counted and hashed from one byte
  stream and emitted only when the digest equals the issued `source_sha256`
  (#143, #142). Public contract: Cognition Volumes v1.
- Hooks fail open on configuration errors: a truncated config or unsupported
  locale no longer makes `aoci hook pretool` or `aoci hook codex-compact`
  exit 3 before running, and pretool no longer migrates a legacy config
  (#144). Public contract: CLI Runtime v1, hook configuration failures.
- A `--repo` spelled through a symlinked component (`/tmp` on macOS) is the
  same Git root as its resolved spelling (#145).
- The optional model client classifies a non-2xx answer before reading its
  body, and a timeout while reading a body is a timeout, not a network
  failure (#139, #138).
- `aoci ui --also <relative> --stop` and a second `--detach` find the page
  registered under the absolute root (#140).
- The index parser strips every leading UTF-8 BOM, not one: a doubled BOM
  left `RawText` starting with the second, so a Document re-parsed from its
  own `RawText` changed (found by `make fuzz-smoke`; the input is a seed).
- Managed manifests no longer build an unused standalone Safe Inventory
  (#133, refs #127); one Windows-only test assertion decodes the Guide JSON
  before searching it (#141).

## v0.1.0-rc21

Eight user reports, four contributed pull requests, and a test-system
upgrade: Volume removal keyed on the object's path, a Ledger that survives a
damaged line, activation that answers `already_aligned`, governed writes on
WSL DrvFs, MySQL 5.7.6 and later, localized refusals, private host-config
backups with a guarded status page, and a command tree that starts in
milliseconds instead of half a second.

- Backups written beside host configuration and formal assets are owner-only.
  The optional model client refuses redirects and credential-bearing base
  URLs, keeps a caller-owned HTTP client's own redirect policy, and reports
  the status, the category, and a sanitized cause class (dns, refused, tls,
  timeout, redirect) without the endpoint's error body or the transport
  cause. No MCP or formal-asset schema changes.
- The loopback status page protects repository data with a per-process
  capability carried in the readiness URL fragment and sent by the browser
  in Authorization headers. The static shell contains no capability or
  repository data, and a page opened without the fragment says which link
  to open instead of rendering empty. Registrations keep their existing
  shape; a tokenless page from an older binary can be stopped but is refused
  by a new start, foreground or detached, until `aoci ui --stop` ends it.
- An out-of-band edit to a formal Volume under Managed Scope still fails
  closed with `managed_scope_formal_volume_baseline_drift`, and the refusal
  now says how to recover: preserve the state, check pending Recovery through
  the Guide, restore a verified matching snapshot when there is none, and
  never edit Baseline hashes. The Legacy `baseline scope` refusal names the
  same route, and `docs/troubleshooting.md` gains the section (#123).
- A Codex config declaring `[mcp_servers."aoci"]` or `[mcp_servers.'aoci']`
  was reported as not installed and then given a duplicate bare table. The
  installer and the status predicate recognize the quoted spellings, optional
  whitespace, and a trailing comment, and leave such a config byte-for-byte
  unchanged.
- Every `aoci` invocation built its command tree by decoding the embedded
  localization manifest once per localized flag, several hundred times, about
  half a second before the first byte of output. The decoded manifest is now
  memoized once per process and the message reverse index is built once, so a
  `config get` answers in a few milliseconds; human output is byte-identical,
  pinned by the init, Guide, and help goldens.
- Fuzzing the parsers that consume external bytes found and fixed four
  defects: an index warning could carry invalid UTF-8 into Findings and JSON;
  a line whose name is only Unicode whitespace lexed as an Entry with an empty
  name; an Attestation that repeated a challenge ordinal was scored every time
  it appeared; and `./C:/x` passed the drive-letter refusal of path
  normalization. The carried openGauss connector panicked on a quoted
  connection-string value ending in a backslash, from every parser entry
  point; the canonical patch now refuses it and `internal/dbevidence` also
  recovers at its boundary.
- Tests: `internal/cli` tests run through the per-invocation executor instead
  of package globals, so the package is order-independent (`-shuffle`) and
  runs in seconds; `internal/testkit` holds the shared fixture, git, locale
  and stdout helpers. Fault-injection tests cover the CAS rollback and
  recovery states, the directory lock fallback, immutable archives, zero-write
  closures, receipt storage, the Migration and Bootstrap codecs, replay
  mismatches and safety approvals; `make coverage-floors` enforces per-package
  statement-coverage floors for the safety core in full-confidence. Eleven
  native fuzz targets with seed corpora run under `make fuzz-smoke` nightly.
- Black box: conformance repeats its locale-dependent checks under zh-CN (53
  checks); scenarios gain group H for the pre-write hook and `--group`/`--list`
  (75 scenarios); lifecycle bring-up covers git submodules, the
  `exclude_root_dirs` roles, `remove-entry`, the machine documents, and a
  zh-CN twin lane. The upgrade axis gains a `downgrade` shape, in which every
  earlier release reads a repository the binary under test wrote (80 checks
  per release over ten shapes), `--self` runs the candidate as both the
  released and the under-test binary before a tag exists (`make axis-self`,
  also in `make verify` and the release rehearsal), and `--recent N` layers
  CI: a push runs the newest three releases, the nightly run every release.
  The real-repository harness is tracked as `scripts/blackbox/realrepo.py`.
- `scripts/blackbox/ladder.json` registers every test contract that must be
  run by name, with cadence, preconditions, and the last verification on
  record; `make verify` ends by listing what is overdue and `make ladder`
  prints it on demand.
- Nightly gates: the long-running cognition refresh harness runs again
  (`make longrun-refresh`; it had drifted from the Attestation schema and the
  repair finding shape since August), the seven first-party benchmarks are
  compared against an advisory baseline (`scripts/bench-compare.py`, a
  warning annotation above 30 percent, results uploaded as an artifact), the
  carried connector's own test suite runs against the openGauss container as
  an advisory step (300 pass, one server-behaviour difference on year-zero
  dates), and the page JavaScript tests get an explicit Node 22.
- Removing a Code Entry keys on the object's path, so an orphan whose Entry
  line is byte-identical to a line in another directory section (a staged
  copy of the same file) goes while its live twin stays; the line-keyed
  remover refused both as duplicates, and three such orphans held a
  1,400-target authoring debt behind `write_conflict` (#117). A plain
  repository-relative path under Volumes v1 names `code:<path>` and reaches
  the Volume pipeline instead of the Legacy loader's `volume_read_only`
  (#112); while a Managed Scope edit is pending, the orphan refusal says
  `scope_change_required` and names `aoci scope activate --json`, since
  orphan proof follows the active Baseline. The remove-entry help states that
  Volumes removal is orphan-only on both surfaces.
- A Volumes removal that had written the Volume and the Baseline but could
  not complete its Ledger event left a receipt nothing could close and
  `remove_ledger_completion_failed` carried no reason (#121). A Ledger line
  that does not decode is now skipped by the terminal-event check, a line
  over 1 MiB counts as one corrupt line instead of ending the read, the
  failure names its cause, and a disabled Ledger is refused before anything
  is written.
- `aoci scope activate` reports `already_aligned` with exit status 0 and
  records no transaction when the desired policy, budget, and authorization
  posture already equal the active receipt and nothing would move; it used
  to file a zero-change transaction and report it as "applied" (#113).
  `scope status` and `scope show` carry `repository_root`.
- When a Volumes v1 policy edit blocks `aoci_maintain`, `next_commands` and
  the Volumes Guide name `scope activate --json` beside `scope status --json`
  instead of looping between status and Maintain (#118; contributed by
  dayebishouji in PR #119).
- Twenty-six CLI commands rendered their English short description under
  every locale because the hand-kept localization map had no entry for them
  (`scope budget` and its subcommands, `observe-policy`, `approval-mode`,
  `authorize`, `source`, `capabilities`, `cognition onboard`, `baseline
  scope`); all are mapped, and a test walks the visible tree under zh-CN.
  The PreToolUse hook injected hardcoded Chinese text into every host; its
  six texts now come from the UI catalog in both locales, byte-identical
  under zh-CN.
- `repair_required` findings about an object that does not exist, or already
  exists, rendered "Repair only the reported  of this candidate"; each now
  has its own action naming the identity and the create/update alternative,
  and no field-less finding renders a blank.
- Governed CAS writes work on filesystems that refuse `RENAME_EXCHANGE`,
  such as WSL DrvFs (`/mnt/*`, 9p), where every Entry batch and Managed Scope
  write failed with "原子交换目标失败: invalid argument" while `init` and
  `scan` worked (#116). The primitive is probed and, when refused, emulated
  with a hard link and two renames that end in the primitive's exact
  post-state; crash states converge through the existing recovery. On such
  a filesystem the replacement window is link-to-rename rather than zero: a
  writer outside aoci that replaces the target by rename in that instant is
  overwritten rather than detected, while in-place modification is still
  caught; every aoci writer is serialized by the CAS lock. Verified on a real
  DrvFs mount.
- Strict decoders of governance and protocol artifacts (receipts, run
  manifests, previews, checkpoints, onboarding sessions, Curation documents,
  Host-Agent stage requests) reject two keys that differ only by letter case:
  `encoding/json` matches struct fields case-insensitively, so
  `{"path":"a","Path":"b"}` passed the exact duplicate-key check and decoded
  last-wins without an error. Callers that decode user-owned maps, such as
  the OpenCode configuration, keep the exact check, where differently cased
  keys are distinct by design. The new scanner has its own fuzz target under
  `make fuzz-smoke`.
- Database Evidence collects MySQL 5.7.6 and later (#111). The collector
  reads the server version and runs only the catalog queries that release can
  answer: CHECK constraints from 8.0.16, index expressions from 8.0.13, index
  visibility from 8.0.0; a 5.7 server records an empty check list, since 5.7
  parses CHECK syntax and stores nothing, and canonical Evidence keeps one
  shape on every supported release. A server below 5.7.6, an unreadable
  version, or MariaDB fails closed with `unsupported_catalog_feature` before
  any table query. The full-confidence MySQL job runs both `mysql:5.7` and
  `mysql:8.4`, each followed by the black-box database lifecycle, and
  `mcp_lifecycle.py --mysql-image mysql:5.7` maps the two 8.0-only statements
  of the frozen fixture to their 5.7 shape in memory.
- A refused repository path (empty, absolute, drive, UNC, no target, or a
  `..` escape) is reported in the active locale on the hook, the remove,
  update, report, and read faces instead of as a Chinese sentence that the
  en-US filter reduced to "non-localized diagnostic"; the refusals are
  sentinel errors with machine-token text, and a UNC path is named as such
  rather than as an absolute path. On a 9p mount (WSL DrvFs) an owner file
  that vanished between open and fstat failed lock acquisition under
  contention with "读取锁owner身份失败"; it now reads as a release and the
  acquisition retries, verified on a real DrvFs mount. The MySQL index
  projections for 8.0.0–8.0.12 and 8.0.13–8.0.15 were exercised against
  `mysql:8.0.12` and `mysql:8.0.15` through the database lifecycle suite.

## v0.1.0-rc20

Five user reports and two contributed pull requests: an E scale audit,
submodule boundaries, refusals that name the working path, root-anchored
artifact names for new repositories, and a benchmark.

- `verify` and `check` audit each Code Entry's E scale letter against the
  source line count under the Meta's bands and report the mismatches, as
  information that never changes the exit code (`e_scale` in `--json`);
  `aoci_maintain` with `intent=cognition_optimization` ranks such Entries for
  review after high-importance Entries without an S, with selection reason
  `e_scale_mismatch` (#103, reported by @Rosenls). Through rc19 the letter was
  checked only in the Legacy layout.
- A git submodule is reported as a `git_submodule` boundary instead of an
  `unsafe_filesystem_object`: `scan` says how many submodules it left alone,
  and `scope explain` on a path beneath one names the boundary (#107, reported
  by @zsjlfb). Indexing a submodule's code still means initializing the
  submodule itself.
- The refusals a stale Managed Scope Baseline meets now name the path that
  works: `scan --force` points at `aoci_maintain`, `aoci_remove_entry`, and
  `aoci scope activate`, and `baseline scope plan`/`preview` say they are the
  Legacy refresh (#102, reported by @Lixiang9716).
- Docs: the index format contract states that an Entry's leading name is the
  bare file name within its directory section (#103); the README's execution
  mode table marks endpoint-native drafting as Legacy-only and says what the
  `ai` configuration block serves in a Volumes v1 repository.
- New repositories get `exclude_root_dirs` (#104, PR #105 by @dayebishouji):
  `init` persists `backup`, `backups`, `build`, `cache`, `coverage`, `dist`,
  `target`, `tmp`, `uploads`, `artifacts`, and `vendor` there instead of in
  `exclude_dirs`, so those names exclude a directory at the repository root
  or wherever git ignores it, while a tracked nested `src/build/tool.go` or
  `views/monitor/cache/index.vue` is source. A git-ignored nested directory of
  such a name stays one inventory line (#97). Existing repositories keep
  their any-depth `exclude_dirs` and their identity; opting in is a config
  change plus `aoci scope activate`. `config get`/`set`, `doctor`, and
  `scope explain` know the field; the upgrade axis gains a `legacydirs` shape
  and publishes 72 checks per released version over nine shapes. A repository
  that carries the field needs rc20 or later on every machine: an older
  binary demands a Scope Change over it and drops the field when it saves the
  team configuration. The Baseline reads Entry paths through the same lists
  as the inventory, so an Entry under a root-excluded directory is skipped
  rather than reported as an orphan; `config set` on either directory list
  takes `build/` as `build`, drops duplicates, and refuses `app/build`, which
  could never match.
- Toolchain: Go 1.26.9. govulncheck reports nine standard-library advisories
  against 1.26.6 (GO-2026-6603, 6605, 6607, 6608, 6610, 6611, 6612, 6613,
  6617 in `net/http`, `net/textproto`, and `crypto/tls`, which the status
  page and the database drivers link), all fixed in 1.26.9; release binaries
  are built with it and `go.mod` requires it.
- Tests: the 1,000-object governance timing moved from a five-second
  wall-clock assertion to a benchmark that full-confidence runs (PR #108 by
  @dayebishouji).

## v0.1.0-rc19

Two fixes from user reports: Scope Change replay, and directory names that
hid source from the index.

- Scope Change replay no longer depends on files other tools write (#101).
  A Git-ignored path that no rule governs and that the active Baseline does
  not hold leaves the transaction envelope, and the candidate count and
  selection identity the envelope carries are recomputed from the paths kept,
  so a lock or cache file appearing between `scope preview` and `scope apply`
  (CodeGraph's watcher, for one) no longer ends in `deterministic_replay_failed`
  or `managed_scope_replay_mismatch`. A preview minted by rc18 or older fails
  replay once after the upgrade; run `scope preview` again. `.codegraph` is a
  built-in runtime directory like `.aoci`. Found and diagnosed by @Zedthm and
  @mahirhir.
- Directory names that can denote source are no longer a built-in safety rule
  (#100). Through rc18 `backup`, `backups`, `build`, `cache`, `coverage`,
  `dist`, `target`, `temp`, `tmp`, `uploads`, `artifacts`, and `vendor` were
  matched against every path component and hard excluded with no way back:
  `src/backup`, `views/monitor/cache`, and `tests/api/coverage` were dropped
  from the index silently. The built-in rule now keeps only names that never
  denote source (`node_modules`, `__pycache__`, `.next`, `.nuxt`, `.output`,
  `.cache`, the third-party distribution names); the rest are governed by
  `exclude_dirs`, which `init` already writes into `.aoci/config.json`, so an
  existing repository changes no role on upgrade and now has a door: remove
  the name with `aoci config set exclude_dirs ...` and run
  `aoci scope activate`. `scope explain` reports a configured exclusion as
  `exclude_dirs:<name>` or `exclude_files:<pattern>` instead of
  `project_config`. Two names were never in the default list: a tracked module
  under a nested `cache/` or `temp/` becomes a Missing Entry after the upgrade
  and is authored like any new source; a git-ignored one stays excluded but is
  listed per file rather than collapsed unless `exclude_dirs` names it.
- Black-box: a scenario plants a file in a git-ignored directory between
  preview and apply; the upgrade-axis fixture carries a tracked module named
  `backup` that every release must keep out across the upgrade.
- Dependencies: `golang.org/x/text` 0.41.0 (GO-2026-6629, a panic on crafted
  input in `x/text/secure/precis`, reachable through the database drivers'
  SASL preparation) and `golang.org/x/sync` 0.22.0 with it.

## v0.1.0-rc18

Inventory speed on large ignored trees, and a few host and CLI fixes.

- Safe Inventory no longer has git list ignored directories whose every file
  was a hard exclusion anyway (#97). The ignored listing now uses
  `git status --ignored=matching`: a git-ignored `node_modules`, `dist`,
  `target`, or other built-in generated, runtime, or VCS directory, and any
  directory named in `exclude_dirs`, is one inventory line instead of one entry
  per file on every `scan`, `verify`, and Maintain, and a pnpm workspace whose
  package links form a cycle no longer runs forever on Windows. Every other
  ignored path is listed one by one as before (an ignored `gen/` or `out/`, and
  ignored files inside untracked directories), so no candidate, rule outcome,
  opt-in, role, or persisted identity changes on upgrade; a test pins the
  candidate set and identities against the full listing. git before 2.16 falls
  back to the full listing.
- When the inventory's git query fails, `init`, `scan`, `scope`, and
  `source manifest` keep the machine code (`safe_inventory_git_query_failed`)
  and print git's own stderr, verbatim and bounded to its last 2 KiB, on the
  next line of stderr, in human and `--json` modes alike; the localization
  notes record this as the one verbatim exception. `verify`, `check`, and
  Maintain report the failure as a business-source finding, so run
  `aoci scan` to see git's message. `scope explain` reports a path under an
  `exclude_dirs` directory as a configured exclusion.
- `config list`, `config get`, and `config set` honour the global `--json`
  flag (#94, PR #95 by @dakjdakd): typed JSON values for `get`, and an
  `{ok, key, value}` result for `set` whose `value` is what the next
  `config get` returns (local overrides included); setting `locale` adds
  `restart_mcp_required` and, when a migration is pending, a
  `locale_migration` summary with the numbers human mode prints. Output goes
  through the command writer; human-mode output is byte-for-byte unchanged.
- `init --agent qoder` writes the project-level `.mcp.json` that Qoder CLI
  reads (verified with `qoder mcp list`), the managed `AGENTS.md` block, and
  no hook; `doctor` and the `aoci ui` status page gain a Qoder row (backed by
  that shared file), and `Detect` recognises a project `.qoder` directory or a
  user-level `~/.qoder`. The Qoder IDE takes the same `mcpServers` block
  through its settings.
- The Maintain authoring instructions now say that batches are not user
  decisions: after a successful write the model calls Maintain again until
  `remaining` is 0 without asking the user to continue. A Qoder session had
  stopped after the first of 53 batches to ask.
- `github.com/spf13/pflag` 1.0.9 to 1.0.10 (PR #93); module set unchanged.
- Black-box: lifecycle bringup plants an ignored `node_modules` tree with a
  symlink cycle and asserts the inventory collapses it; the upgrade axis gains
  shapes for a rule-pulled file inside an ignored directory and for an ignored
  `node_modules` tree; a Windows test builds a junction cycle inside an ignored
  `node_modules`.

- Fix the repair guidance for a mistyped or self-computed Code binding. Since
  rc16 the Maintain response no longer repeats `code_plan.candidates`, but the
  `code_candidate_source_sha256_mismatch`, `code_candidate_id_mismatch`, and
  `code_candidate_path_mismatch` repair actions still told the model to copy
  the value from there, so a first build whose model had hashed a file itself
  was refused with zero writes (correct) and then could not recover. The
  actions now name the Maintain response's `candidates` list and the finding's
  own `expected` value, the source_sha256 action says never to compute the
  hash, and the cause states the fact the server has already checked: the file
  on disk still matches the issued binding, so the submitted value came from
  other bytes. Black-box B4 requires the finding to be recoverable from the
  response alone.

## v0.1.0-rc17

S coverage on high-importance Entries. A real-repository build under Codex
came back with `S:-` on 78 percent of its Entries and on two thirds of its
C7-C9 ones, and a source audit found most of those were misses. The authoring
contract, the Guide field rules, and the public S discipline now say where to
look before writing `S:-` on a C6+ object: the implementation behind an
interface, the aspect or interceptor that wraps it, the SQL, templates, or
scripts it executes or feeds, the configuration that overrides it, and the
cache, transaction, soft-delete, and uniqueness rules on its persistence path;
a C7+ `S:-` written without that look is as wrong as an invented S. The
starter Meta example is now a C7 Entry with every field filled and an S one
sentence long inside the C7 band, where the previous starter showed
`R:- | A:- | S:-`. Existing repositories keep their Meta bytes.

- `aoci verify`, `aoci check`, `aoci scope status`, and the Maintain
  `governance.budget` carry `s_coverage`: Entries and Entries with S per C
  band, plus the count and share of C7+ Entries without S. `check` prints one
  line and, at or above the machine hint threshold of 25 percent, suggests one
  `cognition_optimization` pass. A hint, never a gate.
- `cognition_optimization` reviews C7+ Entries without S right after budget
  overage and before plain C order. Each candidate carries `selection_reason`
  (new value `s_absent_high_importance`), and the batch instructions say that
  returning such an Entry unchanged is a valid result.
- `aoci init` with the production profile persists seven starter `observe`
  rules (`starter-vendored-*`, source `builtin`) for vendored static assets
  and generated bundles: `libs`, `lib`, and `plugins` under `static/`,
  `*.min.js`, `*.min.css`, `*.map`, and `fonts/`. A first build no longer
  authors Entries for jQuery or Bootstrap. Written only when no config
  exists; `DefaultPolicy`, the built-in profile rules, and existing
  repositories are untouched, a user rule overrides a starter rule at any
  order, and `aoci scope rule remove` drops one before the first scan
  without approval. (`vendor`, `dist`, and `build` directories were already
  refused by the safety boundary.)
- `make update-goldens` also regenerates the prompt snapshot digests.
- Black-box: 64 fault-injection scenarios (O2 checks the optimization review
  order, reason, and unchanged-batch completion); the lifecycle suite asserts
  the starter rules after `init` and `scan`.

## v0.1.0-rc16

First-build speed and two recovery fixes. A Maintain response no longer
repeats the issued batch inside `code_plan`, trims its governance samples to
five while it carries candidates, and is cut by a byte budget rather than a
fixed count: `maintain_transport_budget_bytes` (team-owned, default 24 KiB,
16 KiB through 192 KiB) bounds the response of a Code batch (a Database batch
keeps its own `database_cognition_batch_evidence_bytes` limit),
`code_cognition_batch_entries`
becomes the cap with a default of 50, and a batch always carries at least one
candidate. At the default a create batch carries about 20 to 30 candidates
depending on path depth and an update batch of long Entries about a dozen,
where v0.1.0-rc15 sent 20 of either and overflowed Codex's tool-result cap on
the update side (49 KB on this repository). `aoci init --agent
claude` starts a repository at 40 KiB and 12,000 chunk tokens, the window
Claude Code was measured to carry; other hosts keep the defaults. Existing
indexes are not rewritten and every machine-written index that aligned under
v0.1.0-rc15 aligns here.

- Cut the Code authoring batch by transport bytes. The plan reports
  `transport_budget_bytes`; `authoring_batch` carries it too. The wire
  `code_plan` no longer carries `candidates` (the top-level `candidates` list
  is the batch, once); the disk receipt keeps the complete plan. A team that
  raised `code_cognition_batch_entries` above 50 for larger batches must raise
  `maintain_transport_budget_bytes` as well, since the budget is the operative
  bound.
- Trim the Maintain response while it carries candidates: governance lists,
  the review closure, and the write set keep a five-item sample plus complete
  counts (`list_truncation.limit` is 5 there; `sets.write_total` joins
  `sets.review_total`), and the authoring instruction is shorter and asks the model to read
  the batch's sources first, in parallel where the host allows, before writing
  the batch in one call.
- `aoci scan` reports what the first build costs: how many index files need an
  Entry and about how many Maintain rounds they take at the current settings
  (`authoring_estimate` in `--json`); a dry run adds the exclude-rule recipe
  that still narrows the scope for free before the first scan.
- Lower the default `overview_delivery.chunk_tokens` from 8000 to 7000 for
  margin under Codex's per-result cap. Repositories with an explicit value keep
  it.
- Fix `cognition_optimization` stopping with `checkpoint_recovery_required`
  beside a held source (#90). The post-Apply alignment inspector classified
  empty, binary, oversize, and curation-excluded index files as pending debt
  while Verify no longer did, so every optimization batch in a repository
  holding an image failed at finalization, left its entries receipt pending,
  and the documented same-batch retry failed the same way. Both now share one
  classification (`volumegovernance.ClassifyCodeDrift`), and the optimization
  error names the alignment findings it saw. A v0.1.0-rc15 repository stuck
  this way finalizes by resubmitting the identical batch with this release.
- Fix the Claude Code `PreToolUse` hook producing no Entry under Volumes v1
  (#85). The hook parsed `aoci.txt` as a Legacy index; it now loads the layout
  and reads the Code Volume, keeps the Legacy path, and still fails open.
- Suppress the visible console window every `git.exe` probe opened on Windows
  when aoci runs console-less as an MCP child (#79, #80 by @DaisyHunter).
- Keep the Baseline fault-injection fixture effective on Windows so the
  recovery tests exercise the failure they describe (#82 by @dayebishouji).

## v0.1.0-rc15

Five fixes for the first index and for recovery, plus three contributor
changes. A repository that holds an image, an empty file, or a file above
1 MiB now reaches aligned in auto mode: such files are held out of authoring
and reported, never handed to a decision Volumes v1 cannot make, which is
where the first scan of a project holding one stopped under v0.1.0-rc14. A stale
receipt under `.aoci/transactions` is seen by every surface and closed by the
tool that wrote it (#74). Contributor changes: catalog transaction options per
engine (#72), `aoci scope activate` (#73), and a nested-worktree index read
from the primary checkout (#78, fixes #77). Existing indexes are not
rewritten, every machine-written index that aligned under v0.1.0-rc14 aligns
here, and the nine tools accept the same inputs. `aoci verify --json` gains
`code_drift.skipped`, `code_drift.curation_excluded`, and
`governance.pending_transaction_files`; `aoci scan --json` gains
`skipped_sources`.

- Read an index authored in a nested git worktree from the primary checkout
  (#78, fixes #77). When the recorded root lies below the invocation root and
  every section belongs to that family, the reader now resolves the family
  against its recorded root instead of filing each Entry under the worktree
  path, so the primary checkout, the worktree, and an unrelated clone read one
  index. A single-section index stays ambiguous and keeps its direct reading.
  The upgrade axis gains a fifth shape that authors a fixture inside
  `.worktrees/wt` with each released binary, merges it, and reads it from the
  primary checkout with the binary under test: 40 checks per released version.
- Hold empty, binary, and oversize sources out of authoring instead of stopping
  the first Maintain on them. An index-role file whose bytes carry nothing a
  model can read (empty, a NUL byte in its first 8000 bytes, or above 1 MiB)
  is reported as `code_skipped` with its cause, listed under
  `code_drift.skipped`, never planned, and never blocks; an index-role file
  kept out by a valid curation decision is reported as
  `code_curation_excluded`. Verify, Check,
  Guide, and Maintain read one classification, so none of them counts a file
  another never plans. Releases up to v0.1.0-rc14 stopped the first Maintain of
  any repository holding an image with `pending_curation:` markers in
  `orphan_remove_candidates`, and Volumes v1 has no decision path that could
  have cleared them. `scan` announces the counts (`skipped_sources` under
  `--json`), a held file can still be authored directly, and a Maintain that
  names candidates always carries `authoring_meta` and `instructions`. The
  lifecycle governance walk now asserts the held-out probes reach aligned in
  auto mode, and scenario F11 covers a fresh repository with all three kinds:
  61 scenarios.
- Detect pending recovery receipts in one place (#74). Every receipt kind
  written directly under `.aoci/transactions/` (bootstrap, migration, reversal,
  scope, database bootstrap, remove, entries, header) and any foreign `.json`
  file there is pending for Overview, Verify, Check, Guide, Maintain, the CLI
  gate, every transaction start, and the remove and update commit paths alike,
  which refuse to write over any receipt that is not their own. Releases up to
  v0.1.0-rc14 refused
  full cognition delivery on a stale `remove-*.json` while Verify reported the
  repository aligned and nothing named the file. Verify lists
  `pending_transaction_files`, each receipt is its own `recovery_pending`
  finding with its kind as the cause, and the Guide stops on the file with the
  closure that fits it. `aoci_remove_entry` closes a receipt the Volume has
  moved past: with the object absent the receipt completes as superseded and
  neither the Volume nor the Baseline is touched; with the object still present
  and unchanged the receipt is discarded and the removal re-planned, both under
  the index lock and on the Ledger; an object that came back changed still
  refuses with `recovery_entry_reappeared`. A Legacy index closes its receipts
  the same way; a receipt the tool cannot load or resume, and a layout receipt
  pending beside an MCP receipt, have their documented order of closure.
  `verify`, `check`, `status`, `scope status`, `remove-entry`, and the Guide
  stay available while an MCP write receipt is pending.
- Report every repairable candidate of a batch in one `repair_required`
  response instead of stopping at the first; `retry_scope` names them all.
- Keep every fingerprint's index role when a batch advances the Baseline under
  Managed Scope; the batch path wrote roleless fingerprints over the ones scan
  had stamped.
- Add `aoci scope activate` (#73, #54). After editing rules, a budget, or the
  approval mode, one command builds the empty candidate set and runs the
  existing Scope Change preview and Apply. When the preview needs a human, it
  stops with exit 2, keeps the preview in a fresh directory under
  `.aoci/scope-change/`, and prints the `scope approve` and `scope apply`
  commands bound to this executable and repository; `--json` carries them
  under `details`. It edits no rules and authors no Entries, and every
  existing refusal stands. The four-line candidate-set recipes in the
  documentation are replaced by the command; the spec gains a section, and
  a fault-injection scenario (F10) covers activation over retained source
  drift and the approval pause.
- Select catalog transaction options per database engine (#72). PostgreSQL,
  MySQL, and openGauss keep their repeatable-read, read-only transaction; an
  engine without a policy fails closed before any connection is used, and the
  collector test now asserts the isolation level as well as the read-only flag.
  Groundwork for further engines; no behaviour change for the three supported.

## v0.1.0-rc14

One format-level fix: directory and file names the index grammar could not
spell (#58, #60, #48), which wedged any repository with a spaced directory or a
bracketed dynamic-route file. Plus three contributor changes: tool schemas that
Gemini-style function declarations accept (#63), per-repository status
snapshots (#64), and a native build suffix (#65). Existing indexes are not
rewritten, every machine-written index that aligned under v0.1.0-rc13 aligns
here, and the nine tools accept the same inputs. v0.1.0-rc13 still reads an
index this release creates, but not the new names themselves, so a repository
that uses one needs this release or newer on every host.

- Spell directory and file names the index grammar could not (#58, #60, #48).
  The directory path in a section header stopped at the first whitespace, `=`,
  `(`, or `（`, and a file name could not hold `[`. Three things followed. A
  repository under a root with a space wrote that root in full once and then
  appended every later section under the truncated root the parser read back. A
  directory with a space made its Entry resolve to a path that does not exist,
  so the same file was reported orphan and missing and the repository could
  never align. And a file such as `pages/docs/[...id].jsx`, the dynamic-route
  form of Next.js, Nuxt, and SvelteKit, could not be written at all, which
  failed its whole atomic batch on every attempt. The original readings stay
  first: an Entry line that parsed before parses identically, and a directory
  header changes only when its remainder ends in `/`, which is how a
  machine-written header for such a directory looks. A machine-written header
  is now read to its final `/`, a bracketed file name up to the tag that
  precedes `: F:`, and the dictionary, E-scale, S-quota, name-normalization,
  and migration consumers locate the tag through that same reading instead of
  the first `[`. Names are stored as they are, never escaped. An index already
  written with a truncated root keeps resolving in place and in a checkout at
  another path, without a rewrite, and a repository wedged by a directory name
  with a space reads as aligned on upgrade; every machine-written index that
  aligned under v0.1.0-rc13 aligns under this release. The origin and every
  checkout read the shape every release has written under such a root (the root
  section in full, later sections under the root as the original reading reads
  it back) the same way, because it is recognised from the text alone, before
  the runtime root is consulted: the first directory section reads as the
  common prefix under the original reading, whether the full root lies beside
  that prefix or, when the character begins a path segment (`/w/(x)/repo`),
  under it. This release writes that same shape, so one shape exists in the
  wild and v0.1.0-rc13 still reads an index created here. A new index now
  begins with its root section (written empty ahead of a first Entry that lives
  in a directory) and an empty section is kept while every other section lies
  below it, because it anchors relocation. Where an older writer spelled a
  directory in its first section (a dot-directory that sorted before every root
  file, or a directory whose first segment begins with `(`, such as a top-level
  `(home)/` under a clean root), that section keeps the old reading everywhere,
  so both sides report the same orphans and the ordinary repair aligns both;
  the same file name under it and under the section the repair adds is no
  longer taken for a duplicate, which refused every later write to such an
  index. A directory whose header would not read back to the same path (a
  segment that begins or ends with whitespace), or whose section would resolve
  somewhere else, is refused instead of being recorded there: `aoci_maintain`
  withholds the batch and answers `stopped` with `stop` facts that quote the
  directory and give the operator's way out, and `aoci_update_entry` gives the
  same answer to a caller that submits anyway, with a
  `code_directory_unspellable` finding and no retry scope, because no Entry
  edit clears it. A repository root that no header can carry (its first segment
  begins with `(`, `（`, or `=`, or a segment has edge whitespace) is not
  refused: the index records the part of it a header reads back, as every
  earlier release effectively did; only a root with no such part (a repository
  directly under `/` or a drive root whose name begins with one of those
  characters or with whitespace) is stopped, as `code_root_unspellable`. File
  and directory scope patterns accept `[`, so a dynamic route can be named in a
  rule, and a refused rule pattern now says why. v0.1.0-rc13 and earlier cannot
  read the new names themselves (they refuse an index holding a bracketed file
  name and resolve such a directory somewhere else), so a repository that uses
  one needs this release or newer on every host (`docs/upgrading.md`). The rule
  is in `spec/public/aoci-index-format-v1.txt`. Unit tests cover the readings,
  the healing, relocation before and after an insert, roots that cut at a
  segment boundary or cannot be spelled, and the root anchor; MCP tests walk
  authoring, healing, the directory-first index, relocated checkouts, and the
  withheld batch through to the rename that clears it; black-box scenario P1
  authors all four kinds under a root with a space and verifies a copy at
  another path, and P2 walks the withheld batch, the refused direct update, and
  the rename on the shipped binary (59 scenarios); and the upgrade axis gains
  two repository shapes, under a path with a space and under a directory whose
  name begins with `(`, plus a check that what the binary under test authors
  keeps a checkout aligned (32 checks per released version, over four shapes).
- Declare nullable tool inputs without JSON Schema type arrays (#61, #63). Five
  inputs across `aoci_get_entries`, `aoci_update_entry`, and `aoci_overview`
  used `"type": ["null", ...]`, which Gemini's function-declaration endpoint
  refuses, so a host forwarding the MCP schemas there could not register the
  tools at all. They are now `anyOf` with an explicit null branch. The accepted
  instances are identical; the published schema bytes change, and the golden
  moves with them. Checked against a live endpoint: the rc13 declarations
  answer HTTP 400 on gemini-3.8-flash and gemini-2.5-flash, these answer 200.
- Build status-page snapshots for different repositories concurrently (#64).
  One global mutex was held while a snapshot was built, so a slow repository
  blocked every other one; each root now has its own lock.
- Give `make build` the native executable suffix (#59, #65). Windows gets
  `build/aoci.exe` directly, `make verify` uses the same path, and the README
  drops the copy step. Linux and macOS are unchanged.

The first public availability date for v0.1.0-rc14 is 2026-09-19.

## v0.1.0-rc13

One fix for a path that stayed closed after a cognition optimization (#57),
three status-page fixes from the contributor who found the rc10 and rc11
ones (#49, #51, #52), one harness improvement (#53), and one documentation
addition. Nothing in the nine-tool surface or any governance identity
changes.

- Keep the direct update path open after a cognition optimization. The
  update classifier counted an item as part of the current optimization
  batch when its batch id equalled the checkpoint's current or
  last-completed batch id, and one of those two is empty while the first
  batch is pending, between two batches, and after the optimization
  completes, when the checkpoint stays on disk for good. An item without a
  batch id, which is the documented compatibility path for a Code source
  and for an existing Database object, matched the empty id and was refused
  as `cognition_optimization_batch_mixed` with zero writes; Maintain-issued
  batches carry ids and never saw it. Present since the feature shipped
  (v0.1.0-rc3). A batch-less item now never counts as an optimization
  item, so it takes the ordinary path; three regression tests cover the
  three windows, and black-box scenario O1 walks the sequence on the
  shipped binary. (#57)
- Serve raw Volumes from the cached snapshot on the status page. `/api/raw`
  reopened the Code and Database Volume paths on every request while
  `/api/state` reused a two-second snapshot, so a Volume replaced with the
  same size and modification time let the two routes describe different
  bytes, with a validate-then-open window in between. The snapshot keeps the
  bytes the loader validated and the raw route serves those. (#49)
- List only the current user's servers in Linux discovery. The `/proc` scan
  parsed every process's command line without checking ownership, so another
  user's `aoci mcp` could appear on the status page with its repository path.
  Each process directory's owner is now compared with the effective UID
  before its command line is read. (#51)
- Reject an unexpected `Host` on the status page. Any `Host` value was
  accepted, so a browser reaching the loopback page through DNS rebinding
  could read responses carrying absolute repository paths. Every request is
  bound to the listener's exact address; the URL `aoci ui` prints is built
  from that address and keeps working, and on port 80 the form browsers send
  without the port is accepted as well. (#52)
- Keep the server's stderr tail when a black-box RPC times out. The three
  harness clients drained stderr continuously but their timeout error
  dropped the bounded tail. Test infrastructure only. (#53)
- Record the Codex host prerequisites. `docs/agent-integrations.md` gains
  the settings an unattended run through Codex 0.149 had to make before AOCI
  could be used: the Responses API for custom providers, the approval mode
  and standard-input redirection for headless `codex exec`, and the
  per-model tool-result ceiling, which is met by raising it in
  `model_catalog_json` or by lowering the team `overview_delivery.chunk_tokens`
  (floor 4000) beneath it.

The first public availability date for v0.1.0-rc13 is 2026-09-17.

## v0.1.0-rc12

Seven fixes for repositories governed without a human in the loop, surfaced by
the first agent that drives the MCP end to end (aoci-agent) and by a
contributor governing a Volumes repository on Windows (#46, #47), plus one
test-only guard for the driver audit table. Two fixes close a Volumes v1
deadlock in the Managed Scope transaction; five correct counts, warnings, and
refusals. Each fix carries a test that fails without it, and the real-repository
harness now walks the deadlock path itself (56 scenarios and a new lifecycle
governance walk). Nothing in the nine-tool surface or any governance identity
changes.

- Let a Volumes v1 repository with a changed source activate a policy change.
  A policy edit (a scope rule, a budget) makes desired differ from active, and
  every authoring path refuses to write until one governed Apply activates it.
  That Apply refused to plan while any index-role source had changed since the
  Baseline (`managed_scope_index_source_stale`), and the Entry candidate that
  clears the block under Legacy was projected into the Root manifest under
  Volumes, where `aoci.txt` holds no Entries. A repository with one changed
  file and one pending rule had no legal move (#47). A policy-only Scope
  Change now plans over a changed source in Volumes: the postimage Baseline
  keeps the fingerprint the Entry was bound to, the plan reports the path
  under `source_stale_retained`, and the next `aoci_maintain` plans the source
  as it always did. A Volumes candidate set that carries `entries`,
  `dispositions`, or a `header` is refused before projection with
  `managed_scope_volumes_entry_candidates_unsupported`. Legacy keeps failing
  closed, and a plan without retention serializes as before, so no in-flight
  `plan_id` changes.
- Name the field in a refused candidate set. `managed_scope_entry_candidate_invalid`
  and `scope_entry_disposition_invalid` named only the path, so the operator
  read the validator to learn that `candidate_id`, `review_status`, and
  `current_entry_sha256` were required (#46). The refusal now names the
  position and the field (`entries[1]: candidate_id is empty`), the digest
  mismatches say which digest they compare, and the candidate set is
  documented field by field in `docs/managed-scope-and-budget.md`.
- Count a fresh file once. A source created after the last scan has no Entry
  and no Baseline fingerprint, so drift classification files it under both
  Missing and Unbaselined, and Maintain's `authoring_batch` and Guide's batch
  added the lists: two new files promised `total_targets` 4 and `remaining` 2
  with `continuation_required` true while the plan held exactly two
  candidates, and a curation-excluded file was counted although no candidate
  would ever carry it, so remaining never reached zero. Maintain and Guide now
  derive the total from the one authoring-work set Maintain plans from, so the
  batch, the plan, and Guide agree; verify's unresolved-path counts stop
  double-counting the same file. Candidates were never duplicated; only the
  counts were.
- Warn about an E tag that contradicts the file length in Volumes v1. The
  write path read the E-scale thresholds from the Code Volume's own header,
  which in Volumes v1 is one marker line; the dictionary lives in Meta, so no
  threshold was ever found and a four-line file tagged L applied without a
  word. Volumes writes now read the thresholds from Meta's code dictionary
  alone (each Meta section declares its own E Scale line) and answer the
  advisory warning Legacy answers, now localized in both layouts, under
  `audit.warnings`; the S-quota advisory reads the same dictionary. Nothing
  is rejected.
- Name the candidate and field in a batch binding defect. An empty
  `candidate_id` or a malformed `source_sha256` answered a bare bad_args that
  named nothing, and a model that had mistyped one field received the same
  rejection for every resubmission. The rejection now carries a Repair Finding
  with the candidate's position, path, field, and repair action, in the
  vocabulary of the candidate's own domain, with no formal write started.
- Refuse brace alternation in a scope rule. The glob compiler treats `{` as a
  literal, so `assets/*.{png,jpg}` was accepted and matched nothing; adding or
  editing such a rule now fails with the spellings that work. A persisted rule
  is not touched, so no repository stops loading.
- Skip a discovered server whose working directory was deleted. `/proc`
  reports such a directory as `<dir> (deleted)`, and the status page joined
  that text into a root that does not exist.
- Guard the driver audit table in `docs/supply-chain.md` against `go.mod`.
  The pre-tag adversarial review had to move that tag-pinned table's pgx and
  mysql rows by hand after the rc11 driver bumps, because nothing read it. A
  contract test now fails when a listed module's pinned version differs from
  the version `go.mod` requires, and requires the openGauss row to keep
  resolving through the local `replace` to the patched tree under
  `third_party/`. Tests and documentation only; the nine-tool MCP surface and
  every governance identity are unchanged.

The first public availability date for v0.1.0-rc12 is 2026-09-15.

## v0.1.0-rc11

Five more status-panel fixes from the contributor who found the rc10 three,
three dependency updates, and a README that opens with what AOCI-CODE does.

- Recover the raw asset pane after a failed request. A transient `/api/raw`
  error was cached as an empty string, so later unchanged (304) polls never
  fetched it again: the index pane stayed empty and Copy All reported success
  after copying nothing. A failed read is no longer cached, the pane shows the
  disconnected message, Copy All reports failure and leaves the clipboard
  alone, and an unchanged poll retries an uncached asset without holding up
  the next poll. (#41)
- Preserve a replacement registration during `aoci ui --stop`. Stop read the
  registration, probed the old page, and then removed the record
  unconditionally, so a page that registered during the probe kept running
  but could no longer be found by a later `--detach` or `--stop`. Stop now
  removes only the record of the PID it captured, and Register and Unregister
  share a lock under the user cache directory so a replacement cannot land
  between the owner check and the removal. (#42)
- Validate the repository list before the page uses it. A malformed
  `/api/repos` answer replaced the list before any check, the next render
  threw, and startup ended before polling was scheduled. The page now accepts
  only an array whose rows carry a nonempty root, a name, and a nonnegative
  running count, the exact shape the server emits, and otherwise keeps the
  last valid list and selection. (#43)
- Filter the index in one pass. Every matching line walked backward to find
  its section and asset banner, and a standalone Code Volume has no banner, so
  each match scanned to the top: about a second for 16,000 matches. One
  forward pass now remembers the last section and banner; output, matching,
  and counts are unchanged. (#44)
- Resolve a discovered server's relative `--repo` against that server's own
  working directory. On Linux the page resolved it against its own, so
  `aoci --repo project mcp` started elsewhere could show the wrong repository.
  Discovery now reads `/proc/<pid>/cwd` and skips a server whose directory it
  cannot read rather than attach it to a wrong root. (#45)
- Update `github.com/jackc/pgx/v5` to 5.11.0, `golang.org/x/sys` to 0.48.0,
  and `github.com/go-sql-driver/mysql` to 1.10.1 (#38, #39, #40). The release
  module set is the same 22 modules and every license text is byte-identical;
  catalog-only collection was re-verified against PostgreSQL 18 and MySQL 8.4
  with the new drivers.
- Open the README with what AOCI-CODE does and the notes to read before
  starting: the supported system scale, which the index size bounds rather
  than the line count, the time a first index takes, the database index, and
  the read-only, offline, credential-free boundary, all ahead of the one-step
  setup.

Each panel fix carries a regression that fails against the previous page or
registry; the nine-tool MCP surface and every governance identity are
unchanged.

The first public availability date for v0.1.0-rc11 is 2026-09-13.

## v0.1.0-rc10

Three fixes to the status panel, all found by a contributor using it in the
day after rc9 shipped.

- Refresh the page's cached state after an indexed source changes. The cache
  fingerprinted only the formal assets, configuration, Baseline, and
  .git/HEAD, so editing an indexed source moved no watched file and the page
  kept showing the repository aligned while Verify would have called it
  stale. A bounded full recheck now rebuilds the snapshot after two seconds
  or on a manual refresh; an unchanged rebuild keeps its ETag, so 304 still
  works. (#35)
- Recover polling after an invalid state response. An HTTP error with a JSON
  body replaced the last good snapshot, and a body that failed to parse threw
  out of the refresh before the next poll was scheduled, so automatic refresh
  stopped for good with no visible sign. Responses are now checked and parsed
  before being accepted, a failure keeps the last good state, and the poll
  always reschedules. (#36)
- Ignore stale repository and asset responses. Switching repositories or tabs
  while a request was in flight let the late response overwrite the new
  selection, and the raw cache was keyed by asset name alone, so one
  repository's Volume could appear under another's name. State and raw
  responses are now bound to the repository, the request order, and the
  cache generation. (#37)

Each fix carries a regression that fails against the previous page; two of
them execute the embedded page script under node:vm and skip where Node is
absent. The nine-tool MCP surface and every governance identity are
unchanged.

The first public availability date for v0.1.0-rc10 is 2026-09-08.

## v0.1.0-rc9

- Declare a per-tool result-size allowance on `aoci_overview`. A Host that
  persists a large tool result to disk and puts only a preview in the model's
  context turns cognition delivery into a silent failure: the body never
  reaches the model, and no receipt can observe that. The tool now carries a
  static `_meta` declaration raising that threshold for itself alone. Measured
  on Claude Code, the full `24000` chunk_tokens becomes usable and a
  480-object index delivers in three chunks instead of five. It is a
  declaration, not Host capability detection: nothing is probed, and a Host
  that does not recognize the key ignores it, as the MCP specification
  requires. The nine-tool surface, the input schemas, and every governance
  identity are unchanged.
- Add `aoci ui`, a local read-only status page. It shows, for one or more
  repositories at once, the identity, index header, Code and Database Volumes
  with object, line, byte, and token counts against the budget, the exact
  Overview chunk plan at the configured `chunk_tokens`, governance drift and
  Managed Scope counts, host integrations, and the next step the Guide
  recommends — with the prompts a user sends to an agent and the commands that
  clear a block ready to copy. On Linux and WSL it also lists the running
  `aoci mcp` processes of the current user and marks a server whose binary was
  replaced on disk. The page binds loopback addresses only, answers GET and
  HEAD only, takes no lock, appends nothing to the Ledger, and reads the same
  facts Verify, Check, Guide, and Maintain consume through the one Guide
  builder the CLI uses; `aoci mcp` still opens no socket and the nine-tool
  surface is unchanged.
- `aoci ui --detach` starts the panel in the background, detached from the
  shell that asked for it, and prints its link — so an agent can hand a user a
  panel link that still works after the agent's command has returned. A panel
  already running for the repository is reused rather than duplicated;
  `aoci ui --stop` ends it. Registrations live in the user's cache directory,
  never in the repository. The panel also reports how much source the index
  covers (files, lines, tokens) and the compression ratio between source and
  index, shows every Volume verbatim with a copy button, switches language on
  the page, and lets the reader choose the refresh interval.
- Lead the README with the one-step setup. The release-candidate notice moves
  down to the section that obtains the package, and the setup now carries two
  more prompts a user sends verbatim: one that builds the database index once
  the source is declared and its credential variable provisioned, and one that
  re-establishes framework cognition after a context compaction, which is the
  only thing that restores reliability after one.
- Deprecate the Legacy layout. Volumes v1 has been the only layout `aoci init`
  creates since the first candidate, every Legacy-only command is labeled as
  such, and the governed migration exists, so the remaining Legacy code is
  scheduled for removal in v0.2.0. This release only says so; nothing changes
  behavior.

The first public availability date for v0.1.0-rc9 is 2026-09-08.

## v0.1.0-rc8

- Probe the atomic no-replace primitive instead of assuming it. Publishing a
  staged file into a name that must not already exist used `renameat2` with
  `RENAME_NOREPLACE` unconditionally on Linux, but the flag is a filesystem
  capability rather than a kernel one: WSL's DrvFs answers `EINVAL`, so every
  `aoci init` against a path under `/mnt/c` failed with
  `init_volume_create_failed aoci.meta.txt` while the identical repository
  initialized on ext4. A Windows checkout was reachable from WSL for every other
  purpose and not for this one. The flag is now tried first and, where a
  filesystem refuses it outright, the same guarantee is built from `link` plus
  `unlink`: `link` reports `EEXIST` for an occupied name and never replaces it,
  so a refusal still leaves the existing bytes untouched. Being over-broad about
  which errors select the fallback is safe by construction — the fallback
  carries the identical guarantee — while `EEXIST` and the other real answers
  are always propagated. Verified on DrvFs and ext4, and an opt-in test takes a
  directory so a filesystem no CI runner can reach is still checkable.
- Publish a staged file on exFAT under macOS. The Darwin no-replace primitive,
  `renamex_np` with `RENAME_EXCL`, was assumed to answer on every filesystem,
  and a probe run on two macOS runners showed it does not: exFAT returns
  `ENOTSUP`, so `aoci init` could not create a Volume on the one filesystem
  macOS and Windows share on an external drive. It is the shape DrvFs produced
  on Linux, found this time by measuring rather than by a report. The Linux
  repair does not port — its fallback is `link` plus `unlink`, and the
  filesystems that refuse the flag are the msdos family, which has no hard
  links — so Darwin reserves the name with `O_CREAT|O_EXCL`, which the kernel
  either grants or answers `EEXIST`, and then fills it. Every failure after
  the create removes the target, and both callers verify the published digest
  instead of trusting the write. One trap is recorded in the source: on Darwin
  `ENOTSUP` and `EOPNOTSUPP` are distinct errnos, unlike Linux, and
  classifying only the second would have left the fix inert while looking
  complete. The probe now measures exFAT, FAT32, and HFS+ as separate subtests
  on disk images it creates itself, and a filesystem it cannot measure fails
  instead of passing quietly.
- Prove the upgrade axis. All three black-box suites build every fixture with
  the binary under test, so a persisted preimage that changed *between*
  releases is invisible to them by construction — two such defects reached
  rc6 and rc7 and were caught by hand. `scripts/blackbox/mcp_upgrade.py`
  downloads every release in this changelog, verifies it against its own
  `SHA256SUMS`, builds and authors a repository to `aligned` with it, and then
  requires the binary under test to govern that repository without moving an
  identity, demanding a Scope Change, or rewriting a formal asset: 14 checks
  per released version over two configuration shapes. The second shape is the
  point. Every released `aoci init` writes an explicit `cognition_budget`
  block, so a fixture built only by `init` never resolves the frozen
  `LegacyPolicy` at all, and the suite stayed green against a deliberately
  broken freeze; `nobudget` strips the block before `scan`, reproducing every
  repository created before the block existed, and only that shape turns red.
  The suite runs on the nightly Full Confidence line because it needs Release
  assets, and a download failure fails the run rather than skipping the
  matrix. One exemption is deliberate: the newest changelog section may
  precede its release by the one commit the cut takes, so a 404 for that
  version alone is characterized rather than failed — the gate that must pass
  is the one that creates the release. Any older missing release, any other
  fetch error, and a matrix the exemption would empty still fail.
- Fold current database items out of Maintain transport. Every Database
  Cognition item in the `current` state carried its complete Entry text plus
  evidence and binding hashes while feeding no decision: candidates are built
  from the unbounded facts before the transport projection runs. A repository
  with a 52-table domain that needed nothing paid twenty full Entries per
  Maintain, and one 53 KB response was spilled to disk by its host. The
  summary keeps the count, and `aoci verify --json` and `aoci check --json`
  keep the full enumeration. The suites' host window drops with it from an
  assumed 64 KiB to a measured 48 KiB: bisected on a real host, 49,429-byte
  responses enter the model context and 51,916-byte responses arrive as a
  2 KB preview of a file. The response above had passed the old gate and
  died in production, which is the exact gap between assumption and
  measurement.
- Stop demoting delivered cognition to `invalid` over a pending refresh. The
  read path defaulted `cognition_state` to `invalid` and upgraded it only once
  `refresh_status` settled, so a receipt that had just passed a 10/10
  Attestation read `invalid` the moment the working tree carried in-flight
  edits — inviting a pointless Whole-Index redelivery instead of the Maintain
  cycle the state was pointing at. `invalid` is now reserved for identity
  breaks: no delivery in the session, or a receipt whose repository, service,
  or index no longer matches. A reliable identity-matching receipt under a
  pending refresh reads `uncertain`, which is what the refresh contract says
  drift is.
- Carry runnable `next_commands` on terminal, blocked, and evidence-required
  MCP results. The final Volumes Apply prescribed "Verify, Aggregate Check,
  Guide" in prose — names that exist nowhere on the nine-tool surface — and a
  blocked Maintain carried the bare token
  `explicit_orphan_remove_or_resolve_blocker` while the command that clears
  the block lived only in the CLI Guide; two hosts on two repositories stalled
  on exactly that gap. The command suffixes now live once in the machine
  contract, and the CLI Guide and MCP compose the identical spelling from
  them. The prefix is the running server's own absolute, quoted executable,
  because `aoci` is whatever path the host's MCP configuration names and need
  not be on `PATH`; when it cannot be resolved the field is omitted, since an
  absent advisory beats a wrong command. Placeholders such as `{agent}` are
  the Host's to fill. The field is additive and optional, and the scenario
  suite executes every returned command as returned: Verify, Check, and Guide
  exit 0 after the final Apply, and the acknowledge command clears the block
  it was returned for.
- Say what a raised budget does not buy, and why there is no staleness bypass.
  The scale boundary stated a token ceiling and left the reader to infer that
  raising it removes the limit. It does not: the budget bounds what may be
  written, never what a model can receive, and a Whole-Index is delivered as a
  chunk chain in which every round trip is a place a host compaction can void
  the chain. A repository whose plan estimate runs to several hundred thousand
  tokens has a role problem before it has a budget problem. The same guide now
  records why a content-volatile file whose cognition never changes gets a
  role change or a same-text resubmit rather than a per-file exemption from
  staleness: the source binding is the property the write chain protects.
- Make the release rehearsal run the release's own tooling. `release.yml` had
  moved to newer `checkout`, `upload-artifact`, `download-artifact`,
  `setup-go`, and `goreleaser-action` versions while `ci.yml`,
  `full-confidence.yml`, and `release-rehearsal.yml` stayed on older ones;
  every reference was correctly SHA-pinned with its version comment, so both
  existing contract tests passed and nothing reported the divergence. The
  rehearsal is the one that matters: it was packaging with older tooling than
  the release it rehearsed, so a packaging difference between those versions
  would clear the rehearsal and appear only in the tag-triggered signed run,
  the one that cannot be cheaply retried. All three now match `release.yml`
  exactly, and a new contract test requires an action referenced by more than
  one workflow to be pinned to one SHA, with no exceptions list.
- Run the black-box gate when a black-box suite changes. Full Confidence's
  push filter listed every internal package the suites exercise but not the
  suites themselves, so a fix to `mcp_scenarios.py` for a Windows-only
  attestation failure — section roots are written with forward slashes on
  every platform while the fixture path arrives backslashed — shipped without
  the gate that would prove it. `scripts/blackbox/**` joins the filter, and
  the Windows run it triggered is the proof for the platform that failed.

The first public availability date for v0.1.0-rc8 is 2026-09-03.

## v0.1.0-rc7

- Stop refusing to initialize a repository over files the rules already
  excluded. `aoci init` keyed its initial-scope approval on a counter that
  incremented for every tracked path a built-in rule excluded, so one tracked
  file under `vendor/`, `dist/`, `build/`, `coverage/`, `target/` or any other
  built-in generated directory refused initialization outright — and an excluded
  path is never opened, so its presence could not make initialization unsafe.
  Every remediation the refusal offered was wrong for that category: removing
  the files from Git or adding them to `.gitignore` both mean deleting them from
  the repository, `.gitignore` does not untrack an already tracked file, and the
  opt-in it named accepts only `sensitive` paths and reads the configuration
  file that only `init` creates. A repository that vendors its own framework
  source could not be initialized at all. Approval now keys on the counter
  `internal/fs` already maintained for it, which counts only paths whose content
  an explicit opt-in will actually read. Those still require a human, and every
  excluded file keeps its role and stays unread.
- Explain an initialization refusal with the reason that actually stopped it.
  Approval moved to the counter of paths an opt-in will read, but the message
  still branched on the retired one, so a repository blocked because its profile
  assigns no path to the index role was told instead that a tracked file needed
  an explicit decision. Following that advice deleted the file and reached the
  same refusal — the wrong-remediation shape this release exists to remove, left
  one function over.
- Stop letting one approval ratify past a safety boundary. A Managed Scope
  change reports `interaction_required` when a human reviewer may ratify its
  blockers, and the published partition names the blockers that carry no
  reviewer at all, `transport_constraint` among them. The derivation set
  `interaction_required` whenever *any* blocker was ratifiable, and the
  transport-constraint risk is only ever raised inside the coverage-reduction
  branch, which always contributes a ratifiable blocker — so the mixed set was
  the only reachable one, the unratifiable classification could never decide
  anything, and a change the previous release held closed on apply, authorize
  and approve alike could be landed with a single confirmation. The derivation
  now refuses the reviewer route as soon as one blocker is a safety boundary,
  which is what the contract says: the operator resolves the condition instead.
- Deliver the dimension-name diagnostic in the locale it was written for. A
  diagnostic that does not match the active locale is discarded whole and
  replaced by a generic message, and the canonical spelling of every dimension
  is Chinese — so quoting the accepted spellings inside an English refusal made
  the refusal collapse and point the operator at `.aoci/config.json`, which was
  not the problem. The previous release, which offered no hint at all, produced
  a better message on that path. A message now quotes only the spellings its own
  locale can carry, and the test asserts the rendered hint survives the guard
  rather than merely containing the right words.
- Keep the answer `aoci status --deep` gives a repository with no index. The
  Legacy-layout precondition was placed ahead of the absent-index branch and
  shadowed it, replacing "the index does not exist, so --deep cannot run; run
  aoci init first" with a raw filesystem error that also leaked an absolute host
  path into the machine-readable message.
- Hand back a budget remediation that runs. The remediation named `aoci scope
  preview`, and run literally that returns a status document with no preview
  artifact, after which `scope authorize` and `scope apply` both refuse for a
  missing `--preview-file`. `scope preview` emits that artifact only when it is
  given a candidate set, and a configuration-only change still needs one, empty.
  The sequence also has to keep its artifacts out of the worktree: a plan binds
  the repository state, so writing `preview.json` beside the sources changes the
  state the plan was minted against and apply refuses with
  `managed_scope_replay_mismatch` — the first attempt at a runnable remediation
  prescribed the repository root and therefore still did not run. `.aoci` is
  excluded from the Safe Inventory unconditionally, so the finding, the Guide
  instruction in both locales, and `docs/managed-scope-and-budget.md` now put
  every artifact under `.aoci/scope-change/`, and a test pins that the
  prescribed location leaves the plan identity untouched while the worktree
  does not.
- Say in `docs/upgrading.md` that an in-flight Scope Change approval does not
  survive the upgrade. An approval binds the preview envelope digest, and the
  host-independent path change removed two fields from that envelope, so a
  preview and approval minted by v0.1.0-rc5 are refused by v0.1.0-rc6 and the
  reverse is refused too. Nothing is written and no state is damaged, but the
  operator has to finish or discard the pair before replacing the binary. The
  v0.1.0-rc6 note claimed no in-flight approval was stranded; that was true of
  `plan_id` and not of the envelope.
- Ignore the machine-bound backup a host-integration installer writes. The
  installer backs a host config up before rewriting it, and the backup carries a
  timestamp, so it could not be listed as an exact path: the config was ignored
  and its backup, holding the same absolute host paths, was not.
- Fail the Actions version-comment gate when it inspects nothing. Its sibling
  has carried that guard since both were written; this one passed vacuously over
  an empty set, which is the same false green the pair exists to prevent.
- Corrections to the v0.1.0-rc6 notes, which were less accurate than the code.
  `aoci init` installs the Codex compaction prompt and reload hook only when
  both `--agent codex` and `--hooks` are given, not on `init` alone. The bounded
  standard input is two bounds, not one: the hook reads 64 KiB and fails open,
  while `update-entry` reads the Entries request limit and refuses; the command
  is `aoci update-entry`, not `aoci index update-entry`. The unconditional
  `.aoci` and `.git` exclusion lives in `internal/fs/safe_inventory.go`, not in
  `walk.go`.

The first public availability date for v0.1.0-rc7 is 2026-08-31.

## v0.1.0-rc6

- Keep a repository that never chose a cognition budget on the one it has. A
  `config.json` with no `cognition_budget` block resolves the machine default at
  runtime, and that resolved policy's identity is stamped in the Baseline and
  compared on every load — so raising the default would have moved the identity
  of every such repository, flipped budget alignment to false, and demanded a
  human-approved Scope Change the repository did nothing to earn. That
  population is reachable through fully supported commands, because `aoci scope
  rule`, `profile`, and `observe-policy` all write a scope policy and never
  materialize a budget. The legacy policy is now a frozen preimage with its
  identity pinned by a test that says, in the failure message, to revert the
  change rather than update the literal. The new-project default is a separate
  function that may move, and the two must never be collapsed back together.
- Raise the budget a new project starts with to 200000 target / 300000 warning /
  400000 max tokens. Only `aoci init` writes it, and only after proving
  `config.json` did not already exist, so no existing repository is touched:
  every repository created by rc1 through rc5 carries an explicit block and
  keeps it. At this repository's measured density of about 122 tokens per
  object, the previous 240000 covered roughly two thousand objects and the
  warning fired at about fifteen hundred, which put an ordinary large service in
  warning at its first authoring.
- Hand back the remediation a repository over its budget can actually run. The
  refusal reported a bare `cognition_budget_exceeded`, the Guide offered `aoci
  scope status` and an instruction to compress cognition, and `aoci scope budget
  set` — which has existed all along — was named in exactly one line of one
  document. Compression is the wrong answer for an index that grew because the
  repository did, and it was the only answer offered. The finding now carries
  the numbers that produced it and both levers, the blocked Guide carries Stop
  facts and the command that raises the budget, and the instructions state that
  a raise is a governed change requiring `aoci scope preview`, human approval,
  and `aoci scope apply`. `docs/managed-scope-and-budget.md` states that the
  published numbers are the new-project default rather than a system limit, and
  a test holds those numbers to the code that produces them.
- Report the Whole-Index that `aoci scope status` is actually governing. Under
  Volumes v1 it measured `aoci.txt` alone — the Root pointer, a few hundred
  bytes naming the other assets — and announced 101 tokens and zero Entries for
  a repository whose index is 58166 tokens over 476 objects. That is the command
  every blocked budget path hands the operator, so it told them their index was
  empty at the moment they were being refused for its size. Per-field costs and
  per-Entry violations now come from the object Volume, and the Whole-Index
  total is rebound to the complete declared asset set. Legacy repositories keep
  the single-asset measurement unchanged.
- Let a repository's own S quota declaration govern the gate that refuses its
  Entries. `internal/cognition` held a fourth, private copy of the quota rule
  that read the machine default directly and never looked at the Meta
  declaration, while the authoring contract handed to the model is built from
  that declaration: a repository declaring a wider band was told the wider
  number, authored to it, and was refused at the machine default by a message
  naming a limit the operator had already changed. No edit to the declaration
  could clear it. Resolution now runs through one place, and that place only
  ever loosens — a declaration wider than the machine default governs, a
  narrower one does not tighten. The asymmetry is deliberate: an error-level
  gate that honoured a tightening would make an already-persisted Volume
  unloadable the moment an operator narrowed their own Header, taking Verify,
  Check, Guide, Maintain and Overview down together. With the floor, nothing
  that loads today can fail after this upgrade, and a repository that could not
  load its own authored Entries now can. The declaration is read before any
  object Volume is judged rather than only for Volumes the Root happens to
  declare after it, on both the load path and the volume-commit path.
- Correct the published claim about that gate. `spec/public/s-field-discipline`
  stated that an over-quota S "emits warning-level violations that pass through
  without blocking persistence", which is what the curation check does and the
  opposite of what the Volumes v1 object gate does. The two gates, their
  different levels, and the reason they resolve a declaration differently are
  now stated. This repository already enforces that published numbers match the
  code; the divergence survived because a published behavioural claim had no
  such gate, so one was added.
- Name a dimension the header declared in a spelling the parser does not accept.
  Writing `#S-quota` where `#S quota` is expected was indistinguishable from
  declaring nothing: the operator's numbers silently became the machine
  defaults. For the E scale dimension it was worse — the declaration was
  discarded, the fallback dictionary substituted, and the operator's own
  declared symbol then reported as illegal against "the E scale line in the
  header", the line that had just been thrown away, which `aoci_update_entry`
  treats as a hard refusal. The accepted spellings did not change, because
  accepting more of them would turn a currently dark gate on for repositories
  whose header uses hyphens and break writes that succeed today. A rejected
  spelling is now reported as a declaration that is present and unreadable,
  quoted as written and alongside the accepted forms. It is reported only for a
  dimension that received no acceptable line, so a stray line beside a correct
  declaration stays silent.
- Stop paying for a formatter on every file on every call. `managedscope`
  recompiled a glob pattern for each rule and path pair — 171745 compilations
  over 48 distinct patterns for one `aoci check` — and `baseline.HashFile` ran
  `go/format.Source` over every Go file, which profiled at 94.7% of that
  function against 1.5% for the SHA-256 it supplements. A formatted digest is
  only ever consulted when the raw digest changed, so computing it for a file
  whose bytes are unchanged is work no judgement can use. Patterns are memoized
  on their exact bytes, and a caller holding the Baseline may supply it so an
  unchanged file skips the reparse. Every digest is byte-identical to a cold
  hash: reuse requires the raw digest, the size, and the normalized digest to
  agree before any stored value is carried over. `aoci check` on this repository
  went from 2.52s to 0.78s with `check --json` output unchanged byte for byte.
- Stop deciding Managed Scope path matching by asking the host filesystem.
  `filesystemCaseSensitive` probed the running machine and folded the answer
  into the applied scope identity, so one repository carried two different
  governance identities depending on whether it was checked out on Linux or on
  Windows. The equivalence bridge built over that re-evaluated every path under
  the opposite semantics and published an alternate identity when the roles
  provably coincided, which hid the difference without removing it: where a rule
  and a path genuinely differ in case, two platforms could not both be aligned,
  and one of them received a `scope_change_required` with no real cause.
  Matching now uses Git semantics — exact and case-sensitive — on every host,
  and the probe and the bridge are gone. The identity preimage deliberately
  keeps its case-sensitive form, so every receipt established on a
  case-sensitive filesystem stays valid unchanged. Only a receipt recorded under
  the historical case-insensitive semantics migrates, and where both semantics
  assigned the same roles its plan is identity-only — no role change, no Entry
  change, `aoci.txt` byte-identical — and policy-bound auto authorizes it
  without a human. Where they genuinely diverge it is a real role change and is
  authorized as one. `docs/upgrading.md` states the boundary and the command.

- Show a vanished Observe file in the plan the operator has to acknowledge.
  `scope status` keys an Observe removal on the source snapshot while the plan
  keyed it on the role map, and a file deleted from the worktree but still
  tracked by Git is evaluated as an unsafe filesystem object, so it stayed in
  the role map and read as reclassified by policy rather than gone. `status`
  listed it, the plan did not, and the review set `aoci scope acknowledge`
  submits could never match — the refusal named neither the extra path nor the
  missing one. The role map remains the key, because it is what separates a
  deliberate Observe-to-Exclude transition from a path that simply vanished;
  only the vanished set is added to what the plan reports.

- Close a staged host-agent Entries run that a later transaction has already
  superseded. Such a run could stay pending forever once a legitimate governance
  transaction had advanced the formal Index from the same preimage: the recovery
  path read every changed Index as unknown drift, even when a complete
  content-addressed receipt chain proved the staged run itself had written
  nothing, and nothing the operator did cleared the pending state. Closing one
  now requires a pristine stage-only run, no apply Ledger evidence, locked and
  stable Index, Baseline, and repository state, no pending transaction or CAS
  asset, and a unique gap-free, fork-free, time-ordered chain whose first
  receipt belongs to a transaction other than the staged batch. A stored closure
  is revalidated on every later pending check, so a deleted or tampered receipt
  reopens the run rather than leaving a Manifest flag to be trusted.

- Reload the cognition contract after a Codex compaction, and state why a probe
  cannot survive one. `aoci init` installs a compaction prompt and a reload
  hook, and the hidden `aoci hook codex-compact` emits developer context
  carrying a fresh refresh event id. The runtime contract and the public refresh
  spec now state the rule the hook serves: a compacted handoff carries no
  Whole-Index, Overview, Challenge, or Attestation body; an index receipt copied
  into a handoff cannot prove the resumed model's cognition; and a cognition
  probe is valid only when no compaction is known. A probe answered after a
  known compaction passes while proving nothing.

- Pin every GitHub Actions reference to a commit SHA, and stop letting an
  absent gate script report success. `release.yml` has been pinned from the
  start because it signs, attests, and publishes artifacts, but `ci.yml`,
  `full-confidence.yml`, and `release-rehearsal.yml` kept movable tags — and
  those three check out the source, run the gates, and decide whether the
  repository is green, so an upstream retag could change this repository's
  verdict with nothing visible in its own history. All 35 references in those
  three workflows now carry a 40-hex commit SHA and a comment naming the version
  it stands for, because a bare SHA can be diffed but not reviewed;
  `internal/safety` enforces both properties and fails if it inspects nothing,
  so it cannot pass while covering an empty set. The pins are the versions
  already in use resolved to their current commits, since pinning and upgrading
  are separate decisions. The `safety` and `check-deps` Makefile gates each ran
  their script only when the file existed and otherwise printed a skip notice
  and succeeded; a missing gate script is now a failure, because a broken gate
  that reports success is the same false green one layer down.

- Bound the standard input the CLI will read. The pretool hook runtime and
  `aoci index update-entry` both read stdin through an unbounded `io.ReadAll`,
  while the same repository already read `LimitReader(max+1)` on the Agent stage
  and curation protocols — the discipline existed and two entry points missed
  it. Both now read through one bound, and the two callers answer an oversize
  payload differently on purpose. Hook infrastructure must never block a
  workflow, and its input carries only a tool name and a path, so the hook fails
  open on either an oversize input or a read error and discards the payload
  rather than buffering it. `update-entry` refuses as a configuration error
  before any formal write, and also refuses `--entry` together with `--stdin`,
  because silently preferring one would write content the operator did not
  intend. A refused oversize input leaves the Index untouched.

- Make `aoci status --deep` behave the way it is documented. It is a
  Legacy-only command, but it ran against a Volumes repository and reported a
  drift set the Volumes governance path owns, so the public documents and the
  binary disagreed about one command. The existing write-path precondition is
  now reachable under a neutral name and answers both callers, because a second
  copy of a guard is how it silently stops covering one of them. The public CLI
  runtime contract states both this routing and the standard-input bound.

- Give every refused Managed Scope change a reviewer, or a stated reason there
  is none. A cognition coverage reduction planned under `inherit` with effective
  `auto` reported `interaction_required=false`, so `scope approve` answered
  `managed_scope_approval_not_required` while `scope apply` and `scope
  authorize` stayed blocked by `managed_scope_auto_authorization_blocked`: the
  change had no approver at all and the repository was stuck. The auto branch
  derived its answer from a single risk field, so exactly one of the twelve
  reasons `autoAuthorizationBlockers` can refuse was ever routed to a person.
  The answer is now derived from that same blocker set rather than a second
  hand-kept copy of it, and the reasons are partitioned once. Ratifiable, and
  therefore routed to independent review: posture and budget relaxations,
  high-risk content, P0 and P1 findings, cognition coverage reductions, and an
  already-decided explicit drop. Safety boundaries, which carry no reviewer
  because the operator must resolve the condition instead of approving past it:
  a transport-constraint basis, an incomplete retention review, an exceeded
  enforce-mode budget, an absent recovery direction, and any business-source
  write or postimage. An unclassified reason fails the build instead of
  defaulting, because both defaults are wrong — one recreates the dead end and
  the other would let an approval wave a safety boundary through. The public
  Managed Scope contract states the partition and the rule that both sets come
  from one enumeration.

- Stop refusing a Scope Change over a line ending. `internal/scopechange`
  compared sources by raw SHA-256 and so became the last consumer bypassing
  `baseline.EquivalentFingerprints` — the function whose own contract calls
  itself the single entry point for that judgement. An earlier release closed
  the same class in `internal/volumegovernance` and declared that package the
  only bypasser; nothing enforced the claim. `core.autocrlf` is the Git for
  Windows default, so one ordinary checkout left Verify, Check, and Guide
  calling a file aligned with no work to do while every Scope Change hard-failed
  on it with `managed_scope_index_source_stale`. Neither side offered a move
  that cleared the block, the operator concluded their Entries were unreadable,
  and upgrading could not help because the earlier fix never reached this
  package. No Entry was ever lost: the blocker fires only when the file is
  already an index-role object in the Baseline. Every comparison in the package
  is now classified rather than swept. Source equivalence runs through
  `EquivalentFingerprints` — the stale blocker, the coverage-reduction grading,
  the formal Volume and Root Baseline guards, and observed evidence drift —
  while identity bindings stay raw, because candidate source digests, Entry
  preimages, and the historical Root reconstruction must recover the stored SHA
  exactly. Tolerance is not silence: the Plan carries `source_line_ending_only`
  so the operator sees which sources were accepted as equivalent before the
  postimage Baseline records their new bytes. That field is omitted when empty,
  so a plan without such drift serializes exactly as before, its `plan_id` is
  unchanged, and no in-flight approval is stranded.

- Say in `docs/troubleshooting.md` that a committed tree-wide digest must
  exclude `.aoci`. A release process that hashes the whole tree and commits the
  result back covers `.aoci/baseline.json`, which is tracked by design, and the
  two form a fixed point: the digest depends on the Baseline, and the Baseline
  records the fingerprint of the file holding the digest. No pair of contents
  satisfies both, so the repository keeps one permanently stale Entry. This is a
  foreseeable interaction rather than a defect, and AOCI already makes the same
  decision internally — `internal/fs/walk.go` excludes `.aoci` and `.git`
  unconditionally to prevent self-swallowing, so the Business Source manifest
  never contains a governance asset. Nothing under `.aoci` participates in a
  build or in the source a review covers, so the exclusion makes such a digest
  answer its intended question rather than weakening it.
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

The first public availability date for v0.1.0-rc6 is 2026-08-30.

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
