# Troubleshooting

## Confirm the executable actually in use

Record the absolute path, `aoci --version`, and file SHA-256. For MCP, first
check the server loaded by the current Host and compare `serverInfo.version`.
If the target server is not loaded or an old version remains active, refresh or
reconnect that project's MCP integration, or reopen the project session when
the Host requires it. A matching binary on disk does not prove that the active
server is using those bytes.

## MCP closes with EOF

stdio MCP is incremental. Keep stdin open, send `initialize`, wait for its response, send `notifications/initialized`, and only then send requests such as `tools/list`. MCP stdout must contain JSON-RPC only; inspect stderr for diagnostics.

## Verify or Check wrote files

They do not change the formal index or Baseline. With Ledger enabled they may append local audit events, and Verify may write Verify History. Use an isolated copy when an audit requires literally zero filesystem writes.

## A run returns `repair_required`

Repair only the candidates explicitly identified by the current response, preserve their source and candidate/batch bindings, and resubmit the complete current machine-issued batch. Do not drop unrelated candidates or construct a replacement state manually. If `remaining` is nonzero after a successful Apply, call Maintain again; do not reduce Scope or slice the returned batch to fit transport.

## A run returns `stopped`

Stop at the reported failed step and follow its recovery evidence. Do not bypass CAS, edit manifests, delete pending evidence, or turn a stopped run into an applied result.

## Windows host cannot start MCP

Use an absolute `.exe` path and explicit `--repo` path. Avoid PowerShell 5 text pipelines for signed or non-ASCII JSON. See [`windows-host-agent.md`](windows-host-agent.md).

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
