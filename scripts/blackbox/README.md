# Black-box verification suites

Three standalone suites exercise a built `aoci` binary strictly from outside
the process: they speak the public stdio MCP protocol and the public CLI, never
import internal packages, and use only the Python standard library. They ship
with the repository clone (binary Release archives do not include `scripts/`).

| Suite | Proves | Needs | Typical time |
| --- | --- | --- | --- |
| `mcp_conformance.py` | The MCP wire surface honors its contract: handshake, the nine-tool registry, input schemas, response shapes, error behavior, clean handling of malformed input. Read-only. | python3, git, a built binary | seconds |
| `mcp_scenarios.py` | Safety under hostile handling: cursor replay/tampering, write-lifecycle rejections, crash injection during Apply, racing writers. Disposable fixture repositories; the host repository is only read. | same as above | minutes |
| `mcp_lifecycle.py` | Complete lifecycles on three frozen realistic projects (a TypeScript service, a Python + MySQL service, and a 453-file layered service), from `init` through incremental maintenance, database Evidence, drift, and re-alignment. The large fixture additionally exercises multi-batch authoring at the real machine batch limit, including a relation cycle that spans every batch. An optional model track drives a real AI agent through the small repositories. | above + Docker for the `database` suite; OpenCode + a model subscription for the model track | minutes; model track depends on the model |

## Running

Build or obtain a binary first, from the repository root:

```bash
make build          # or: export AOCI_BIN=/path/to/downloaded/aoci
```

Protocol conformance (58 checks, read-only):

```bash
python3 scripts/blackbox/mcp_conformance.py
```

Fault-injection scenarios (30 scenarios, disposable fixtures in a temp dir):

```bash
python3 scripts/blackbox/mcp_scenarios.py
```

Lifecycle, deterministic track only (no AI involved; `database` pulls
`mysql:8.4` and needs a working Docker daemon — drop it via `--suites`):

```bash
python3 scripts/blackbox/mcp_lifecycle.py
python3 scripts/blackbox/mcp_lifecycle.py --suites bringup,incremental,governance
python3 scripts/blackbox/mcp_lifecycle.py --suites scale     # 453 objects, no Docker needed
```

The `scale` suite first authors the 454 objects under the machine-default team
batch (20 per Maintain, about 23 rolling rounds) and asserts every Maintain
response stays under 64 KB — the first Maintain of a fresh 453-file repository
used to weigh 212 KB, which no Host displays inline. It then pins the team batch
at the 200 wire ceiling (`code_cognition_batch_entries`) and is the only suite
that reaches it: 454 objects in three rolling batches, repeated twice more with
relations the model would realistically write — a layered dependency DAG, and a
210-object ring whose members reference each other across every batch. All three
must reach `aligned` in the same three batches: relations are model-authored
semantics and never constrain how the machine schedules a batch.

Lifecycle model track — a real agent authors real cognition over the frozen
fixtures. Install [OpenCode](https://opencode.ai), authenticate once
(`opencode auth login`), pick any id from `opencode models`:

```bash
python3 scripts/blackbox/mcp_lifecycle.py --model opencode/claude-sonnet-5
python3 scripts/blackbox/mcp_lifecycle.py --model opencode/deepseek-v4-pro --suites establish
python3 scripts/blackbox/mcp_lifecycle.py --compare results/A.json results/B.json
```

Environment overrides for every suite: `AOCI_REPO` (repository root),
`AOCI_BIN` (binary path), and for the lifecycle suite `AOCI_OPENCODE`
(OpenCode binary path).

## Reading results

- `PASS` / `FAIL` are binary contract judgments; any `FAIL` makes the process
  exit nonzero.
- `CHAR` records a characterization: the contract permits more than one
  outcome and the line documents which one this machine produced. It is not a
  failure.
- The lifecycle suite writes a JSON report per run under
  `scripts/blackbox/results/` (gitignored) plus per-run artifacts, and
  `--compare` aligns two reports — across models or across binary versions.
- Model-track judgments come from public end-state surfaces (`aoci verify`,
  `aoci check`, entry counts, local Ledger operation counts), not from parsing
  the agent transcript; agent session output is retained as an artifact.

## Scope and platform notes

- The frozen fixture masters under `fixtures/` are never written; every run
  copies them to a temporary directory, so "reset to a clean repository" is
  implicit. Do not edit the masters casually — scenario expectations (file
  counts, curation probes, schema drift sets) are frozen with them.
- `fixtures/repo-c` was produced once by `generate_repo_c.py` and frozen. The
  generator exists for deliberate rebuilds only: regenerating changes the fixture
  identity, and the `scale` suite asserts the frozen file count before it runs.
- Primary platform is Linux (including WSL2). macOS works for all suites; the
  model track enforces its timeout in-process, so GNU coreutils is not
  required. Windows is untested for the Python runners.
- Conformance and scenarios also serve as an executable compatibility check
  for alternative implementations: point `AOCI_BIN` at any binary that claims
  the public contracts in `spec/public/` and run them unchanged.
