# Windows Host-Agent and PowerShell 5 Contract

This is the English rendering of
[`windows-host-agent.md`](windows-host-agent.md) (the Chinese original). It
defines the stable invocation contract for the AOCI-CODE CLI on Windows, Codex
Desktop, and Windows PowerShell 5, as part of the Host-Agent automation
protocol. Where a rule is already carried by an English authority — the
`AGENTS.md` managed block, the MCP tool descriptions, the live Guide, or a
public spec — this page summarizes and points instead of restating.

> Scope: this page describes a Volumes v1 repository (the current default
> initialization layout) on Windows: the live Guide plus ordinary no-argument
> `aoci_maintain` and `aoci_update_entry`, as everywhere else; see
> [`cognition-volumes.md`](cognition-volumes.md). A Legacy monolithic
> repository is read-only since 0.2.0: every write tool answers
> `legacy_read_only_migrate_required` and the Guide offers the migration as
> its one next step. The installation, cognition-reuse, PowerShell, and
> dual-fingerprint sections apply to it as well.

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

Header, tag dictionaries, every tag, F/R/A/S, and every Scope or Curation
decision (role and reason) must be generated by the current host model
after reading real content. Generating, prefilled, assembling, or rewriting
semantics via AST/symbol enumeration, import scans, path or extension
templates, regex extraction, fixed boilerplate, rule engines, batch scripts,
or tool-draft-then-polish is forbidden. Tools may enumerate targets, read and
chunk content, hash, size, control batches, save UTF-8 requests, validate,
audit, and persist. Structural tools (AST, LSP, graphs) may assist location
and relation checking but their output never becomes AOCI semantics directly.

FRAS discipline: see [`s-field-discipline.en.txt`](../spec/public/s-field-discipline.en.txt)
and the authoring contract served by the live Guide. Every object enforces
the FRAS v2 density limits owned by `internal/machinecontract`; Legacy v1
entries are read-only and no longer authored.

## 7. Guide commands run verbatim

After the first successful Guide, `commands` are bound to the current absolute
binary path. Keep the PowerShell call operator `&`, the quoted paths, and the
returned global arguments; never fall back to bare `aoci`, rely on an
interactive PATH, or reuse an old command, `plan_id`, `run_id`, or digest.

## 8. aoci_update_entry three states

`aoci_update_entry` is the only Entry write. Each submitted machine batch
reports one terminal state in `status`:

- `applied` — the batch completed validation, Diff, P-23 review, atomic
  Apply, and audit. Do not resubmit it; follow the returned `next_action` and
  `next_commands`, call `aoci_maintain` again while `remaining` is nonzero,
  and close the final batch with Verify, aggregate Check, and Guide.
- `repair_required` — repairable candidate content errors with zero formal
  writes. Stable contract: `applied=0`, `remaining=attempted`,
  `formal_writes_started=false`, `preserve_other_candidates=true`,
  `retry_scope` naming only the failed objects, findings each carrying
  `candidate_index`, `path`, `canonical_object_identity`, `domain`, `field`,
  `rule_code`, `expected`, `actual`, `cause`, and `safe_repair_action`. Repair
  only the named candidates, keep every other candidate byte-for-byte, keep
  `path`, `candidate_id`, and `source_sha256`, and resubmit the same complete
  batch. Never ask the user to reply "continue", never shrink the batch to the
  repaired candidates, never bypass machine validation, never regenerate
  unrelated correct candidates. Source-SHA drift, CAS conflicts, pending
  Recovery, third-party conflicts, and formal-write or post-write audit
  failures remain `stopped` and must not be relabeled.
- `stopped` — the automatic flow cannot continue safely. Read the `stop`
  facts and the audit. The current version additionally closes stopped write
  attempts from evidence: proven zero-write closes the attempt and fetches a
  fresh batch; a complete Intent with a provable postimage resumes; a
  policy-selected rollback with the exact preimage rolls back and replans.
  Only approval boundaries, unprovable recovery state, third-party byte
  conflicts, `scope_change_required`, `legacy_read_only_migrate_required`,
  and real environment failures stop at the user.

Unified principle: relaxed toward user stops, never relaxed toward formal
asset quality; content errors self-repair and continue; only consistency,
approval, write-state, or environment failures stop.

## 9. Non-ASCII JSON never crosses a text pipeline

Entries are written only through `aoci_update_entry`; MCP stdio is raw UTF-8
JSON-RPC and needs no staging file (section 11.1). Governance commands that
still take a file (`scope preview --candidate-file`, `scope approve
--approval-file`, the `cognition migration` snapshot and mapping files) read
UTF-8 files, because Windows PowerShell 5 text pipelines may re-encode
non-ASCII content through the local code page. Write UTF-8 without BOM from
PowerShell 5 via `[System.IO.File]::WriteAllText` with
`New-Object System.Text.UTF8Encoding($false)`; never save non-ASCII JSON with
plain `>` or pipe it into a command.

