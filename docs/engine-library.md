# Embedding the engine

The `aoci` binary is one kernel with two entry points, the CLI and the MCP
server. The Go package `github.com/aoci-spec/aoci-code/engine` is that same
kernel as a third entry point: a program imports it to open a repository, read
its formal cognition and governance facts, and run the nine MCP tools in
process. Nothing is implemented a second time: each read is the loader, the
delivery framer, or the governance assessment the CLI and the tools use, and
each write is one of the nine tools, executed over an in-memory transport.
The contract is [`aoci-engine-library-v1.txt`](../spec/public/aoci-engine-library-v1.txt).

The engine keeps its boundaries when embedded: it calls no model, holds no
credential, and reaches no remote service. The program that embeds it is the
host.

## Reading a repository

```go
repo, err := engine.Open("/path/inside/the/repository") // ascends to .git, then .aoci
cog, err := repo.Cognition()       // layout, assets with SHA-256, every Entry
ov, err := repo.Overview("all")    // the exact delivered body; ov.SHA256 == body_sha256
gov, err := repo.Governance()      // the facts verify, check, and the Guide share
id, err := repo.Identity()         // what a published cognition is
```

`Cognition` lists every Entry in formal order with its Volume, domain, object
kind, canonical identity (`code:<path>` or a `database://` reference), path,
tag, and F/R/A/S fields, taken from the formal line without rewriting.
`Overview` frames the body exactly as `aoci_overview` delivers it, so the
`SHA256` and `Bytes` it reports are the `body_sha256` and `body_utf8_bytes` a
Host confirms. `Governance` returns the decision (`Aligned`, `Result`,
`NextRequiredAction`), the findings and drift counts, and the complete fact
set as JSON in `Facts`, without writing Verify History or a Ledger event.
`Identity` carries the SHA-256 of each formal asset, the composite identity,
the complete-scope identity, the Baseline digest, and the engine version: two
clones with equal identities carry the same cognition.

`Overview` and `Governance` are Volumes v1 operations and return
`ErrLayoutUnsupported` on a Legacy single-file layout, which is read-only
since 0.2.0; `Cognition`, `Identity`, and the tools still answer there.

## Running the tools in process

```go
session, err := repo.Connect(ctx)
defer session.Close()
names, err := session.Tools(ctx)                       // the nine tool names
rules, err := session.Call(ctx, "aoci_rules", nil)      // rules.Text
batch, err := session.Call(ctx, "aoci_maintain", nil)   // batch.Text is the Maintain JSON
```

`Call` runs the tool's own handler and returns the tool's own result: the
text (JSON for the write tools, the documented text contract for the read
tools), `IsError` when the tool refused, and the `_meta` the server attached.
A refusal is a result, not a Go error; a Go error means the call could not be
made. The in-process session keeps the per-server state the stdio server
keeps (refresh generation, reused Overview deliveries), so a long-lived
program holds one session per repository rather than reconnecting per call.

`repo.ServeStdio(ctx)` serves the same nine tools over the process's standard
streams exactly as `aoci mcp` does; stdout then carries JSON-RPC only, so the
program must keep its own output on stderr. `repo.NewMCPServer()` returns the
assembled server for any other transport the MCP SDK offers.

## Version and build identity

`engine.Version()` reports the version a build injected into the binary, else
the module version the Go toolchain recorded for the importing program, else
`dev`. The injected identity lives in `internal/buildinfo`, which is the
ldflags target of the Makefile, GoReleaser, and the clean-room smoke build,
so the CLI's `--version` and the library report one value.

## Importing the module

Until the reviewed openGauss connector is published as a module, an importer
carries the same `replace` directive the engine's `go.mod` uses, pointing at
the connector tree inside the engine checkout:

```
require github.com/aoci-spec/aoci-code v0.0.0
replace github.com/aoci-spec/aoci-code => ../path/to/aoci-code
replace gitcode.com/opengauss/openGauss-connector-go-pq => ../path/to/aoci-code/third_party/openGauss-connector-go-pq
```

Without the second directive the build resolves the unpatched upstream
connector and fails to compile. [`examples/engine-embed`](../examples/engine-embed)
is a complete program: it prints the layout, Entry count, identity, and
alignment of a repository, and with `--mcp` serves the nine tools over stdio.

One process serves one repository Locale at a time: `Open` binds the runtime
text catalog, which is process-wide, to the repository's Locale.

## Who this is for

A scheduler that embeds the engine (pinning its version through `go.mod`)
reads Entries and governance facts structurally instead of parsing Volume
text, and exposes the tools to its agents from its own process. A remote
service validates a published cognition at a commit with `Identity` and
`Governance` in a temporary checkout, using the kernel rather than a second
implementation of the format.
