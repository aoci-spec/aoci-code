# AI Agent Integrations and Execution Modes

AOCI-CODE runs beneath a host AI Agent. The AI Agent performs semantic
understanding; AOCI-CODE supplies persistent cognition and deterministic
governance. Host configuration must use the same absolute binary and repository
root whose identity was verified.

After writing or changing an MCP configuration, first check whether the current
host session already exposes the AOCI tools. Refresh or reopen that project
session only when it has not loaded the new server; hosts that support dynamic
MCP reload do not require a blanket application restart.

The host configuration files written by `aoci init --agent` (`.mcp.json`,
`.claude/settings.json`, `.codex/config.toml`, `opencode.json`) embed
machine-bound absolute binary and repository paths, and must not be committed:
a committed copy is broken on every other machine, and because each installer
detects an existing entry by key presence, re-running `init` there silently
keeps the broken paths.

`init` adds the files it just wrote to the repository's `.gitignore` under its
own marked block, so an ordinary `init` then `scan` leaves them out of Git and
out of Managed Scope. It only ever appends, never rewrites maintainer content,
and it skips any path that Git already tracks — a tracked host configuration is
a decision `init` will not reverse for you. The timing matters: Managed Scope
roles are fixed by the first `scan`, `scan --force` cannot advance them, and
removing a path afterwards is a coverage reduction that requires an approved
Scope Change. If you configure a host by hand, add its file to `.gitignore`
before the first `scan`.

## Codex

```bash
aoci --repo /absolute/path/to/repository init --agent codex
```

This writes or extends project-level `.codex/config.toml`:

```toml
[mcp_servers.aoci]
command = "/absolute/path/to/aoci"
args = ["--repo", "/absolute/path/to/repository", "mcp"]
```

Review the repository before granting host trust. Codex integration does not install a file-edit hook; MCP tools and the Guide govern cognition writes.

## Claude Code

```bash
aoci --repo /absolute/path/to/repository init --agent claude
```

This merges an `aoci` server into project `.mcp.json`. Add `--hooks` only if the optional Claude Code `PreToolUse` integration is desired:

```bash
aoci --repo /absolute/path/to/repository init --agent claude --hooks
```

The hook is a thin pre-write guard, not an AI Agent runtime and not a replacement governance flow.

## OpenCode V1

```bash
aoci --repo /absolute/path/to/repository init --agent opencode
```

This creates or strictly merges the stable OpenCode V1 project configuration at
the repository root, `opencode.json`:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "aoci": {
      "type": "local",
      "command": [
        "/absolute/path/to/aoci",
        "--repo",
        "/absolute/path/to/repository",
        "mcp"
      ],
      "enabled": true
    }
  }
}
```

If the current OpenCode project session already exposes the AOCI tools, no
restart is required. Otherwise refresh or reopen that project session and then
verify the loaded server. OpenCode may display MCP tool names with the server
prefix, for example `aoci_aoci_rules`; the AOCI Server itself still exposes the
same nine tool identities.

The first implementation intentionally does not merge OpenCode V2
`mcp.servers` documents or JSONC configuration. It fails closed instead of
creating a competing configuration or stripping comments; configure those
formats manually after reviewing the installed OpenCode version.

## Cursor

```bash
aoci --repo /absolute/path/to/repository init --agent cursor
```

The current release candidate prints a reference `.cursor/mcp.json`
configuration but does not write it. Verify the syntax against the installed
Cursor version before adding it manually:

```json
{
  "mcpServers": {
    "aoci": {
      "command": "/absolute/path/to/aoci",
      "args": ["--repo", "/absolute/path/to/repository", "mcp"]
    }
  }
}
```

This limitation must remain visible in compatibility claims; a reference template is not native-host validation.

## Deterministic offline mode

AI is disabled by default. To make that state explicit:

```bash
aoci --repo . ai setup --disable --local
aoci --repo . ai status
```

Initialization, scanning, inventory, status, validation, querying, governed writes, and CI can operate without an AI endpoint. New repository semantics still require a host AI Agent or configured endpoint; AOCI-CODE does not fabricate them.

## User-owned endpoint mode

Configure only the endpoint and the name of an environment variable that holds its secret:

```bash
export AOCI_AI_KEY='set-outside-command-history'
aoci --repo . ai setup --local --enable \
  --provider openai-compatible \
  --base-url https://endpoint.example.invalid/v1 \
  --model model-id \
  --key-env AOCI_AI_KEY
```

`--local` keeps machine-specific endpoint configuration out of Git. AOCI never accepts the secret value as a flag and never stores it in project configuration. `aoci ai test` performs a real network request; run it only intentionally.

## Shared workflow boundary

A Host should read the machine capability document once for the current binary
and repository identity:

```bash
aoci --repo /absolute/path/to/repository --json capabilities
```

It reports the exact nine-tool surface, lifecycle and recovery support, current
schema versions, layout, TTY boundary, Database boundary, and Overview delivery
modes. Machine Guide results then provide a complete `next_action_contract`
with the exact command, `--agent`, request schema/file, expected preimage,
Plan/Run identity, retry rule, and success continuation. Hosts should correct
at most one purely transport-schema mismatch; semantic repair still follows
the normal model-owned path.

Protected approval commands expose `aoci-host-interaction/v1` when no real TTY
is attached. A PTY-capable Host can use its exact command and confirmation
phrase. AOCI preserves the TTY and no-self-approval guard; this is not evidence
that Codex Desktop or another external Host automatically allocates a PTY.

For every host, obtain `aoci_rules` once and establish one complete Overview
at the start of a normal task, then keep reusing that cognition while it
remains reliable. The binding rules for chunk continuation, Attestation, the
single same-response schema-only correction, prohibited cognition supplements,
and the prescribed one-sentence result are carried by the `aoci_rules`
contract and documented in [`overview-delivery.md`](overview-delivery.md);
this page does not restate them. Checkpoint facts (context compaction, the
project semantic threshold, a major phase transition) advise but never decide
whether the AI Agent needs the system-wide view; `check_only=true` returns
only compact status, and every ordinary explicit Overview delivers the
complete requested scope when a coherent snapshot is available. In a
long-running auto task, answer non-control questions briefly and resume the
existing next action; only explicit stop/pause/cancel/rollback/scope or
commit/Push changes control execution, and a stopped write attempt is
replanned, resumed, or rolled back only on proven formal-write and Recovery
evidence. Local doubts should still prefer source investigation,
`aoci_get_entries`, or `aoci_search`.

When checkpoint facts show semantic drift, follow the live Maintain and Guide
path to alignment. A dirty ordinary Overview may expose the complete formal
body but cannot claim current reliable cognition. Do not copy the Guide into
host prompts or hard-code its state transitions in wrappers. Full details are in
[`cognition-refresh.md`](cognition-refresh.md). The machine interaction contract
is specified by
[`aoci-host-capability-and-interaction-v1.txt`](../spec/public/aoci-host-capability-and-interaction-v1.txt).
Whole-Index transport and Host confirmation are specified by
[`aoci-overview-delivery-v1.txt`](../spec/public/aoci-overview-delivery-v1.txt).

Without a normal AOCI MCP, an AI Agent may read `aoci.txt` in segments as a manual
fallback or diagnostic. This is not equivalent acceptance because it lacks the
Chunk-chain proof, governance and Baseline state, pending-transaction
protection, and model Attestation.

AOCI-CODE is not a substitute for source reading, tests, LSP, CodeGraph, code review,
or the host AI Agent.
