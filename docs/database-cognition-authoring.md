# Database Cognition Authoring v1

Database Cognition authoring turns saved Database Evidence into governed,
model-authored table cognition without giving the program any
database-semantic authoring role. It
works only with an already declared and present Cognition Volumes v1 Database
Volume. A marker-only `aoci.database.txt` is a supported present-empty starting
point; an absent Volume is not created.

The normative contract is
[`aoci-database-cognition-authoring-v1.txt`](../spec/public/aoci-database-cognition-authoring-v1.txt).
The Evidence format remains
[`aoci-database-evidence-v1.txt`](../spec/public/aoci-database-evidence-v1.txt), and all
new Entries continue to use Object FRAS v2 and Database Table FRAS v1.

## Responsibility boundary

The host model reads one complete Evidence Bundle per target table, the old
Entry when present, and relevant migrations, repositories, services, APIs,
configuration, and tests. It independently writes the complete tag and F/R/A/S
line. F is one core proposition; R contains only strong relations; A contains
only stable access and contract surfaces; each S statement must be both
non-inferable from ordinary facts and capable of preventing a wrong change.

AOCI discovers target tables, returns complete Evidence, owns candidate
identities, validates the complete batch, computes impact, renders Diff, and
uses the existing CAS/AtomicWrite/Baseline/Ledger/Recovery pipeline. It never:

- derives F from a table name;
- copies every FK into R or every caller into A;
- infers S from `status`, `deleted_at`, `tenant_id`, or timestamps;
- emits a semantic draft for later polishing;
- truncates or repairs a candidate;
- reads business rows or reconnects during Maintain/Apply.

## Binding state

`.aoci/database-baseline.json` remains the independent Database Evidence
Baseline: it says which structural snapshot was explicitly accepted.
`.aoci/baseline.json` now has an optional, versioned
`database_cognition` extension: it binds each formal table Entry to
`source_id`, Evidence version, `table_evidence_sha256`, and the exact Entry SHA.
Keeping these facts in the existing cognition Baseline lets the existing Apply
and recovery transaction move Entry bytes and bindings together.

The offline status model is:

| State | Meaning |
| --- | --- |
| `cognition_current` | Entry, Entry SHA, and current Table Evidence SHA agree |
| `cognition_missing` | current Evidence has a table but the Volume has no Entry |
| `cognition_stale` | the bound Table Evidence identity differs from current Evidence |
| `cognition_unbaselined` | an Entry exists without its matching binding |
| `cognition_orphan` | reliable current Evidence no longer contains the Entry's table |
| `evidence_unavailable` | saved Evidence cannot be obtained reliably |
| `evidence_invalid` | saved Evidence is damaged or non-canonical |
| `source_disabled` | the configured source is disabled |

Snapshot, Verify, and Evidence Baseline acceptance never refresh cognition
bindings. A rename is reported as old orphan plus new missing; neither side is
changed automatically. No configured sources means no Database Cognition debt
and `network_accessed=false`.

## Normal AI Agent flow

Database access remains explicit and occurs only during the Database Evidence
source commands:

```bash
# Explicit network operation, normally performed before the authoring task.
aoci database snapshot --source primary --json

# Offline status: no database connection.
aoci database cognition status --json
```

The AI Agent then uses the existing MCP tools:

1. Call `aoci_maintain` with no scope. In Volumes mode it automatically selects
   Code, Database, both, or aligned. Explicit `scope=database` remains an
   advanced diagnostic. Maintain reads only accepted local saved Evidence and
   returns a deterministic candidate page, exact formal `authoring_meta`, the
   current Database dictionary and FRAS/relation rules, and one complete legal
   Database Entry example. No preliminary explicit scope call is required. It
   may write a local machine candidate receipt and Ledger audit record, but no
   formal cognition or Code Volume.
2. For every target, read Root/Meta, the complete returned Table Evidence,
   current Entry, and relevant repository evidence. Author one complete Entry.
3. Submit one `aoci_update_entry` call with the page's top-level `batch_id` and
   every target's `object_ref`, `candidate_id`, and complete `new_entry`.
4. If the page succeeds and work remains, call Maintain for the next
   deterministic page. Stop when the assessment is current.

No-argument Maintain and explicit `scope=all` run Code and offline Database
assessment together; they do not collapse to Database scope.

`candidate_id` and `batch_id` are non-secret machine identities. The receipt
internally binds the exact Database Volume preimage and each target Table
Evidence SHA, so the model does not copy multiple long hashes. Legacy and Code
`source_sha256` inputs remain compatible.

