# Getting Started

AOCI-CODE, built on AOCI (AI-Oriented Cognition Infrastructure), provides a
local-first, maintained Cognition Layer beneath AI Agents. It persists high-value
responsibilities, relationships, public contracts, and constraints in a
governed, Git-reviewable `aoci.txt`.

New projects start directly in a semantic-free Code-only Volumes layout. A
governed Legacy repository uses the live `aoci cognition` Guide and capability
manifest. Public command and write classes are defined by the
[`AOCI-CODE CLI Runtime Contract`](../spec/public/aoci-code-cli-runtime-v1.txt).

AOCI-CODE is not an AI Agent, RAG replacement, AST replacement, CodeGraph
replacement, vector database, daemon, or cloud service. AI Agents and
structural tools still inspect real source and tests. AOCI-CODE helps them
reuse repository-level understanding and governs candidate cognition before
it becomes formal.

## Build and install

Build from a source checkout:

```bash
git clone https://github.com/aoci-spec/aoci-code.git aoci-code
cd aoci-code
go mod verify
mkdir -p build
make build
./build/aoci --version
```

Copy the resulting binary to a stable absolute path. See
[`install.md`](install.md) for platform guidance and the distinction between
the current source build and GitHub Releases available after public release.

## Initialize a disposable example

Copy [`examples/minimal-repository`](../examples/minimal-repository) to a directory outside this repository and initialize it:

```bash
git init
git add .
git commit -m "initial example"
/absolute/path/to/aoci --repo . init --locale en-US --agent codex
/absolute/path/to/aoci --repo . scan
```

`init` creates Root, Meta, and an empty Code Volume; Database remains absent.
It never invents repository or Database semantics. A host AI Agent must follow the
installed repository contract and current AOCI Guide to build complete
cognition.

`scan` is not optional: it establishes the governed Baseline that every later
step binds to. Without it the Guide reports `blocked` with a `baseline_missing`
stop and a `scan` command, and no authoring path exists yet — an initialized
Volumes repository with zero Entries is not a Bootstrap or Legacy Plan target;
its only path is scan, then Guide, then no-argument Maintain.

Fresh initialization uses Code-only Cognition Volumes. In that layout the host
follows the live Guide, calls ordinary no-argument `aoci_maintain`, submits every
candidate in the complete current machine-issued batch through
`aoci_update_entry`, and finishes with Verify, Check, and Guide. Repeat Maintain
only when a successful batch reports remaining work.

`status --deep`, `index score`, and `index agent plan` are Legacy-only workflow
commands. They are not the Fresh/Volumes onboarding or maintenance path.

## Choose an execution mode

- Agent-native: Codex, Claude Code, Cursor, or another MCP-capable host generates semantics while AOCI-CODE governs them.
- Endpoint-native: the optional AI layer sends source only to the endpoint explicitly configured by the user.
- Deterministic-only: keep AI disabled and use scanning, status, validation, querying, and CI without semantic generation.

See [`agent-integrations.md`](agent-integrations.md) for host setup and data-flow boundaries.

## Verify alignment

After the guided workflow reports complete and aligned:

```bash
aoci --repo . verify
aoci --repo . check
```

These commands do not change the formal index or Baseline, but may append local audit evidence when Ledger is enabled; Verify may also write Verify History.
