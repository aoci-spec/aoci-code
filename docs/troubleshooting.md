# Troubleshooting

## Confirm the executable actually in use

Record the absolute path, `aoci --version`, and file SHA-256. For MCP, restart the host and compare `serverInfo.version`; a matching binary on disk does not prove that the active server was restarted.

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

Run `status --deep`, then follow the current Guide. Do not infer recovery solely from old logs or documentation; Plan and Guide are bound to current repository evidence.
