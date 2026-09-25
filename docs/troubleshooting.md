# Troubleshooting

## Confirm the executable actually in use

Record the absolute path, `aoci --version`, and file SHA-256. For MCP, first
check the server loaded by the current Host and compare `serverInfo.version`.
If the target server is not loaded or an old version remains active, refresh or
reconnect that project's MCP integration, or reopen the project session when
the Host requires it. A matching binary on disk does not prove that the active
server is using those bytes.

The running server also watches its own binary: when the on-disk file no longer
matches what the process started from, `aoci_maintain`, `aoci_rules`, and the
final Overview metadata carry `service_binary_replaced_on_disk: true`. The fact
is advisory only — nothing blocks — and it means exactly one thing: restart the
host MCP integration to load the replaced binary.

## Section roots show an old absolute path

Code Volume section headers such as `===/old/machine/path/project/===` are
historical structural coordinates, not runtime paths. The public index format
defines them that way: after a clone or relocation the formal bytes are
preserved, and every reader derives repository-relative identities from the
invocation root, never from the recorded prefix. An outdated prefix is
expected, harmless, and not worth a formal write to rewrite.

A repository whose own path holds a space, `=`, `(`, or `（` shows two spellings
of that prefix: the full one in its root section and a truncated one in every
later section. Every release writes it that way, because the original reading
of a header stops at that character and later sections continue what was read
back. Both resolve to the same repository root, at the origin and in a checkout
elsewhere; do not rewrite the headers by hand to make them match.