## 10. PowerShell 5 native JSON capture

Ordinary CLI `--json` output on Windows is ASCII-safe JSON and can be captured
directly:

~~~powershell
$raw = & 'C:\aoci\bin\aoci.exe' --repo $repo verify --json
$exitCode = $LASTEXITCODE
$result = ($raw -join [Environment]::NewLine) | ConvertFrom-Json
~~~

`aoci_update_entry` results do not pass through PowerShell: judge them by
`status` as section 8 says, never by the call succeeding. Keep stdout and
stderr separate; never contaminate parseable JSON with `2>&1`. MCP stdio
continues to use raw UTF-8 JSON-RPC.

## 11. Request size and batch caps

Protocol hard caps exist for `aoci_update_entry` batches (at most 200
candidates, the `code_cognition_batch_entries` ceiling) and for every FRAS v2
field (F ≤160 runes; R ≤8 items and ≤360 runes; A ≤6 items and ≤400 runes; S
≤600, ≤200, or ≤50 runes by importance band). The numeric authority is
`internal/machinecontract/numeric.go`; Guide and CLI Help derive from that
contract at runtime.

### 11.1 Volumes authoring batches are sized for the Host window; no staging files

Below those caps, each Volumes Maintain issues one current machine batch of the team size `code_cognition_batch_entries` (default 50, never above the 200 ceiling, and cut earlier by the transport budget `maintain_transport_budget_bytes`, default 24 KiB). The batch is sized to be authored inline as one `aoci_update_entry` call, and the Maintain response cuts per-file governance enumerations to a sample plus complete counts (`governance.list_truncation`, `sets.review_total`), so it reads inline in any Host; candidates, plans, and receipts are always complete. A lost context is recovered by calling Maintain again, which reproduces the same batch and candidate identities.

Never move candidate or Entry text through helper scripts or shell command lines. The real failure chain: an oversized batch → the Host spills the tool result to its own data directory (for Claude Code `%USERPROFILE%\.claude\projects\<project>\…\tool-results\`, not written by aoci) → the model writes Python to parse and merge → on Chinese Windows `open()` defaults to GBK and mis-reads UTF-8, quotes inside `python -c "…"` break bash, temporary paths do not exist. If a model still runs Python, set `PYTHONUTF8=1` and pass `encoding='utf-8'` to every `open()`; aoci itself writes only `.aoci/` and the formal Volume files inside the repository (its only write outside is a tiny CAS lock file under the temp directory).

## 12. Missing machine-field compatibility (Legacy)

Legacy Verify, read-only since 0.2.0, reports `result.missing = raw_missing`;
Score and Check expose the raw and pending-curation aliases documented in the
Chinese original. A governed terminal state permits
`raw_missing > 0` only when actionable, pending-curation, orphan, stale, and
unbaselined are all zero and the remainder is explained by curation exclusions
or non-pending technical skips. Volumes verify returns the
`layout_mode`/`volumes`/`governance` structure instead.

## 13. Apply and safe retry

Principles: zero writes (`formal_writes_started=false`, or proven zero-write)
may retry along the recovery action; once formal writes have started, never
resubmit blindly, the receipt decides between Resume and Rollback; a post-write
audit failure requires read-only Verify/Check/Guide first; never infer write
state from the exit code in either direction; `repair_required` resubmits the
same complete batch after reading findings; Warning-only batches may Auto
Apply; F/R/A/S are bounded by the FRAS v2 limits and wording quality is never a
machine block.

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
`.codex/config.toml`; Guide commands do not depend on PATH; the three
`aoci_update_entry` states behave per section 8 (repair_required writes
nothing and never asks the user to continue); JSON parses directly in PowerShell 5;
MCP stays raw UTF-8 JSON-RPC; the final Guide returns complete or an accurate
stop.

## 16. Minimum Windows acceptance matrix

Complete cognition: the full index body arrives (chunked when large); no
repeated Overview inside a valid context; the three refresh reasons adjudicate
per contract; drift is maintained and proven aligned before a refresh; the
semantic threshold triggers at the machine-owned count; MEMORY never overrides
the current index and source.

Model-only semantics: Header, tags, FRAS, and Scope or Curation decisions are
model-generated item by item; no AST/path/template/script ghost-writing.

`aoci_update_entry` repair, JSON/UTF-8 behavior, and final governance follow
the matrices in the Chinese original: clean batches apply, injected candidate
errors return `repair_required` with zero formal writes, UTF-8 content
round-trips losslessly, and the final Guide converges to aligned/complete with
Missing, Stale, and Orphan at zero or explained by explicit Scope decisions.
