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
