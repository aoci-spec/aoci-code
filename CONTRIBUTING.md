# Contributing to AOCI-CODE

Thank you for helping improve AOCI-CODE. Start with a focused issue or design
statement before preparing a substantial change.

## Development setup

Requirements are Git, the Go version declared in `go.mod`, and a POSIX shell
for repository scripts.

```bash
git clone https://github.com/aoci-spec/aoci-code.git
cd aoci-code
make fast
```

Use focused tests during development. `make fast` is the ordinary contribution
gate. `make full` is required for stable freezes and high-risk changes when
requested by maintainers. `make release-check` is reserved for package
rehearsal.

## Change rules

- Preserve MCP stdio stdout for JSON-RPC and send diagnostics to stderr.
- Keep the binary name `aoci` and treat the nine MCP tool names as a public
  compatibility surface.
- Put user-visible text in Locale assets instead of hard-coding prose in Go.
- Update `spec/public/` and compatibility tests when changing formats, parsing,
  configuration, CLI/MCP schemas, or compatibility behavior.
- Use the existing lock, CAS, validation, atomic-write, and recovery pipeline
  for formal assets.
- Do not generate cognition semantics from paths, filenames, ASTs, imports,
  regular expressions, fixed templates, or rule engines.
- Do not refresh Golden files solely to make tests pass. A Golden change needs
  an intentional behavior change and production-path evidence.
- Keep private security details, credentials, proprietary source, research
  archives, and non-public design material out of issues and pull requests.

## Change evidence

A contribution should explain:

- the problem and intended boundary;
- affected public contracts and compatibility impact;
- tests run and their results;
- operating-system impact;
- migration or recovery behavior for persistent data changes;
- whether MCP tool, protocol, JSON, or data-format identity changes.

Contributors must have the right to submit their work. Accepted contributions
are governed by the repository license and any published inbound contribution
terms. Maintainers may require additional contributor documentation before
merging.

## Security reports

Follow [SECURITY.md](SECURITY.md). Never place exploitable details, credentials,
private endpoints, or personal data in a public issue.