Saved Evidence must also match the current non-secret source selection:
engine, database name, namespaces, and include/exclude filters. Reusing a
`source_id` for another database or filter set makes the old Evidence
unavailable and invalidates its candidate. Credential environment names and
timeouts are deliberately not part of selection identity.

## Batching and atomicity

Targets sort by canonical `object_ref`. A page is bounded by object count and
the complete canonical Evidence-byte sum. Machine defaults and bounds live only
in `internal/machinecontract`; optional team keys
`database_cognition_batch_objects` and
`database_cognition_batch_evidence_bytes` may tune large projects, while local
configuration cannot override them. One oversized table is returned alone;
Evidence is never truncated and no token predictor is used.

Every target must appear exactly once. Candidate order is irrelevant, but a
missing, duplicate, additional, invalid, or stale item rejects the whole page.
One valid page produces one Database Volume postimage and moves every binding
in the same existing Baseline save. If a postimage exists but a later Baseline
effect fails, version 4 of the same existing Entries recovery receipt retains
the exact Volume preimages, Baseline preimage identity, and Evidence-binding
targets and can complete them even if the local
candidate draft receipt is lost. That fallback is accepted only for a proven
Database Volume postimage, never to authorize a new preimage write. An
identical completed retry is idempotent. Before Baseline advancement, a
policy-selected exact rollback uses the existing CAS only while all guards and
participants remain provable; otherwise the transaction resumes or hard-blocks.

The same update call may include multiple Code candidates and a complete
Database receipt batch. Code, Database, and the combined Baseline/Binding are
each written at most once. Only candidate-receipt mode may add a new Database
Entry. The existing-entry `source_sha256` compatibility path remains available
for updates to an existing Database object but cannot bypass Evidence binding
to create one.

Immediately before writing, AOCI rechecks target Evidence, the receipt, the
Database Volume preimage, source configuration, full target set, Meta
dictionary, FRAS limits, identities, and cross-Volume relations. Target schema
drift invalidates the candidate; a valid unrelated-table change does not.

## Present-empty and update behavior

A present-empty Database Volume can receive new sections and model-authored
table Entries. An existing Entry can be replaced, or can acquire a missing
Evidence binding without rewriting identical Entry bytes. Later Table Evidence
drift makes only the affected Entry stale. The model rewrites its complete
Entry and preserves an old high-entropy S constraint only when current Evidence
and code still support it.

Orphans stop for review and are never removed automatically. Missing or invalid
Evidence stops authoring rather than being interpreted as an empty database.
An absent Database Volume remains read-only during Maintain. It returns
`snapshot_or_repair_evidence` until every enabled source has accepted Evidence,
then `bootstrap_database_cognition`. In auto mode, explicit Evidence acceptance
uses the independent Database Cognition Bootstrap to add only the Database
descriptor, marker-only Volume, and Baseline binding. Code is not rewritten;
model-authored table FRAS continues through ordinary Database Maintain.
The public command and write boundary is defined by the
[`AOCI-CODE CLI Runtime Contract`](../spec/public/aoci-code-cli-runtime-v1.txt) and
the live capability manifest. Internal lifecycle derivation is not part of the
public contract set.

## Security and compatibility

Database Maintain, status, candidate validation, and Apply are offline. They do
not read a credential environment value, open a socket, query a table, or issue
DDL/DML. Results exclude DSNs, passwords, tokens, hosts, IPs, usernames, and
business-row sentinels. Evidence references are content-addressed and checked
against path escape and exact SHA-256.

Database candidate-receipt I/O diagnostics are stable machine codes and do not
echo absolute filesystem paths. The pre-existing MCP cognition receipt still uses
`runtime_repository_root` as repository identity.

The official AOCI repository remains Legacy monolithic and has no official
Database Source, Evidence runtime asset, or Database Volume. Legacy and
unconfigured projects retain their prior behavior. Code-only Volumes can add
Database Cognition without Legacy Migration. The MCP surface remains
nine tools, ordinary Overview still delivers the complete requested scope, and
`check_only=true` remains compact.

Supported:

- offline discovery from existing canonical Evidence;
- independent semantic-free Database Volume Bootstrap after accepted Evidence;
- model-authored table FRAS;
- double-bound candidate receipts;
- cognition Missing/Stale/Unbaselined/Orphan status;
- new and updated Entries in an existing Database Volume;
- deterministic multi-table pages;
- existing governed Apply and recovery.

Not supported:

- Database Volume creation by Maintain or Database Entry Apply;
- Meta or Code writes during Database Bootstrap;
- Legacy-to-Volumes migration;
- View, trigger, procedure, function, or event cognition;
- program-generated FRAS;
- automatic production database connection;
- any additional Database Bootstrap mode beyond the documented evidence-bound
  bootstrap.
