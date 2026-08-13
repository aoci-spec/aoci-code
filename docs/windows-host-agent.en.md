# Windows Host-Agent and PowerShell 5 Contract

This is the English rendering of
[`windows-host-agent.md`](windows-host-agent.md) (the Chinese original). It
defines the stable invocation contract for the AOCI-CODE CLI on Windows, Codex
Desktop, and Windows PowerShell 5, as part of the Host-Agent automation
protocol. Where a rule is already carried by an English authority — the
`AGENTS.md` managed block, the MCP tool descriptions, the live Guide, or a
public spec — this page summarizes and points instead of restating.

> Scope: the Entries/Header/Curation Stage pipeline (sections 5-13 and 16.2)
> applies to Legacy monolithic repositories. A Volume-first repository (the
> current default initialization layout) uses the live Guide plus ordinary
> no-argument `aoci_maintain` and `aoci_update_entry` on Windows as everywhere
> else; see [`cognition-volumes.md`](cognition-volumes.md). The installation,
> cognition-reuse, PowerShell, and dual-fingerprint sections apply to both
> layouts.

## 1. Stable installation path

Recommended fixed location: `C:\aoci\bin\aoci.exe`.

Upgrade order: close Codex Desktop; confirm no leftover `aoci.exe` MCP
process; back up the old binary; replace the file at the stable path; verify
`--version` and SHA-256; restart Codex Desktop and open a new session.

Project-level Codex MCP configuration must reference the stable absolute path:

~~~toml
[mcp_servers.aoci]
command = "C:/aoci/bin/aoci.exe"
args = ["--repo", "C:/aoci/project", "mcp"]
~~~

## 2. Complete cognition and reuse

A normal task starts with one `aoci_rules` call and one ordinary
`aoci_overview`. Overview returns the complete index body; when it exceeds the
transport budget it is delivered as chunked continuation
(`continuation_required` + cursor) with unchanged semantics. The binding rules
for chunk continuation, Attestation, refresh reasons
(`context_compaction` / `semantic_threshold` / `phase_transition`), receipt
validity, and the prescribed result sentences are carried by the `aoci_rules`
contract and [`overview-delivery.md`](overview-delivery.md); this page does
not restate them.

While the cognition receipt stays valid, do not re-read the Overview, re-fetch
known Entries, or re-search known facts because of replanning, tool retries,
test failures, or small steps. Global MEMORY, historical sessions, and
external material are auxiliary only: system architecture follows the current
complete index, implementation follows current source.

When the index exceeds the transport budget, the Host must submit each exact
cursor automatically until `completed=true`; `aoci_get_entries` and
`aoci_search` answer local questions only after complete cognition is
established and never substitute for it.

## 3. User file scope and AOCI-managed assets

When the user restricts changes to specific files, the restriction binds
business modifications (source, tests, configuration, documents). Managed
cognition assets updated to preserve consistency are not a scope violation:
`aoci.txt`, the Volume assets (`aoci.meta.txt`, `aoci.code.txt`,
`aoci.database.txt`), `.aoci/baseline.json`, and `.aoci/curation.json`.
Runtime assets (`.aoci/ledger.jsonl`, reports, drafts, verify history) stay
local. "Read-only" commands (verify, check, index score, index inventory) may
still append local Ledger/Verify History audit records.

The final report must separately list business modifications and AOCI-managed
maintenance. If the user explicitly forbids touching `aoci.txt`, `.aoci`, or
metadata: perform no formal Apply, advance no Baseline, run
verification only in an isolated copy, report remaining Stale state honestly,
and never claim aligned.

## 4. Closing order for a normal development task

`aoci_maintain` is the final closing entry, not an intermediate step:

~~~text
business changes → format → lint/tests → git diff review
→ aoci_maintain (once, at the final stable state)
→ model authors real semantic candidates per file
→ one aoci_update_entry batch preserving source_sha256 bindings
→ internal Check / Diff / CAS / AtomicWrite / Baseline / Ledger / alignment recheck
→ final reply
~~~

Modifying any managed file after maintenance invalidates the previous result;
close again from the new stable state. Maintain returns compact JSON
(`applied`, `repair_required`, or `stopped`, plus alignment and refresh
facts); after an aligned Apply that reports `refresh_ready_for_overview`,
complete Verify/Check/Guide proof first, never Maintain again, and let the
Agent decide whether the next phase needs an ordinary Overview.

## 5. First Guide starts from the effective MCP configuration

