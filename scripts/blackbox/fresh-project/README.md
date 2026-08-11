# Fresh project release black box

This acceptance test treats the selected `aoci` executable as the complete
product. The harness imports only the Go standard library, creates an isolated
Git subject, and consumes CLI stdout JSON plus raw MCP JSON-RPC. It does not
read the AOCI checkout, import an `internal` package, or reproduce the Candidate
payload digest.

Run it against an unpacked release asset:

```bash
AOCI_BIN=/absolute/path/to/aoci \
  go test ./scripts/blackbox/fresh-project -run TestFreshProjectReleaseBinary -count=1 -v
```

PowerShell:

```powershell
$env:AOCI_BIN = 'C:\absolute\path\to\aoci.exe'
go test ./scripts/blackbox/fresh-project -run TestFreshProjectReleaseBinary -count=1 -v
```

The test covers project-mode OpenCode initialization, the pre-Bootstrap
`aoci_rules` route, repeatable context-budgeted authoring batches, machine
Completion templates, one strict zero-write rejection, the machine Candidate
draft, source-independent Candidate binding, split provenance Preview,
policy-bound auto Apply, Verify/Check/Guide closure, and the exact raw MCP
nine-tool surface with post-Bootstrap Rules and Overview.

`AOCI_BIN` is intentionally required. An ordinary `go test ./...` skips this
release gate instead of silently substituting a source-tree build. Release
automation must unpack each target asset and set `AOCI_BIN` to that artifact.
