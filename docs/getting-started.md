# Getting Started

AOCI-CODE gives an AI Agent a maintained Cognition Layer for your repository:
a governed, Git-reviewable index of what each file is responsible for, what it
depends on, which contracts it exposes, and what must not be broken. The first
week has three steps: install and initialize, connect your Agent and build the
index, then keep it aligned as part of every task.

AOCI-CODE is not an AI Agent, RAG replacement, AST replacement, CodeGraph
replacement, vector database, daemon, or cloud service. Agents and structural
tools still read real source and tests; AOCI-CODE lets them reuse
repository-level understanding and governs candidate cognition before it
becomes formal. Public command and write classes are defined by the
[`AOCI-CODE CLI Runtime Contract`](../spec/public/aoci-code-cli-runtime-v1.txt).

## Step 1: install and initialize (day one)

Install a released binary following [`install.md`](install.md), or build from
source:

```bash
git clone https://github.com/aoci-spec/aoci-code.git aoci-code
cd aoci-code
go mod verify
mkdir -p build
make build
./build/aoci --version
```

Copy the binary to a stable absolute path; the MCP configuration written in
the next step points at it.

Initialize your repository, or a copy of
[`examples/minimal-repository`](../examples/minimal-repository) outside this
checkout if you want to try things first:

```bash
aoci --repo . init --locale en-US --agent codex
aoci --repo . scan
```

`init` creates Root, Meta, and an empty Code Volume, installs the repository
`AGENTS.md` block, and with `--agent` registers the MCP server for that host.
It never invents repository semantics. `scan` is not optional: it establishes
the governed Baseline that every later step binds to. Without it the Guide
reports `blocked` with a `baseline_missing` stop, and no authoring path exists
yet. Fresh initialization uses Code-only Cognition Volumes; Database stays
absent until you declare a source.

`aoci --help` lists the five everyday commands first: `init`, `scan`, `mcp`,
`verify`, and `check`. Everything else sits under Additional Commands and is
explained by `aoci <command> --help`.

## Step 2: connect your Agent and build the index (days two and three)

Open the repository in Codex, Claude Code, Cursor, or another MCP-capable
host; `init --agent` already registered `aoci mcp` for it, and
[`agent-integrations.md`](agent-integrations.md) covers the other hosts and
the data-flow boundaries. The host's model does the authoring; AOCI-CODE
issues, validates, binds, and records it. Nothing leaves the machine and no
model client is involved.

In this layout the host follows the live Guide,
calls ordinary no-argument `aoci_maintain`, submits every candidate in the
complete current machine-issued batch through `aoci_update_entry`, and
repeats Maintain while a successful batch reports remaining work. A first build of a large
repository is a series of such batches; each one is machine-issued, bound to
source digests, and applied atomically. When Maintain reports aligned, the
Agent finishes with Verify, Check, and Guide, and later sessions start with
one `aoci_overview` that delivers the complete index and checks that the model
assimilated it.

Without a host model in the loop, scanning, status, validation, querying, and
CI still run deterministically; only semantic generation needs the Agent.

## Step 3: keep it aligned (every task from then on)

An index is worth only as much as its alignment with the code. Close every
task the same way: the Agent runs `aoci_maintain` once after the files reach
their final state, authors the candidates it is issued, and proves alignment:

```bash
aoci --repo . verify
aoci --repo . check
```

`verify` reports the governance state and `check` is the pre-commit gate;
neither changes the formal index or Baseline, though both may append local
audit evidence when Ledger is enabled. `status` shows a compact alignment
readout at any time, and `check` run in CI catches an index that drifted from
the code before it merges. The repository `AGENTS.md` carries the same closing
rule for the Agent, so a host that reads it closes tasks this way on its own.

`status --deep` is a Legacy-only read command, and `index score` reads both
layouts as a quality readout. A Legacy single-file repository is read-only
since 0.2.0 and migrates with `aoci cognition onboard start --json`; see
[`upgrading.md`](upgrading.md).
