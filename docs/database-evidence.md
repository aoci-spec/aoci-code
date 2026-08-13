# Database Evidence Layer v1

The current release candidate provides a redacted access preflight and an
environment Credential Provider adapter. `database source add` can derive the environment reference from
`source_id`, and `database source access` checks readiness without connecting.
No credential value is saved; Vault, Kubernetes, and cloud secret providers
remain future adapters. The public derived-view boundary is in
[`aoci-system-cognition-runtime-v1.txt`](../spec/public/aoci-system-cognition-runtime-v1.txt).

The Database Evidence Layer provides explicit, read-only evidence for
PostgreSQL, MySQL, and the constrained openGauss profile described below. It
answers which base tables, columns, keys, constraints, indexes, and partition
facts are visible now, which deterministic fingerprints represent them, and
how those facts differ from an explicitly accepted evidence Baseline.

It does not author Database Cognition. Tags and F/R/A/S remain entirely
model-authored after the model reads the evidence. The Evidence Layer does not create
`aoci.database.txt`, change Root or Meta, or claim Database Cognition alignment.

The executable format authority is `internal/dbevidence`; the public contract
is [Database Evidence v1](../spec/public/aoci-database-evidence-v1.txt).

## Source configuration and credentials

Add a logical source declaration to team configuration:

```bash
aoci database source add \
  --source-id primary \
  --engine postgresql \
  --database-name app \
  --namespace public \
  --credential-env AOCI_DB_PRIMARY_DSN
```

For MySQL, `--namespace` must equal `--database-name` in v1. `--namespace`
defaults to `public` for PostgreSQL and openGauss and to the configured
database for MySQL.
The include/exclude flags accept Go-style glob patterns over the entire
identifier and may be repeated. A slash is an identifier character, not a path
separator:

```bash
--include-namespace 'tenant_*' --exclude-table '*_archive'
```

`source_id` is a stable project-local logical name, not a host, IP, username,
or credential. The team file stores only `credential_env`. Put the connection
string in that environment variable:

```bash
export AOCI_DB_PRIMARY_DSN='<user-owned connection string>'
```

The environment value is passed directly to the selected driver and is never
placed in configuration, Evidence, Snapshot, Baseline, Ledger, Research
Manifest, or returned diagnostics. Connection failures expose a stable error
code and source_id while suppressing driver endpoint details.

Database source declarations are team-owned. `config.local.json` cannot replace
them. A source must be enabled, and database network access happens only for an
explicit `source inspect`, `snapshot`, or `verify` command. With no source
configuration, existing AOCI commands do not open a database connection or
create database assets.

## User flow

```bash
# No network: show non-secret declarations.
aoci database source list --json

# Explicit network: test connection, read-only transaction, and visibility.
aoci database source inspect --source primary --json

# Explicit network: write reproducible runtime Evidence and Snapshot.
aoci database snapshot --source primary --json

# No network: compare the saved Snapshot with the evidence Baseline.
aoci database inventory --source primary --json

# Explicit network: collect fresh Evidence and report drift. Never accepts it.
aoci database verify --source primary --json

# Explicit formal action: bind and accept one exact saved Snapshot.
aoci database baseline accept --source primary \
  --snapshot-sha <source_snapshot_sha256> --json

# No network: export one model-facing table bundle to stdout.
aoci database evidence bundle --source primary \
  --object database://primary/public/users --json
```

Human output is compact. JSON outputs include source identity, visible table
count or drift counts, Snapshot SHA, stable error code when applicable, and
`business_data_read: false`.

## PostgreSQL collector

The PostgreSQL collector supports base and partitioned tables visible through
the configured schemas. It records:

- catalog spelling for quoted and unquoted identifiers;
- ordinal columns, `format_type` native types and information-schema type;
- nullability, default, catalog-owned serial sequence, identity, and
  generated-column facts;
- ordered primary, unique, and foreign keys plus update/delete actions;
- check definitions;
- ordered index expressions, uniqueness, method, included columns, and partial
  predicates;
- partition method/key, parent/bound, and sorted child object identities.

Queries use `information_schema` and the necessary `pg_catalog` functions and
tables. `information_schema`, `pg_catalog`, `pg_toast*`, and `pg_temp_*` are
always excluded as object namespaces.

## MySQL collector

The MySQL collector targets MySQL 8.0+ catalog fields and records:

- the exact catalog spelling and `lower_case_table_names` value;
- ordinal columns, `COLUMN_TYPE`, normalized data type, nullability, default,
  `auto_increment`, and generated-column facts;
- ordered primary, unique, and foreign keys plus update/delete actions;
- check clauses;
- index uniqueness, order, column or expression, prefix length, descending
  direction, index method, and visibility;
- partition method/expression and ordered partition definitions.

`information_schema`, `mysql`, `performance_schema`, and `sys` are always
excluded as object namespaces. MySQL v1 intentionally treats one configured
database as the source namespace.

## openGauss collector

