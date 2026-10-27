# Black-box verification suites

Four standalone Python suites exercise a built `aoci` binary strictly from
outside the process: they speak the public stdio MCP protocol and the public
CLI, never import internal packages, and use only the Python standard library.
Two runners written in Go take the same outside stance. They ship with
the repository clone (binary Release archives do not include `scripts/`).

| Suite | Proves | Needs | Typical time |
| --- | --- | --- | --- |
| `mcp_conformance.py` | The MCP wire surface honors its contract: handshake, the nine-tool registry, input schemas, response shapes, error behavior, clean handling of malformed input, and a locale twin lane that repeats the locale-dependent checks on two throwaway fixtures initialized with `--locale en-US` and `--locale zh-CN` (human sentences follow the locale; keys, machine fields, error codes, and digests do not). Read-only on the host repository. | python3, git, a built binary | seconds |
| `mcp_scenarios.py` | Safety under hostile handling: cursor replay/tampering, write-lifecycle rejections, crash injection during Apply, racing writers, and the pre-write hook (`aoci hook pretool`) under the Claude Code stdin-JSON and Codex flag shapes: fail-open on bad input, Entry injection, and the strict stale block with each agent's exit code. Disposable fixture repositories; the host repository is only read. | same as above | minutes |
| `mcp_lifecycle.py` | Complete lifecycles on three frozen realistic projects: `repo-a` (a TypeScript service) and `repo-b` (a Python + MySQL service) run from `init` through incremental maintenance, database Evidence, drift, re-alignment, the governance walks (curation probes, post-scan exclusion, and a pending policy edit over a changed source), and the bring-up lanes (a git submodule boundary, `exclude_root_dirs` roles with `config set` normalization, `remove-entry --preview` and the real removal of an orphan, the `source manifest` and `capabilities` machine documents, and a zh-CN run compared with an en-US run of the same commands: no en-US sentence fragment in the human output, identical `--json` machine fields), while `repo-c` (a 453-file layered service) additionally exercises multi-batch authoring at the real machine batch limit, including a relation cycle that spans every batch. An optional model track drives a real AI agent through the small repositories. | above + Docker for the `database` suite; OpenCode + a model subscription for the model track | minutes; model track depends on the model |
| `mcp_upgrade.py` | The upgrade axis: a repository built and authored by a *previously released* binary stays governable by the binary under test — no identity moves, no Scope Change is demanded, no formal asset is rewritten. Every release in the CHANGELOG is downloaded, checksum-verified, and probed in ten repository shapes; the tenth turns the axis around, so each release also has to read a repository the binary under test wrote. `--self` runs the same shapes with the binary under test in both roles, before a tag exists. | above + network access on the first run (binaries are cached; `--self` needs none) | minutes |
| `longrun-refresh/` (Go; `make longrun-refresh`) | The long-running cognition refresh contract against a real stdio MCP server over two disposable Git repositories: the 29-versus-30 semantic threshold, compact `check_only`, Dirty delivery marked unreliable, a simulated Host compaction with a partial Attestation, three-reason merging, one injected `repair_required` correction, and final Verify/Check/Guide alignment. Writes a JSON report; see its own README. | go, git, a built binary | seconds |
| `legacy-migration/` (Go; `make legacy-migration`) | The one exit from the read-only Legacy layout: a repository the last Legacy-authoring release built and aligned migrates through onboarding and a host-dialog approval to aligned Volumes v1, every Code Entry carried byte for byte, by running only the commands each result names. | go, git, python3, a POSIX shell, a built binary, network once for the release binary (cached) | seconds |

## Running

Build or obtain a binary first, from the repository root:

```bash
make build          # or: export AOCI_BIN=/path/to/downloaded/aoci
```

Protocol conformance (58 checks, read-only): the host repository is only read;
the locale twin lane mints its two fixtures in a temp dir and removes them.

```bash
python3 scripts/blackbox/mcp_conformance.py
```

Fault-injection scenarios (80 scenarios, disposable fixtures in a temp dir).
Without arguments every group runs in the published order and the published
count is enforced; `--group` runs only the named letters (repeatable) and skips
that binding, and `--list` prints the letters with what each group runs:

```bash
python3 scripts/blackbox/mcp_scenarios.py
python3 scripts/blackbox/mcp_scenarios.py --group H --group O
python3 scripts/blackbox/mcp_scenarios.py --list
```

Upgrade axis (80 checks per released version, over ten repository shapes;
downloads and checksum-verifies each release once into a gitignored cache):

```bash
python3 scripts/blackbox/mcp_upgrade.py
python3 scripts/blackbox/mcp_upgrade.py --versions v0.1.0-rc6,v0.1.0-rc7
python3 scripts/blackbox/mcp_upgrade.py --recent 3      # the newest three releases
python3 scripts/blackbox/mcp_upgrade.py --self          # the binary under test in both roles, no download
make axis-self                                          # the same, over BUILD_BIN; part of make verify
```