An index authored inside a git worktree nested under the primary checkout,
such as `<repo>/.worktrees/wt`, records that worktree as its root. Once the
branch is merged, the primary checkout reads the same bytes with the recorded
root below its own; the reader recognises the family and resolves every Entry
against the recorded root, so the primary checkout, the worktree, and an
unrelated clone all read one index (#77). Releases up to v0.1.0-rc14 filed
every Entry under `.worktrees/wt/` from the primary checkout instead, which
looked like the whole index missing and orphaned at once.

## Maintain or an update stops with `directory_unspellable`

A candidate lives in a directory the index cannot record: its name begins or
ends with whitespace, so no section header for it reads back to the same path,
or, far more rarely, the header it would get already resolves to another
directory of this index. Its Entries would resolve somewhere else, and no edit
to an Entry can change that. `aoci_maintain` finds it before issuing the batch
and answers `stopped` with `next_action: resolve_unspellable_directory` and
`stop` facts that quote the directory (`code_directory_unspellable`); it issues
no candidates, because a model would author the whole batch only for the update
to stop. A caller that submits such a path anyway gets the same `stopped` answer
from `aoci_update_entry`, never `repair_required`.

Two things clear it, and then `aoci_maintain` issues the batch again:

- Rename the directory.
- Take its files out of the index role: with a governed scope rule of kind
  `glob` or `file`, for example `trail /**`, or by listing the exact files under
  `curation_exclude` before the first scan. A `directory`-kind pattern is
  trimmed, so it cannot name a directory with leading or trailing whitespace,
  and a `curation_exclude` entry names a file, never a directory.

`code_root_unspellable` is the same stop for the repository root itself, which
happens only when no part of the root path reads back as a usable root: a
repository directory directly under `/` or a drive root (or under nothing but
such segments) whose name begins with `(`, `（`, `=`, or whitespace. No scope
rule helps there; move or rename the repository directory. A root whose first
segment merely begins with such a character, or that has a segment with leading
or trailing whitespace, is not refused: the index records the part of the path
that a header can carry, and resolves the same way at the origin and in every
checkout.

## Host config points to a moved binary or repository

After the `aoci` binary or the repository moves, host configs written by
`aoci init --agent` keep the old absolute paths: the MCP server fails to start
while `aoci doctor` still reports the Claude or Codex integration as installed,
because doctor and the installers check entry presence, not path validity.
OpenCode instead fails closed with an `mcp.aoci` conflict. Remove the stale
`aoci` entry (`mcpServers.aoci` in `.mcp.json`, the `[mcp_servers.aoci]` table
in `.codex/config.toml`, `mcp.aoci` in `opencode.json`, and any stale
`PreToolUse` command in `.claude/settings.json`), then re-run
`aoci --repo <root> init --agent <name>` from the new location.

## AOCI tools appear in unrelated projects

Each `aoci mcp` server is bound to the repository named by its `--repo`
argument. If that server is registered in a Host's user-level or global MCP
configuration, every project may expose the same tools, but those tools still
read and govern the one bound repository.

Remove that global entry and configure AOCI inside the intended project:
`.mcp.json` for Claude Code, `.codex/config.toml` for Codex, `opencode.json` for
OpenCode V1, or `.cursor/mcp.json` for Cursor. `aoci init --agent cursor` prints
the Cursor configuration but does not write it. After moving the entry, refresh
or reopen only the intended project session if its tools have not reloaded.

## MCP closes with EOF

stdio MCP is incremental. Keep stdin open, send `initialize`, wait for its response, send `notifications/initialized`, and only then send requests such as `tools/list`. MCP stdout must contain JSON-RPC only; inspect stderr for diagnostics.

## Verify or Check wrote files

They do not change the formal index or Baseline. With Ledger enabled they may append local audit events, and Verify may write Verify History. Use an isolated copy when an audit requires literally zero filesystem writes.

## A run returns `repair_required`

Repair only the candidates explicitly identified by the current response, preserve their source and candidate/batch bindings, and resubmit the complete current machine-issued batch. Do not drop unrelated candidates or construct a replacement state manually. If `remaining` is nonzero after a successful Apply, call Maintain again; do not reduce Scope or slice the returned batch to fit transport.

## A run returns `stopped`

Stop at the reported failed step and follow its recovery evidence. Do not bypass CAS, edit manifests, delete pending evidence, or turn a stopped run into an applied result.

## Files appear under the Host's own data directory

If helper scripts or entry drafts show up under `~/.claude/projects/<project>/…/tool-results/` (or the equivalent for another Host), the Host spilled an oversized tool result to disk and the model kept working next to it. aoci never writes there: its writes are `.aoci/` and the formal Volume files inside the repository, plus a tiny CAS lock file under the system temp directory. The cause is a Maintain response or authoring batch larger than the Host window; current versions size the batch (`code_cognition_batch_entries`, default 20) and bound the governance enumerations so the response fits inline. Upgrade, or lower the team batch size, and let the model author entries directly as `aoci_update_entry` arguments.

## Initial cognition authoring is unexpectedly slow

First separate deterministic AOCI time from Host and model time. When a response
includes `metrics.deterministic_ms`, compare it with the tool call's wall-clock
duration. A small deterministic value with a long wall-clock delay points
outside AOCI's deterministic core. Check first whether the Host asks for
confirmation on every AOCI tool call; after reviewing the exact project-level
server, use the Host's project or session trust controls instead of weakening a
global policy.

The Code authoring batch defaults to 20 entries. A larger batch reduces model
round trips when the Host can carry the response; increase it gradually rather
than assuming the wire ceiling is safe:

```bash
aoci --repo . config set code_cognition_batch_entries 50
```

`overview_delivery.chunk_tokens` affects Whole-Index delivery, not semantic
authoring throughput. Raising it can reduce Overview round trips on a Host that
preserves the complete response. If the Host truncates or spills a result,
lower it and restart the Chunk chain instead:

```bash
aoci --repo . config set overview_delivery.chunk_tokens 24000
```

When the Host controls model selection, a faster model that can still follow
the authoring contract may also shorten the first build. AOCI does not select
the Host model. Change one factor at a time and compare confirmations, model
latency, batch count, and `deterministic_ms` before attributing the delay.

## Windows host cannot start MCP

Use an absolute `.exe` path and explicit `--repo` path. Avoid PowerShell 5 text pipelines for signed or non-ASCII JSON. See [`windows-host-agent.en.md`](windows-host-agent.en.md) (English) or the Chinese original [`windows-host-agent.md`](windows-host-agent.md).

## AI endpoint fails

Run `aoci ai status` first. Use `aoci ai test` only when a real network request is intended. Configuration stores an environment-variable name, so confirm the named variable exists without printing its value. Deterministic offline commands remain available when AI is disabled.

## Alignment does not converge

Follow the current Guide. For Cognition Volumes, call ordinary no-argument
`aoci_maintain`, author every candidate in the complete current machine-issued
batch, submit that batch through `aoci_update_entry`, and finish with Verify,
Check, and Guide. Do not infer recovery solely from old logs or documentation;
the Guide is bound to current repository evidence.

`status --deep`, `index score`, and `index agent plan` diagnose or drive Legacy
repositories only. A `volume_read_only` response from one of those commands
means the command is not the Volumes route; it does not by itself prove that the
CLI and running MCP Server have different versions.

If Maintain stops with `scope_change_required` instead of offering candidates,
no batch will converge anything until the policy is active; the next section
is the exit.

## Maintain stops with `scope_change_required`

Desired policy differs from the active one: a scope rule or a budget was
edited, and every authoring path refuses to write until one governed Apply
activates it. Run:

```
aoci scope activate
```

Sources that changed since the Baseline do not stop this under Volumes v1: the
plan lists them under `source_stale_retained`, keeps their old fingerprints,
and `aoci_maintain` plans them once the policy is active. A candidate set that
carries `entries`, `dispositions`, or a `header` is refused under Volumes with
`managed_scope_volumes_entry_candidates_unsupported`; Entries there are written
only through Maintain. Under the Legacy layout the same state answers
`managed_scope_index_source_stale: <path>` and needs an Entry candidate for
that path; the fields are listed in `docs/managed-scope-and-budget.md` under
"The candidate set". Reverting the edit (`aoci scope rule remove <rule-id>`)
is the other exit in either layout.

If activation answers `managed_scope_human_approval_required` (exit 2), it saved
the preview under `.aoci/scope-change/` and printed the approve and apply
commands. Run them in order, replacing `<id>` with your reviewer identity;
approve requires a real TTY. Review mode and policy relaxations can require
this boundary even without a coverage reduction. With `--json`, the paths and
commands are in the error's `details` object. Existing safety refusals still
require resolving the reported cause.

## The cognition layer must be visible to Git

`scan` takes its file inventory from Git. A formal cognition asset covered by
`.gitignore`, `.git/info/exclude`, or `core.excludesFile` is therefore absent
from the Baseline it publishes, and the Volume it governs can never align.
`scan` refuses with `formal_cognition_assets_git_ignored` and names both the
asset and the rule that hides it; remove the rule and run `scan` again.

`aoci.txt`, `aoci.meta.txt`, `aoci.code.txt`, and `AGENTS.md` are versioned
cognition meant to be committed, exactly as this repository commits its own.
Only the host integration file `init` writes — `opencode.json`, `.mcp.json`,
`.codex/config.toml` — carries machine-bound absolute paths and belongs in
`.gitignore`, which `init` arranges by itself.

## A Volume reports line-ending-only difference

`core.autocrlf=true` is the Git for Windows default, so an ordinary checkout can
rewrite every line ending in the repository. AOCI baselines exact raw bytes, so
those Volumes differ from their Baseline records while carrying identical
content. Under the default `line_ending_tolerance` this is reported as
`code_volume_line_ending_only` (or its `root_`, `meta_`, `database_` siblings)
and does not block authoring, but a Scope Change still requires the original
bytes. Restore LF endings, or set `* text=auto eol=lf` in `.gitattributes` and
check the files out again. `init` writes that file for new repositories and
never rewrites an existing one.

## A tree-wide digest must exclude `.aoci/`

Reproducible-build proofs, release attestations, and review records often hash
the whole tree and commit the result back. `.aoci/baseline.json` is tracked by
design, so a digest that covers it forms a fixed point: the digest depends on
the Baseline, and the Baseline records the fingerprint of the file holding the
digest. No pair of contents satisfies both, and the repository is left with one
permanently stale Entry that reappears after every maintenance round.

Exclude `.aoci/` from any such digest. This is what AOCI does internally —
`internal/fs/walk.go` excludes `.aoci` and `.git` unconditionally, and the
Business Source manifest therefore never contains a governance asset. Nothing
under `.aoci/` participates in a build or in the source a review covers, so
excluding it makes the digest answer its intended question rather than weakening
it.

## The repository must keep a clean working tree

Some build tooling refuses to run while the tree is dirty. AOCI cannot be made
invisible to Git without breaking the Baseline, so the honest options are:

- commit the cognition layer, and do authoring in a dedicated window — each
  authoring batch writes `aoci.code.txt` and `.aoci/baseline.json`, so the tree
  is dirty until those are committed;
- run authoring in a separate `git worktree`, leaving the primary tree clean;
- do not use AOCI in that repository.

Adding the cognition assets to an ignore file is not among them: it produces a
Baseline that omits the Volumes it governs.

## Seeing every repository at once

`aoci ui` opens a loopback-only status page for one or more repositories. On
Linux and WSL it also lists the running `aoci mcp` processes of the current
user with the executable each one loaded, and marks a server whose binary has
since been replaced on disk — the same fact the server self-reports as
`service_binary_replaced_on_disk`, seen from outside the process. The page is
read-only, takes no lock, and writes nothing, so it can stay open beside a
working agent.

```bash
aoci ui --repo /path/to/repository --also /path/to/another --open
```

To keep a panel running after the shell that started it has gone — the case
when an agent starts it for you — use `--detach`; it prints the link and
returns, reuses a panel already running for the repository, and `--stop` ends
it:

```bash
aoci ui --detach --json
aoci ui --stop
```
