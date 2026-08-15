# Cognition Volumes v1

The runtime exposes derived Lineage, database-to-code Impact, Evolution, and a narrow
relation projection. None is a Volume or fact source, and none has a write or
recovery lifecycle. Scope Change now preserves and guards existing formal
Volume fingerprints when it advances its owned business-source Baseline
projection. See
[`aoci-system-cognition-runtime-v1.txt`](../spec/public/aoci-system-cognition-runtime-v1.txt).

Cognition Volumes v1 is additive. Fresh initialization now creates a semantic-
free Code-only Volumes repository. Existing Legacy monolithic repositories
remain supported and are not migrated automatically. Reads cover Root, Meta,
Code, and model-authored Database table FRAS. The current contract lets the model submit deterministic
multi-object Code, Database, or mixed Code+Database batches within an already
declared layout, using current source or accepted saved Evidence and the same
existing governance transaction. Existing single-object inputs remain
compatible. The CLI exposes read-only Bootstrap/Migration planning,
new-repository approval-bound Root-last Bootstrap Apply, and a
separate, Snapshot- and Mapping-bound Legacy Migration with exact pending
Rollback and a strictly eligible completed Reversal. Neither path edits an
existing normally used layout; general Root/Meta editing, Volume deletion, and
multi-Volume lifecycle updates remain unavailable.

The narrow Database Cognition Bootstrap is a separate lifecycle exception: an
aligned Code-only layout with explicitly accepted Schema Evidence may add only
the Database descriptor, marker-only Database Volume, and Baseline fingerprints.
When the active Baseline already contains the Root, Bootstrap requires that
binding to match the exact Root preimage and advances it to the descriptor-bearing
Root postimage in the same transaction without changing its Managed Scope role.
An unmanaged Root is not enrolled by Bootstrap. A later Scope Change may reconcile
the one canonical historical Root/Baseline state produced by older Bootstrap
versions, but arbitrary Root drift remains a stopped conflict.
It does not provide general Root/Meta editing or Volume Apply.

## Lifecycle command routing

Fresh and existing Volumes repositories use the current Guide and ordinary
no-argument `aoci_maintain`. The model authors the complete current
machine-issued batch and submits it through `aoci_update_entry`; a successful
non-final batch returns to Maintain, and the final batch closes with Verify,
Aggregate Check, and Guide.

`status --deep`, `index score`, and `index agent plan` remain Legacy-only. They
must not be used as fallback maintenance commands for a Volumes repository.
Existing Legacy repositories retain those commands and their established
contracts; this compatibility does not make Legacy the Fresh default.

The four responsibilities are deliberately singular:

| Asset | Sole authority | Excludes |
| --- | --- | --- |
| `aoci.txt` | Repository identity, project overview, cross-domain boundaries, explicit layout and dependencies | Business Entries, dictionaries, live hashes/counts/status |
| `aoci.meta.txt` | Shared object/FRAS protocol, S discipline, code/database dictionaries and calibration | Business Entries and execution state machines |
| `aoci.code.txt` | Code sections and model-authored file Entries | Project overview and copied Meta rules |
| `aoci.database.txt` | Database namespace sections and one model-authored Entry per table | Schema/column/constraint sub-Entries and copied Meta rules |

## Historical Code roots and clones

An absolute path in an `aoci.code.txt` directory section header is the
historical structural coordinate captured when that section family was
created. It is not the active repository location, a path used to read source,
or part of a Code object's semantic identity. The runtime repository root is
resolved for each invocation; an explicit `--repo` binds it directly and takes
precedence over repository discovery.

On a clone or move, AOCI maps the consistent historical section root to the
current runtime root and resolves each object to a repository-relative path.
The canonical Code identity remains `code:<repository-relative-path>`, so the
same Entry order and object identities remain valid without rewriting the
index. Ordinary reads preserve the historical section text. New sections in an
established index reuse its safely resolved historical root rather than
injecting the current clone path; an ambiguous or inconsistent mapping fails
closed.

This section-header rule is separate from the Root Manifest's `#Volume path=`
field. A declared Volume asset path such as `aoci.code.txt` must be normalized
and repository-relative. The absolute historical coordinate inside that Code
Volume is neither a Volume asset path nor the runtime root.

Historical roots are committed text and can disclose creation context even
though they are not runtime dependencies. Before publishing a repository,
review them for hostnames, user names, secrets, and private directory topology.
Do not hand-edit `aoci.txt`, `aoci.code.txt`, or the Baseline to change that
context; use only a formally supported cognition lifecycle when a rewrite is
actually required.