The other three suites mint every fixture with the binary under test, so a
preimage that changed *between* versions is invisible to them by construction —
this suite exists for exactly that class. Its shapes are not
decoration: `init` is `.aoci/config.json` as the released `aoci init` wrote it,
which always carries an explicit `cognition_budget` block, while `nobudget`
removes that block before `scan`. Only the second shape resolves
`cognitionbudget.LegacyPolicy`, which is the frozen preimage every repository
created before that block existed still depends on. Verified against a
deliberately un-frozen binary: the `init` shape stays green, and only `nobudget`
reports `scope_change_required=true` from the upgrade alone. The third shape,
`spacedroot`, is the `init` shape under a path that holds a space: every release
up to v0.1.0-rc13 read a section header back only up to its first space, so the
index it wrote there mixes one full-spelled root with a truncated one. The last
check of every shape has the binary under test author a new file in a directory
whose name holds a space and then verifies a copy of the repository at another
path; a writer that does not continue the spelling the index already uses turns
it red for `spacedroot` alone. The fourth shape, `cutsegment`, puts the
repository under a directory whose name begins with `(`, where the truncated
root is an ancestor of the full one: a reader that recognises the old writer
only by a section outside the truncated root misreads every checkout there
while the origin stays aligned. `legacydirs` plants tracked root and nested
artifact names and pins what the released policy made of them, any-depth
exclusions before rc20 and the root list from rc20 on, so a new repository
default cannot silently change an existing repository's scope. The last two shapes put git-ignored directories
under the released index. In `ignoredpull` a user rule indexes `gen/**` while
`.gitignore` hides `gen/`, so the release authors an Entry for `gen/api.go`; the
binary under test must read it aligned with no orphan, and its own MCP Maintain
and update must then re-author a changed file beside it. In `ignoredtree` a
`node_modules/` of 300 files and an `out/` whose one file a user rule observes
are both ignored; the composite identity must not move and the observed file
must stay observed. The tenth shape, `downgrade`, turns the axis around: the
binary under test initializes and authors the fixture, and the released binary
runs the read-only surface over it (`verify`, `check`, `scope status`, `scope
explain` on a planted path, `status`), then an MCP Overview and one Maintain.
Every command must exit with an ordinary code and answer JSON where `--json`
was asked, the released binary must read the stamped active identity, budget,
layout and composite identity back unchanged, its Maintain must either reach
`aligned` or stop on a named machine state (`scope_change_required`, a
`cognition_*` refusal, or an unsupported layout) and never crash, no formal
byte may move, and the binary under test must afterwards still verify aligned
and grow. Which answer a release owes is a floor in the suite: a release from
v0.1.0-rc20 on shares every preimage the binary under test writes and must
read the fixture aligned, while every earlier release computes its desired
policy identity without `exclude_root_dirs` and must answer
`scope_change_required`, which is what `docs/upgrading.md` promises. A
candidate that adds a persisted preimage moves that floor, and its release
notes must say which binary every machine then needs. A run that cannot
fetch a release fails; pass `--allow-offline` to downgrade that to a skip. One
exemption is deliberate: the newest CHANGELOG section may precede its GitHub
release by the one commit the release cut takes, so a 404 for that version
alone is reported as `CHAR` and left out of the matrix — the gate that must
pass is the one that creates the release. A 404 for any older version, or any
other fetch error, still fails, and a matrix the exemption would leave empty
fails too.

`--self` is the pre-tag step. The moment a candidate is tagged it becomes an
"old version" for this suite, and a shape premise that was written for the
releases before it goes stale with no run in between: rc20 moved the artifact
names to a root list, and `legacydirs` turned red on the first nightly after
its tag because nothing had run the rc20 binary as a released version before
the tag existed. Under `--self` the binary under test plays both roles of
every shape, labelled with the newest `## v...` heading of `CHANGELOG.md` (or
`candidate` while the top section is Unreleased), and downloads nothing. `make
axis-self` runs it over `BUILD_BIN`; `make verify` runs it after the three
other suites, and the release rehearsal runs it over the packaged Linux
binary. It cannot replace the released matrix: a real older binary is the only
thing that proves what an older binary does.

`--recent N` runs the newest N releases only. CI layers the matrix by event:
a push to `main` runs `--recent 3`, so the releases a change can realistically
meet are proved on every push, and the nightly schedule and a manual dispatch
run the whole list, which grows with every release and is what keeps the oldest
preimages honest.