Before the first Guide there are no bound commands, so a bare `aoci` must not
be attempted. Codex Desktop must read the project `.codex/config.toml`, locate
`[mcp_servers.aoci]`, confirm `command` is an existing absolute executable
path, keep the global arguments that precede the terminating `mcp`
subcommand, drop `mcp`, and append `index agent guide --agent codex --json`:

~~~powershell
& "C:/aoci/bin/aoci.exe" `
    --repo "C:/aoci/project" `
    index agent guide `
    --agent codex `
    --json
~~~

Stop and report when the config file or table is missing, `command` is empty
or relative, the executable does not exist, or the terminating `mcp` cannot be
identified safely. Never guess PATH, scan disks, or probe multiple paths.

## 6. Model-only semantic generation

Header, tag dictionaries, every tag, F/R/A/S, and every Curation
decision/role/reason/confidence must be generated by the current host model
after reading real content. Generating, prefilled, assembling, or rewriting
semantics via AST/symbol enumeration, import scans, path or extension
templates, regex extraction, fixed boilerplate, rule engines, batch scripts,
or tool-draft-then-polish is forbidden. Tools may enumerate targets, read and
chunk content, hash, size, control batches, save UTF-8 requests, validate,
audit, and persist. Structural tools (AST, LSP, graphs) may assist location
and relation checking but their output never becomes AOCI semantics directly.

FRAS discipline: see [`s-field-discipline.en.txt`](../spec/public/s-field-discipline.en.txt)
and the authoring contract served by the live Guide. F length is not
machine-blocked for Legacy v1 entries; Volumes v1 objects enforce the FRAS v2
density limits owned by `internal/machinecontract`. In Curation templates,
`confidence = -1` is an invalid placeholder and must be replaced by a JSON
integer from 0 to 100 decided per item.

## 7. Guide commands run verbatim

After the first successful Guide, `commands` are bound to the current absolute
binary path. Keep the PowerShell call operator `&`, the quoted paths, and the
returned global arguments; never fall back to bare `aoci`, rely on an
interactive PATH, or reuse an old command, `plan_id`, `run_id`, or digest.

## 8. Entries Auto three states (Legacy Stage pipeline)

With `automation.mode=auto`, Entries Stage saves the standard draft, runs the
machine preflight internally, and reports one terminal state in
`auto_finalize.status`. A Volumes repository sees the same three-state
semantics on `aoci_update_entry` results.

- `applied` — the batch completed Check, Diff, P-23 review, atomic Apply, and
  audit. Do not repeat Check/Diff/Apply; execute the returned absolute
  `next_command` (normally Verify), rerun Guide, continue.
- `repair_required` — repairable candidate content errors with zero formal
  writes. Stable contract: exit code 0, empty `auto_finalize.error`,
  `applied=0`, `formal_writes_started=false`, `preserve_other_candidates=true`,
  `retry_scope` naming only the failed objects, findings each carrying
  `candidate_index`, `path`, `canonical_object_identity`, `domain`, `field`,
  `rule_code`, `expected`, `actual`, `cause`, and `safe_repair_action`. Repair
  only the named candidates, keep every other candidate byte-for-byte, rewrite
  the same complete batch, re-run the same Stage command, and let the new
  Stage create a new Run. Never ask the user to reply "continue", never call
  Entries Check/Diff/Apply directly, never bypass machine validation, never
  regenerate unrelated correct candidates. Source-SHA drift, Plan expiry, CAS
  conflicts, pending Recovery, third-party conflicts, and formal-write or
  post-write audit failures remain `stopped` and must not be relabeled.
- `stopped` — the automatic flow cannot continue safely. Read `failed_step`,
  `error`, `recovery`, `asset_written`, and `audit_recorded`. The current
  version additionally closes stopped write attempts from evidence: proven
  zero-write closes the Run and replans; a complete Intent with a provable
  postimage resumes; a policy-selected rollback with the exact preimage rolls
  back and replans. Only approval boundaries, unprovable recovery state,
  third-party byte conflicts, and real environment failures stop at the user.

Unified principle: relaxed toward user stops, never relaxed toward formal
asset quality; content errors self-repair and continue; only consistency,
approval, write-state, or environment failures stop.

## 9. Stage requests use plain UTF-8 files

Windows PowerShell 5 text pipelines may re-encode non-ASCII content through
the local code page. Prefer `--request-file <UTF-8 JSON file>` (BOM or no
BOM, absolute Windows paths, forward or back slashes, spaces and non-ASCII
paths, non-ASCII JSON content). Directories, devices, named pipes, UTF-16 or
invalid UTF-8, empty files, and over-limit files are rejected. Write UTF-8
without BOM from PowerShell 5 via `[System.IO.File]::WriteAllText` with
`New-Object System.Text.UTF8Encoding($false)`; never save non-ASCII JSON with
plain `>` or pipe it into `--stdin-json`.