Code and Database are optional. Root+Meta is valid. An undeclared Volume is
`absent`; a declared valid file is `present`; a declared missing or damaged
file is `invalid`. An undeclared fixed-name file is only an unmanaged candidate
and is not read. Empty-present and absent are distinct identity states.

The runtime makes those four authorities an explicit, mutually exclusive
ownership classification shared by Migration, Bootstrap, Verify, Check, and
Guide. `aoci.txt` is always Root-owned, so it must never also appear as a Code
Entry. A conflict is reported with its cause, expected and actual owner,
affected path, and safe repair action.

## FRAS density calibration

The starting Legacy index contained 875 formal code Entries (Header examples
excluded). Unicode-character and comma-item distributions were:

| Measure | p50 | p75 | p90 | p95 | p99 | max |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| F characters | 78 | 91 | 109 | 118 | 130 | 142 |
| R characters | 138 | 176 | 214 | 243 | 315 | 434 |
| R items | 4 | 5 | 6 | 7 | 8 | 12 |
| A characters | 34 | 50 | 111 | 157 | 358 | 1366 |
| A items | 1 | 1 | 1 | 1 | 2 | 9 |
| S characters | 168 | 220 | 301 | 336 | 421 | 599 |

The Volumes v1 candidate sets F at 160 Unicode characters, R at 360 characters
and 8 items, and A at 400 characters and 6 items. Those thresholds sit above
the existing F maximum and near/above the p99 code distributions while
rejecting dependency dumps and the extreme A outlier pattern. Database
fixtures cover lookup, ordinary business, 20+ and 50+ column evidence,
multi-foreign-key, transaction-core, append-only audit, and pure junction
tables. Their manually authored Entries remain within the same limits because
physical breadth does not increase cognition allowance.

Across the eight table fixtures, F is 38-75 characters, R is 1-50 characters
with 0-4 items, A is 11-54 characters with 1-4 items, and S is 1-130
characters. The 24-column and 56-column evidence tables therefore need no
larger allowance than the small lookup or junction tables.

If the new limits were hypothetically applied to the 875 Legacy Entries, 15
distinct Entries would receive a density finding: 0 for F characters, 4 for R
characters, 8 for R item count, 6 for A characters, and 1 for A item count
(categories overlap). They are deliberately not applied: Legacy v1 remains
readable and writable under its existing contract without new rejection.

The numbers above are a calibration record, not a second executable authority.
`internal/machinecontract.ObjectFRASV2Limits` owns the values consumed by the
validator. Legacy v1 is not checked against the new F/R/A limits. Findings ask
the model to regenerate the complete Entry and preserve the original evidence;
the program performs no truncation, semantic compression, or generation.

## Overview and identities

`aoci_overview` retains the nine-tool MCP surface and accepts `all`, `project`,
`meta`, `code`, or `database`. The explicit `meta` scope contains Root and Meta
without object Volumes. Root and Meta form every Volumes scope dependency
closure. Scope identity changes only when that closure changes; composite
identity always describes the whole layout. Both use typed, length-framed
encoding over raw-byte SHA-256 values, fixed order, descriptors, format
versions, and absent/present state.

Receipt v2 reports the requested/effective scope, Root/Meta hashes, delivered
Volume facts, scope availability/state, object counts, scope/composite
identities, reliability flags, and refresh facts. `delivered_volumes` is empty
when no Volume bytes were delivered. A partial, token-fallback, or absent
delivery is never marked reliable. Every ordinary explicit call returns the
complete selected dependency closure when a coherent snapshot is available;
only `check_only=true` returns compact checkpoint facts. Dirty or Stale formal
cognition is delivered with unreliable flags. Pending recovery, damaged
participants, and concurrent formal-asset changes fail closed without a mixed
body. Legacy keeps its no-argument call shape and Receipt v1.

`aoci_header` returns Meta in Volume mode. `aoci_search` filters object Volumes
by scope. `aoci_get_entries` keeps Legacy/Code paths and adds Volume object
references, including `database://source/namespace/table`.

The Meta created by Volume-first `init` is the complete formal generic
authoring authority (scheme A), not a structural placeholder that needs a
project-specific Root/Meta authoring phase. Here “ready” means its protocol,
compact Tag grammar, A/B/C/optional-D/E dictionaries, S quota authority,
canonical relation rules, and a legal Code Entry example are complete and
machine-parseable. Project semantics are still absent until the model authors
Code objects; Root may remain the structural repository entry.