Legacy migration drill (a disposable repository in a temp dir). Since 0.2.0
the Legacy layout is read-only and migration is its one exit, so this runner
proves that exit end to end. The last release that authors Legacy indexes
(`LEGACY_RELEASE`, default v0.1.0-rc22, taken checksum-verified from the
upgrade axis's cache through `mcp_upgrade.py --fetch`) builds a small Legacy
repository in each Locale, en-US and zh-CN, the way its users did, since the
two Headers differ in every line, and proves each aligned with its own Verify
and Check. The binary under test must then route it to onboarding, refuse a write,
and carry it through the migration: the authoring batches, the mapping
template, Preview, Prepare, an approval the runner gives as the person in the
host dialog, and Apply. After the Guide's onboard command the runner spells no
command itself: it runs what each result names in `next_commands`, in a POSIX
shell from the repository root, so a step a host could not find in the output
fails here. Verify, Check, and the Guide must report the result aligned, and
the Code Volume must hold every Legacy business Entry byte for byte and not
the Root-owned self-Entry. The runner plays the host and its model; plans,
templates, Previews, approval records, and Apply decisions are the binary's,
and the result is judged from the published files as text. `--keep` retains
the repositories and `--locale` runs one Locale. Any release that authored
Legacy indexes can be the author; v0.1.0-rc8 through rc22 all migrate:

```bash
make legacy-migration BUILD_BIN=build/aoci-<label>
go run ./scripts/blackbox/legacy-migration --binary build/aoci-<label> \
  --legacy-binary "$(python3 scripts/blackbox/mcp_upgrade.py --fetch v0.1.0-rc22)" --keep
```

Lifecycle, deterministic track only (no AI involved; `database` pulls
`mysql:8.4` and needs a working Docker daemon — drop it via `--suites`):

```bash
python3 scripts/blackbox/mcp_lifecycle.py
python3 scripts/blackbox/mcp_lifecycle.py --suites bringup,incremental,governance
python3 scripts/blackbox/mcp_lifecycle.py --suites scale     # 453 objects, no Docker needed
python3 scripts/blackbox/mcp_lifecycle.py --suites database --mysql-image mysql:5.7
```

`--mysql-image mysql:5.7` runs the `database` suite against MySQL 5.7 with the
same assertions. The frozen `repo-b` schema carries two statements 5.7 cannot
parse, an `INVISIBLE` index and the `ALTER INDEX ... VISIBLE` that follows it
in v2; the harness maps them in memory to their 5.7 shape (no index in v1, the
index added in v2), so the fixture files stay frozen and the drift arithmetic
is the same on both releases. The report records the server version the
container reported next to the `snapshot` check.

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
  required. CI runs conformance and scenarios on Windows; `make build` writes
  `build/aoci.exe` there, so point `AOCI_BIN` at it, because the runners default
  to `build/aoci`.
- Conformance and scenarios also serve as an executable compatibility check
  for alternative implementations: point `AOCI_BIN` at any binary that claims
  the public contracts in `spec/public/` and run them unchanged.

## Run-by-name ladder

`ladder.json` is the registry of every test contract that someone must run by
name because no gate runs it for them: the release-time axis run with the
published binary, the real-repository track, the Docker-only database suite,
the nightly-only gates, and the model track. Each entry records its command,
cadence (`change`, `closure`, `release`, `nightly`, `manual`), preconditions,
and the last verification on record. `make verify` ends by printing the
overdue entries, and `make ladder` prints them on demand:

```bash
python3 scripts/blackbox/ladder.py --check            # what is overdue or never recorded
python3 scripts/blackbox/ladder.py --list             # the whole registry
python3 scripts/blackbox/ladder.py --mark realrepo --version v0.1.0-rc21 --evidence "RuoYi 26 rounds"
```

The registry is kept by hand, so a stale record means nobody wrote the run
down; that is the fact worth seeing. A new suite or harness is registered here
in the same change that adds it, and the AOCI index Entry of the registry is
how an agent finds the list at all.

## Real-repository track

`realrepo.py` clones a real third-party repository locally, initializes it,
authors template Entries through Maintain and `aoci_update_entry` until
aligned, touches a wave of files and re-aligns, runs one cognition
optimization batch, and probes the PreToolUse hook on an indexed file, with
every Maintain response measured against the transport budget. Template
Entries carry no semantics, so the track proves transport, batching, alignment
and timing on real trees, never cognition quality. It is a release-cadence
ladder entry, not a CI gate: it needs local clones and a few minutes per
repository. The release record compares RuoYi and excalidraw against the
previous release.

```bash
export AOCI_REALREPO_SOURCES=~/aoci-demo-cand        # directory holding the clones
python3 scripts/blackbox/realrepo.py RuoYi build/aoci --label=rc21 --ignored-tree=3000
python3 scripts/blackbox/realrepo.py /path/to/any/clone build/aoci --label=rc21
```