The initial openGauss profile supports exactly openGauss 6.0.5 LTS in A/PG
compatibility mode. It uses the dedicated `opengauss` engine token and the
reviewed local patch over the official openGauss Go Connector v1.0.8 source;
`postgresql`, `mogdb`, and product-name aliases do not select this collector.

The collector supports ordinary, non-partitioned base tables visible through
the configured schemas. It records the v1 column, primary-key, unique,
foreign-key, check, and ordinary-index facts that the openGauss catalog can
represent without translation loss. It verifies the server identity, 6.0.5
profile, and A/PG compatibility mode before accepting PostgreSQL-style
identifier case semantics.

The openGauss collector uses its own fixed query inventory. It does not route
through the PostgreSQL collector or retry failed openGauss queries as
PostgreSQL. `PG_PARTITION` is used to detect catalog features outside the
initial profile. Partitioned or subpartitioned tables, local/global index
semantics, Dolphin/B/MySQL compatibility mode, MogDB/GaussDB aliases,
column-store, MOT, foreign or temporary tables, and views are not silently
reduced to ordinary table evidence. A selected visible table-like object that
requires one of those unsupported semantics fails closed with
`unsupported_catalog_feature` and does not advance Evidence or Baseline.
Routines and triggers are outside the v1 table-object domain; they are not
selected as table objects and never become or decorate canonical table facts.

`information_schema`, `pg_catalog`, `pg_toast*`, `pg_temp_*`, and openGauss
engine-owned administration, performance, snapshot, and service schemas are
always excluded. Include filters cannot re-admit a system schema.

The production opener uses the patched connector's deterministic strict parser.
It accepts only the reviewed connection parameters, requires host, port,
database, user, password, and `sslmode` in the externally supplied DSN, and
ignores ambient libpq-style environment, service, password-file, HOME, and
logger configuration. TCP outside a numeric loopback address accepts only
`sslmode=verify-full`, uses TLS 1.2 or later, and requires `sslrootcert` to be an
absolute path when supplied; `allow`, `prefer`, `require`, and `verify-ca` are
rejected instead of creating a downgrade or identity-verification gap.
`sslmode=disable` is limited to a Unix socket or numeric loopback address for an
explicit local/test boundary.

## Read-only and data boundary

Each operation uses a single connection and a repeatable-read transaction with
the driver's read-only transaction flag. Connect and whole-transaction timeouts
are explicit and context cancellation is propagated. Query text is centralized
and tested as a fixed inventory.

The product never issues a query against a user table. It never selects or
counts business rows, samples values, reads table size/statistics, or executes
INSERT, UPDATE, DELETE, TRUNCATE, ALTER, CREATE, DROP, or migrations. Integration
fixtures insert conspicuous business sentinels using the test administrator;
collector output is scanned to prove those values and runtime-generated reader
credentials are absent.

A database account should have the minimum catalog visibility needed for the
selected namespaces and read-only access. Driver, TLS, permission, connection,
and timeout errors are mapped to redacted codes. Raw driver text is not returned
or appended to Ledger.

## Evidence, fingerprints, and drift

Runtime Evidence is stored under `.aoci/database/evidence/` and is ignored by
Git. `source.json` contains non-secret source/audit facts. Each table file is
content addressed by `table_evidence_sha256`; `snapshot.json` lists the sorted
object identities and hashes and carries `source_snapshot_sha256`.

Table fingerprints exclude engine patch version, endpoint, host, IP, username,
credential, capture time, row facts, size, statistics, and comments. A source
fingerprint also records engine, database, namespace and case semantics, empty
versus non-empty state, sorted include/exclude selection rules, and the complete
sorted table identity/hash set.

`.aoci/database-baseline.json` is a separate formal evidence identity asset. It
does not modify or reuse `.aoci/baseline.json`. The implementation reuses AOCI's
existing repository write lock and atomic writer; it does not introduce another
CAS, Apply, Ledger, or Recovery state machine.

Drift reports:

- `unchanged`: current and accepted table hashes match;
- `new`: the current table has no accepted record;
- `changed`: hashes differ, with changed component names;
- `removed`: an accepted identity is absent now;
- `source_unavailable`: a fresh source cannot be read reliably;
- `evidence_invalid`: runtime Evidence is incomplete or non-canonical.

The report also carries `source_identity_changed` for engine, database,
namespace, evidence-version, or identifier-case semantics that differ from the
accepted source. It remains drift even when all surviving table hashes match.

Rename similarity is never inferred; a rename is `removed` plus `new` until a
model and maintainer decide what cognition work is appropriate.

## Evidence Bundle and Database Cognition authoring

The bundle contains the complete current Table Evidence and its SHA, optional
migration/code evidence references, and the old Database Entry if it exists.
It contains no proposed tag or F/R/A/S field and does not copy foreign keys into
a relationship candidate. Its terminal state is “evidence ready; waiting for
the model to author table-level FRAS.”