For a new Volume-first `init`, the starter Meta supplies fixed, general-purpose
expanded A and B vocabularies for both Code and Database, every C importance
digit from 1 through 9, no D dictionary, and the unchanged E L/M/S/T scale. D
remains available in the compact grammar when a repository's formal Meta
declares it. In the starter B vocabulary, `G` means genuinely cross-domain;
`Z` in starter A or B means the evidence is understood but no named category
fits. Neither unknown nor insufficient evidence may be hidden as `Z`, and a
classification gap is not an S constraint. These defaults affect only new
initializations. Existing repositories retain their formal Meta and Entries
without automatic migration or retagging.

New Tags have one canonical physical form: compact `A+B+C+[D]+E`; `EG7T` is a
legal example under the starter Code dictionary. Dotted Tags remain Legacy read
compatibility only. `ParseTags`, the scoped Meta dictionary parser, normal Entry
validation, loaded object validation, candidate validation, and projected
Volume validation share this decision. A valid Meta plus an invalid candidate
returns a complete candidate `RepairFinding`. An unavailable or conflicting
Meta returns an asset-level `stopped` result for `aoci.meta.txt` and never a
zero-index candidate Finding.

For every affected authoring domain, Guide and ordinary no-argument Maintain
use one shared assembler. They return the exact formal Meta bytes also returned
by `aoci_header` plus compact Tag, FRAS, S-quota, canonical-R rules, and a
complete parser/validator-proven Entry example. `authoring_meta` is never
modified or supplemented. Consequently an older valid formal Meta remains
usable with a newer runtime without a Meta rewrite. Code-only returns Code;
Database-only returns Database; mixed authoring returns Code then Database.
`R` uses comma-separated managed identities such as `code:path/to/file.go` or
`database://source/namespace/table`, uses `-` for no strong relation, and does
not accept prose or mechanically copied import lists. A repeated unchanged Meta
failure routes to `repair_meta_tag_dictionary`, not generic replan or candidate
resubmission.

`ParseTags` and formal object loading retain dotted Tag and bare-R read
compatibility. New Volume Entries, and updates that actually change the
corresponding field, require compact Tags and exact canonical R identities.
Rejected dotted or bare authoring is a candidate `repair_required` result with
complete Repair Finding identity and zero formal writes; AOCI never silently
turns `main.go` into `code:main.go`.

For each complete current machine-issued candidate set, AOCI runs the deterministic Impact Resolver
first. Only explicit candidates enter Write: `[code]`, `[database]`, or
`[code,database]`. Canonical forward/reverse relations may add objects to
Review and their Volumes to Guard, but never synthesize another candidate. Root
and Meta always guard a Volume update. A relation-crossing single-Volume update
also guards the related object Volume. Relations that name a missing, unmanaged,
later-batch, or ambiguous target simply contribute no edge: they are accepted
and persisted exactly as written, produce no Finding, and never trigger a replan.
The machine never compares one Entry's relations against another Entry — that
graph is what the model builds by reading the Whole-Index. All Code and Database
candidates are applied in memory before Code, Database, and the combined
Baseline/Binding are each written at most once. The model-authored R is never
dropped or rewritten to fit transport.

Machine maintenance watches source bytes, so an Entry whose file did not change
can still go semantically stale when the system around it moves — a removed
mechanism its S still cites, a renamed contract its F still describes. After a
system-level semantic change lands, run one explicit `cognition_optimization`
pass (or read the Whole-Index and update the affected Entries directly) so that
class of drift gets a model review instead of rotting silently.

The projected CognitionSet must pass the same loader, FRAS-v2, Volume boundary,
Meta dictionary, identity, and single-Entry relation form checks before the existing Diff/P-23,
write lock, CAS, AtomicWrite, Baseline, Ledger, governance receipt, and Recovery
pipeline is entered. Target files require their exact planned preimages. A
second-Volume failure never reports success: the existing Entries recovery
receipt records typed preimage/postimage transitions and the same complete
candidate safely rolls the batch forward. Successful recovery is idempotent.
The same receipt retains bounded exact Volume preimages and the Baseline
preimage identity. Before Baseline advancement, a policy-selected rollback
restores only proven transaction postimages in reverse order through the same
CAS and archives the receipt; changed guards, Evidence, Baseline, or
third-party bytes hard-block.

The AI Agent supplies only complete model-authored Entries and machine receipt
identities. Code uses the source-file SHA-256. The compatible direct Database
path still uses the current raw Database Volume SHA-256 from Receipt v2. The
Database Cognition candidate-receipt path uses a machine-owned `batch_id` and
per-table `candidate_id` to bind the raw Volume preimage and target
`table_evidence_sha256` separately, without overloading `source_sha256` or
asking the model to copy both hashes.
The AI Agent does not supply the Change Envelope, Write Set, Guard Set, write
order, or recovery state. Normal results identify the affected Volume or
Volumes while internal consistency details remain hidden. The Legacy
governance path is unchanged and no parallel system exists.

