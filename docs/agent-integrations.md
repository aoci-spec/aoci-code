# AI Agent Integrations and Execution Modes

AOCI-CODE runs beneath a host AI Agent. The AI Agent performs semantic
understanding; AOCI-CODE supplies persistent cognition and deterministic
governance. Host configuration must use the same absolute binary and repository
root whose identity was verified.

After writing or changing an MCP configuration, first check whether the current
host session already exposes the AOCI tools. Refresh or reopen that project
session only when it has not loaded the new server; hosts that support dynamic
MCP reload do not require a blanket application restart.

For a new project, add `--cognition project` to run the selected Host integration adapter
before the existing Fresh Onboarding Plan is frozen. The current session can
complete that CLI-governed authoring flow even when the newly written MCP
configuration is not hot-loaded. After Bootstrap completes, verify the loaded
server by actually calling `aoci_rules`; refresh or reopen only if that call is
unavailable. Project mode must not run `aoci scan` before Bootstrap.

During Fresh Onboarding, consume the current JSON `next_action_contract` as the
complete transport instruction. Use its exact command and request template,
preserve its machine identities, fill only its declared Host-authored fields,
and continue from the contract returned by the next response. Do not guess a
Completion or Candidate shape, reproduce the lifecycle in a wrapper, inspect
AOCI source, or implement the Candidate digest. The live contract supplies the
read-only binding action before Preview. If an action result does not include a
new contract, query the active Onboarding status rather than infer a transition.

## Codex

```bash
aoci --repo /absolute/path/to/repository init --agent codex --cognition project
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
aoci --repo /absolute/path/to/repository init --agent claude --cognition project
```

This merges an `aoci` server into project `.mcp.json`. Add `--hooks` only if the optional Claude Code `PreToolUse` integration is desired:

```bash
aoci --repo /absolute/path/to/repository init --agent claude --hooks --cognition project
```

The hook is a thin pre-write guard, not an AI Agent runtime and not a replacement governance flow.

## OpenCode V1

```bash
aoci --repo /absolute/path/to/repository init --agent opencode --cognition project
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
aoci --repo /absolute/path/to/repository init --agent cursor --cognition project
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

## Explicit generic fallback

Omitting `--cognition` preserves the existing init behavior for compatibility.
The fixed starter may be selected directly in an untouched repository, or after
a project-specific Fresh Session is safely aborted before approval or any
formal transaction:

```bash
aoci --repo /absolute/path/to/repository cognition onboard abort
aoci --repo /absolute/path/to/repository init --agent <host> --cognition generic
aoci --repo /absolute/path/to/repository scan
```

An active Session, Baseline, formal cognition asset, approval artifact, pending
transaction, or Recovery makes this fallback fail closed. Repairable authoring
or copied machine-identity errors remain on the original Fresh Session.

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
with the exact command, `--agent`, applicable action payload schema and request
file, expected preimage, Plan/Run identity, retry rule, and success
continuation. Hosts should correct
at most one purely transport-schema mismatch; semantic repair still follows
the normal model-owned path.

Fresh Onboarding returns the same style of executable contract directly from
its active Session. A Host loops only by consuming each newly returned
contract; it does not infer a Fresh state transition from prose or from a
previous response.

Protected approval commands expose `aoci-host-interaction/v1` when no real TTY
is attached. A PTY-capable Host can use its exact command and confirmation
phrase. AOCI preserves the TTY and no-self-approval guard; this is not evidence
that Codex Desktop or another external Host automatically allocates a PTY.

For every host, obtain `aoci_rules` once and establish one complete Overview at
the start of a normal task. If it reports `continuation_required=true`, submit
the exact `next_cursor` automatically through the final Chunk before beginning
the task or stating a system conclusion. Do not ask the user to continue and do
not replace missing Chunks or failed Attestation with Memory, source, Spec,
direct `aoci.txt`, historical sessions, scoped reads, Search, or Entry lookup.
Stop the cognition chain on Host truncation, missing/duplicate/reordered
Chunks, cursor error, or Index/configuration change. Attestation failure stops
complete-cognition acceptance and semantic retry, not honest answers or a
source-bound task. The formal ordinal is the
1-based position in the shared Entry/object sequence; Header content, comments,
blank lines, markers, receipts, and Metadata are excluded. After the chain,
submit the existing model cognition Attestation once; only one schema-only
correction is allowed. Normally expose only one concise result sentence that
separates machine Index coverage from self-assessed framework mastery. Keep
reusing the resulting cognition while reliable. Context compaction,
the project semantic threshold, and a major phase transition are checkpoint
facts; they advise but do not decide whether the AI Agent needs the system-wide
view. `check_only=true` returns only compact status. Every ordinary explicit
Overview delivers the complete requested scope when a coherent snapshot is
available. Local doubts should still prefer source investigation,
`aoci_get_entries`, or `aoci_search`.

For a complete, Host-confirmed and governance-aligned compaction refresh,
partial or failed Attestation consumes the current generation, leaves the
receipt uncertain, disables a full-system claim, and returns source-bound
continuation guidance. Do not loop Overview in the same generation. In a
long-running auto task, answer non-control questions briefly and resume the
existing next action; only explicit stop/pause/cancel/rollback/scope or
commit/Push changes control execution. A stopped write attempt is replanned,
resumed, or rolled back only when existing formal-write and Recovery evidence
proves that action. These are Host rules, not another persistent Task state.

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