Database Cognition authoring uses the existing `aoci_maintain` and
`aoci_update_entry` tools to author and govern table cognition in an already
present Database Volume. A machine
candidate receipt binds the Database Cognition Volume preimage and each target
table Evidence SHA separately. The AI Agent submits `batch_id` and `candidate_id`
rather than copying both hashes into `source_sha256`. See
[`database-cognition-authoring.md`](database-cognition-authoring.md).

## Dependencies and platform evidence

The PostgreSQL driver is `github.com/jackc/pgx/v5` (MIT). The MySQL driver is
`github.com/go-sql-driver/mysql` (MPL-2.0). The openGauss driver is
`gitcode.com/opengauss/openGauss-connector-go-pq` (MIT), required at the
reviewed upstream v1.0.8 identity and replaced by the complete patched module
under `third_party/`. Its canonical patch, origin/sum/license/tree provenance,
and replay gate are versioned beside that module; AOCI uses only its
`opengauss` database/sql registration. All three driver paths are pure Go in this build and
`CGO_ENABLED=0` remains supported. The dependency gate locks the direct modules
and keeps `internal/dbevidence` outside the AI orchestration closure.

Default tests cover offline canonicalization through 10,000 tables, randomized
catalog ordering, source validation, redaction, fixed catalog queries, atomic
runtime storage, explicit Baseline acceptance, drift, Bundle boundaries,
Locale parity, and CLI JSON.

## Real PostgreSQL, MySQL, and openGauss acceptance

Separate Linux CI jobs run the integration contract against disposable
PostgreSQL 18, MySQL 8.4, and openGauss 6.0.5 LTS instances. The PostgreSQL job
starts an isolated container after masking its runtime-randomized administrator
password; the MySQL service uses an isolated test-only instance. The openGauss
job downloads the project's official 6.0.5 x86_64 Docker tar, verifies the
publisher-provided SHA-256 pinned in the workflow, and only then loads the
`opengauss:6.0.5` image. It does not use a floating registry tag or present the
tar checksum as a registry OCI digest. Test administrators prepare the fixture
and sentinel rows, while product collection uses a distinct runtime-randomized
read-only account. No source is persistent or connected to production.

The public openGauss support claim is release-gated on that named real-engine
test being present and passing with a reviewed canonical Golden. The workflow
checks the test name before execution, so a missing test cannot become a green
no-op through Go's `-run` filtering.

The PostgreSQL fixture covers multiple schemas, same-name tables across
schemas, quoted identifiers, composite keys and foreign keys, unique and check
constraints, JSONB, serial, identity and generated columns, ordinary,
composite, expression, included and partial indexes, partitions, system-schema
exclusion, partial visibility, catalog permission denial, failure cases, and a
connection terminated during collection. The MySQL fixture records the real
lower_case_table_names fact and covers quoted and reserved identifiers,
composite keys and foreign keys, unique and check constraints, JSON,
auto_increment and generated columns, ordinary, composite, prefix, descending
and invisible indexes, partitions, system-database exclusion, partial
visibility, failure cases, and a killed collection connection.

The openGauss fixture proves the exact 6.0.5 A/PG profile, quoted identifiers,
multiple schemas and same-name tables, ordinary v1 keys, constraints and
indexes, system-schema exclusion, partial visibility, redacted failure paths,
and interruption handling. The server is configured with an ephemeral CI CA
and loopback certificate, and the collector connects with `verify-full`; the
fixture also rejects wrong trust/identity material without TLS downgrade. It
also presents unsupported partition/catalog
features and requires fail-closed behavior without a partial Evidence or
Baseline advance. This test does not imply support for another openGauss
version, compatibility mode, edition-specific storage engine, MogDB, or
GaussDB.

All three jobs require byte-identical repeated collection, explicit Baseline
acceptance followed by unchanged verification, exactly one changed table after
test-admin DDL, a stable unrelated table fingerprint, new and removed states,
and failure paths that preserve the last valid Evidence and Baseline. The
reviewed canonical Goldens are
`internal/dbevidence/testdata/real/postgresql_accounts.json` with SHA-256
`d4a04d14f0cca18478e33a86c2988f862457ace07c3b6f865e888ddf4f362c04`
and `internal/dbevidence/testdata/real/mysql_accounts.json` with SHA-256
`79f1de81b949fd5de0e3d8a53042cbfb28aeb014a8234ced266ab694453c346a`;
the openGauss A and PG fixtures are
`internal/dbevidence/testdata/real/opengauss_a_accounts.json` with SHA-256
`563e92b64b3144d03455026ca03b9aa0f7ecced003809da4b61d7d7ab148fca6`
and `internal/dbevidence/testdata/real/opengauss_pg_accounts.json` with SHA-256
`4a215f5154e9140ac36167a4aaba565746c84f46fdf47f1c97ceeedff8fda81f`.
These fixtures are derived acceptance evidence, not a duplicate
canonicalization authority. Native builds remain separate platform evidence;
cross-compilation does not claim live database coverage on Windows or macOS. A
native Windows job runs the offline canonical Evidence and Database CLI JSON
tests, while CI also builds Windows amd64 and Darwin amd64/arm64 CGO-free
binaries.