For an aligned repository that needs an explicit Header semantic refresh, use
the documented `intent: "semantic_refresh"` Header Stage request bound to the
current `plan_id`; it opens only Header semantics and changes none of the
Diff/P-23/approval/Apply/CAS/Baseline pipeline.

## 10. PowerShell 5 native JSON capture

Ordinary CLI `--json` output on Windows is ASCII-safe JSON and can be captured
directly:

~~~powershell
$raw = & 'C:\aoci\bin\aoci.exe' --repo $repo verify --json
$exitCode = $LASTEXITCODE
$result = ($raw -join [Environment]::NewLine) | ConvertFrom-Json
~~~

Judge Stage results by `auto_finalize.status`, never by exit code alone
(`applied` and `repair_required` both exit 0). Keep stdout and stderr
separate; never contaminate parseable JSON with `2>&1`. MCP stdio continues to
use raw UTF-8 JSON-RPC.

## 11. Request size and batch caps

Protocol hard caps exist for Entries, Header, and Curation Stage requests
(total size, per-field size, and per-batch candidate/decision counts). The
numeric authority is `internal/machinecontract/numeric.go`; Guide and CLI Help
derive from that contract at runtime, and the Chinese original of this
document is locked to it by cross-layer equivalence tests.

## 12. Missing machine-field compatibility (Legacy)

Legacy Verify reports `result.missing = raw_missing`; Score/Check and
Plan/Guide expose the raw/actionable/curation-excluded/pending aliases
documented in the Chinese original. A governed terminal state permits
`raw_missing > 0` only when actionable, pending-curation, orphan, stale, and
unbaselined are all zero and the remainder is explained by curation exclusions
or non-pending technical skips. Volumes verify returns the
`layout_mode`/`volumes`/`governance` structure instead.

## 13. Apply and safe retry

Standalone Apply reports may return `applied`, `rejected`,
`applied_with_warnings`, or `asset_written_audit_failed`. Principles:
`asset_written=false` means zero formal writes and a retry may follow the
recovery action; `asset_written=true` forbids blind re-Apply;
`asset_written_audit_failed` requires read-only Verify/Check/Guide first;
never infer write state from the exit code in either direction;
`repair_required` re-Stages after reading findings; Warning-only batches may
Auto Apply.

## 14. LF and CRLF dual fingerprints

With the default line-ending tolerance, pure LF/CRLF differences classify as
LineEndingOnly, not Stale: Verify and Check stay successful and Guide stays
aligned with no Entries targets. Real content changes always classify as
Stale. Setting the team option `line_ending_tolerance=false` restores strict
byte comparison.

## 15. Codex Desktop upgrade acceptance

After an upgrade, confirm at minimum: a fresh session establishes cognition
via `aoci_rules` + one ordinary Overview; a valid receipt is reused while an
explicit ordinary Overview still delivers the complete requested scope;
context compaction is declared by the host or Agent, never claimed as CLI
auto-detection; semantic changes reaching the team threshold are maintained to
aligned before any refresh; the first Guide derives its absolute command from
`.codex/config.toml`; Guide commands do not depend on PATH; the three Auto
states behave per section 8 (repair_required exits 0 with zero formal writes
and never asks the user to continue); JSON parses directly in PowerShell 5;
MCP stays raw UTF-8 JSON-RPC; the final Guide returns complete or an accurate
stop.

## 16. Minimum Windows acceptance matrix

Complete cognition: the full index body arrives (chunked when large); no
repeated Overview inside a valid context; the three refresh reasons adjudicate
per contract; drift is maintained and proven aligned before a refresh; the
semantic threshold triggers at the machine-owned count; MEMORY never overrides
the current index and source.

Model-only semantics (Legacy Stage; Volumes repositories accept via
Guide+Maintain batches): Header, tags, FRAS, and Curation are model-generated
item by item; no AST/path/template/script ghost-writing; `confidence=-1` left
unreplaced is rejected.

Entries Auto repair, JSON/UTF-8 behavior, and final governance follow the
matrices in the Chinese original: clean batches apply, injected candidate
errors return `repair_required` with zero formal writes and a preserved Run,
UTF-8 requests round-trip losslessly, rejected encodings fail explicitly, and
the final Guide converges to aligned/complete with the Raw Missing three-way
partition conserved.