## Daily governance

Verify, Check, Guide, and no-argument Maintain consume one private deterministic
facts assessment. Root+Meta, Code-only, Database-only, and Code+Database are
all complete legal layouts; an undeclared domain is `not_applicable` and adds
no debt. Verify distinguishes structural validity from governance alignment.
Check is successful for aligned Volumes instead of returning a read-only
placeholder. Guide returns one of `aligned`, `authoring_required`,
`evidence_required`, or `blocked`.

No-argument Maintain selects the affected domain automatically and emits one
current machine batch of exact model authoring targets:
by default 20 exact model authoring targets, and never more than
200 exact model authoring targets (the wire ceiling). The team batch size
`code_cognition_batch_entries` moves it (`aoci config set
code_cognition_batch_entries N`). The batch comes with Review/Write/Guard
closure and the affected-domain authoring contract. The response separates the
logical plan from the complete current machine batch with `total_targets`,
`max_entries`, `included`, `remaining`, batch and Composite/Scope identities,
and a continuation action. The limit is per request and atomic Apply, never per
Whole-Index or Managed Scope. After a successful non-final batch, call Maintain
again against the new preimage; Guide cannot report complete while debt remains.

The batch is sized so the model authors it inline as the arguments of one
`aoci_update_entry` call: no intermediate files, helper scripts, or shell
staging are needed, and a lost context is recovered by calling Maintain again,
which reproduces the same batch and candidate identities from the unchanged
repository. The Maintain response itself stays inside ordinary Host tool-result
windows regardless of repository size: candidates, plans, and receipts are
always complete, while per-item governance enumerations (`governance.findings`,
`governance.code_drift.*`, `sets.review`) keep a leading sample of 20 and
report complete counts under `governance.list_truncation` and
`sets.review_total`; `aoci verify --json` and `aoci check --json` still list
every item. Raise the batch only when both the Host window and the model can
carry it — a 1,400-file repository at 200 per batch answered its first Maintain
with roughly 330 KB, which no Host displays inline.
Database candidates include accepted Evidence identities
and a complete legal Database example without requiring a preliminary explicit
scope call. Maintain creates no semantic candidate and performs no formal
write. Orphans appear only as explicit remove candidates;
`aoci_remove_entry` re-proves the orphan and relation/guard state before using
the existing transaction authorities. Database assessment remains offline and
requires accepted saved Evidence.

The current repair path closes an ownership-conflicting Entry,
which is one of those governed orphans. Ordinary
orphan removal still rejects a still-valid relation. The only exception is an
exact machine ownership-conflict finding whose expected and actual owners
differ and whose expected formal owner exists, with valid structure and no
Recovery or third-party conflict. Repair removes only the incorrect Volume
Entry through the existing transaction, guards every formal owner, and reports
that Root ownership was preserved and source and Database bytes were not
changed.

## Explicitly not implemented

- automatic or unapproved Legacy-to-Volume migration;
- general rollback after migrated Volumes have received a cognition write;
- Root or Meta updates after Bootstrap, Volume deletion, or identity changes;
- a parallel multi-Volume transaction or separate recovery system;
- Database Volume declaration or creation by Maintain or Entry Apply;
- automatic database FRAS generation.

PostgreSQL/MySQL/openGauss connections, Canonical Schema Evidence, an
independent evidence Baseline, and drift detection remain in the explicit CLI
Database Evidence Layer. The initial openGauss profile is limited to 6.0.5 LTS
in A/PG compatibility mode and ordinary non-partitioned base tables; unsupported
catalog features fail closed. Database Cognition authoring consumes only saved
local Evidence and never connects during Maintain/Apply. See
[`database-evidence.md`](database-evidence.md) and
[`database-cognition-authoring.md`](database-cognition-authoring.md).

The public consistency boundary is defined by the runtime contracts in
[`aoci-cognition-volumes-v1.txt`](../spec/public/aoci-cognition-volumes-v1.txt),
[`aoci-object-fras-v2.txt`](../spec/public/aoci-object-fras-v2.txt), and
[`aoci-database-table-fras-v1.txt`](../spec/public/aoci-database-table-fras-v1.txt),
plus [`aoci-database-evidence-v1.txt`](../spec/public/aoci-database-evidence-v1.txt)
and [`aoci-database-cognition-authoring-v1.txt`](../spec/public/aoci-database-cognition-authoring-v1.txt).
The public command, exit, and write classes are defined by the
[`AOCI-CODE CLI Runtime Contract`](../spec/public/aoci-code-cli-runtime-v1.txt).
Planner derivation and internal lifecycle design are outside the public
contract set.
