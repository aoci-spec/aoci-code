# Getting Started

AOCI-CODE (AI Software System Cognition Infrastructure) provides a local-first,
maintained Cognition Layer beneath AI Agents. It persists high-value
responsibilities, relationships, public contracts, and constraints in a
governed, Git-reviewable `aoci.txt`.

For a new Agent-integrated project, the recommended path first installs the
Host integration and then uses the existing Fresh Bootstrap to author a
project-specific Root, Meta tag dictionary, and Entries before activating the
Volumes layout. The fixed generic starter remains an explicit compatibility
fallback. A governed Legacy repository uses the live `aoci cognition` Guide
and capability manifest. Public command and write classes are defined by the
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
/absolute/path/to/aoci --repo . init --locale en-US --agent codex --cognition project
```

Project cognition mode writes configuration, Managed Scope and Budget policy,
`AGENTS.md`, and runs the selected Host integration adapter before starting the existing
Fresh Onboarding session. It does not create Root, Meta, Code, Database, or a
Baseline before Bootstrap Apply. The Host follows the returned Onboarding next
actions, authors the project-specific Root, Meta tag dictionaries, and complete
Entries, and lets the existing Root-last transaction publish the first governed
Baseline. Project cognition mode must not run `aoci scan` before Bootstrap.

For automation, Fresh Batch, Candidate-binding, and Session status JSON expose
the versioned `next_action_contract`. Execute its exact command and populate
only the Host-authored fields in its request template; preserve every machine
identity unchanged. When an action result does not contain the next contract,
query the active Onboarding status instead of guessing the transition. Do not
guess Completion or Candidate JSON, copy an internal state machine, or call
post-initialization MCP tools before formal cognition exists. Candidate
provenance binding likewise uses the exact read-only validation action emitted
by the live contract, so a Host never needs AOCI source code or an internal hash
implementation.

The lower-information generic starter may be selected directly in an untouched
repository. If a project-specific Fresh Session has already started, it may be
selected only after a safe abort while no approval artifact, formal write,
pending transaction, Baseline, or Recovery exists:

```bash
/absolute/path/to/aoci --repo . cognition onboard abort
/absolute/path/to/aoci --repo . init --locale en-US --agent codex --cognition generic
/absolute/path/to/aoci --repo . scan
```

Generic fallback reuses the fixed starter taxonomy. It is not a substitute for
repairing a candidate, source/CAS drift, an approval boundary, or Recovery.

After either Fresh Bootstrap or the explicit generic path has activated
Code-only Cognition Volumes, later maintenance follows the live Guide: the Host
calls ordinary no-argument `aoci_maintain`, submits complete machine-issued
batches through `aoci_update_entry`, and finishes with Verify, Check, and Guide.
Repeat Maintain only when a successful batch reports remaining work.

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
