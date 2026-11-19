# MongoDB collection Evidence profile (proposed)

Status: a proposal from issue #135, written up by dayebishouji (#166), pending
implementation and real-engine acceptance. It records the collection profile
for the implementation steps that follow. The implemented source declaration
is documented in Database Evidence v1 under `spec/public`; this collection
profile remains a proposal pending the collector and real-engine acceptance.
The current binary accepts `mongodb` declarations and reports
`engine_not_implemented` for access and collection.

## 1. Authority and implementation boundary

The accepted scope comes from the maintainer's design in issue #135. The
executable types, validators, canonical encoders, and hash rules must land
together before this profile can be advertised as supported. Existing SQL
Evidence, Snapshot, Baseline, Bundle, and Database Cognition contracts remain
the authority for their implemented table profiles.

Collections need their own evidence kind. They must not be encoded as base
tables with invented columns or empty SQL constraints. The implementing change
must declare the persisted format versions and older-reader rejection rules
before writing collection artifacts. This proposal allocates no JSON fields,
machine tokens, hash domains, or MCP tools.

## 2. Explicit source selection and access

A MongoDB source declares source_id, engine=mongodb, an explicit database,
credential_env, selection rules, timeouts, and enabled state. The connection
URI exists only in the named environment variable. Source add and list never
connect, and team configuration never stores a URI or credential value.

The access implementation must support mongodb:// and mongodb+srv:// and
reject options that disable certificate or host-identity validation. Timeout,
cancellation, credential-redaction, and explicit Baseline acceptance
requirements apply as they do to existing sources. The implemented declaration
binds one namespace equal to the configured database. Namespace filters select
that database, and table filters represent future collection selection.

The runtime account needs listCollections and listIndexes for the selected
database. Documentation must provide a custom catalog-only role rather than
require the built-in read role, which also permits business-document reads.
The collector must not create users, grant privileges, or modify a source.

## 3. Object identity and kinds

Each selected collection or view has one identity of the form

  database://<source_id>/<percent-encoded-database>/<percent-encoded-name>

The source_id follows the existing logical-token restriction. Database and
collection spelling is preserved, with the existing RFC 3986 path-segment
escaping and upper-case hexadecimal escapes. An embedded slash is encoded.
An identity does not contain a connection address or a collection UUID.

The logical object is a collection, with catalog facts distinguishing ordinary
collections, capped collections, time-series collections, and views. An index,
validator property, or time-series backing bucket is not a separate cognition
object. Engine-owned collections must not enter the inventory through an
include rule. Selection and system-object boundaries need real-engine tests.

## 4. Catalog-only collection

The metadata inventory uses listCollections and listIndexes. It exhausts
their cursors and checks every command error. Authentication, connection
negotiation, and cursor continuation or cleanup are transport operations, not
permission to run additional business queries. A view's definition comes from
the catalog; the collector must not execute its pipeline or request indexes
as if the view were an ordinary collection.

Business-document reads, sampling, row or document counts, schema inference,
DDL/DML, change streams, $listCatalog, and sharding metadata are outside the
initial profile. A collection without a validator has no server-declared
field inventory. The collector must preserve that absence rather than derive
fields from documents or pretend that an empty list proves there are none.

No partial catalog or authorization-filtered name-only listing may be reported
as a complete snapshot. A selected object that cannot be represented by the
supported profile stops collection with a diagnostic. Mongo-compatible
services, including DocumentDB, Cosmos DB, and FerretDB, require separate
acceptance and must be rejected rather than treated as tested MongoDB servers.

## 5. Evidence content and canonicalization

One collection's evidence contains its identity and the following facts.

- Kind and behavioral options, including capped settings, TTL, collation,
  clustered-index settings, and time-series fields where applicable.
- The declared validator, including $jsonSchema when present, with validation
  level and action. Validator expressions retain their meaning and BSON types.
- Index names, ordered keys and direction or type, including 2dsphere, text,
  hashed, and wildcard, with their semantic options. These include unique,
  sparse, partial filters, TTL, collation, hidden state, and type-specific
  settings needed to distinguish the supported indexes.
- For a view, viewOn and its ordered pipeline, recorded without execution.

Canonical object records sort by identity, and index records sort by name.
Compound index keys and pipeline stages retain order. Canonicalization must
preserve BSON scalar types and order-sensitive embedded documents, including
inside validators and expressions. It must not recursively sort arbitrary
document keys, rewrite expressions, or serialize BSON values through an
untyped JSON map. The implementation must pin the exact encoding and golden
bytes before it defines fingerprints.

Connection information, credentials, capture times, server patch versions,
business documents, counts, sizes, and statistics do not enter structural
fingerprints. UUIDs serve as collection-consistency guards, not stable object
identities or semantic content. Their persistence and hash treatment must be
specified with the wire format so a rename or recreation is not inferred from
UUID similarity.

## 6. Consistency, drift, and authoring

MongoDB catalog collection does not claim the SQL engines' repeatable-read
transaction guarantee. The collector reads the catalog twice and fails closed
if the listing or collection UUIDs change between passes. The implementing
change must also bind the validator, options, view, and index observations
across those passes so an observed metadata change cannot produce a mixed
successful snapshot. Matching observations are a stability check, not proof
that no concurrent change or change-and-revert occurred.

A failed or interrupted capture must not publish partial successful Evidence
or advance an accepted Baseline. The implemented Snapshot, explicit Baseline
acceptance, drift, and model-authored cognition lifecycle must support the
collection kind together. Component drift distinguishes validator, indexes,
options, and view definition. New and removed identities stay separate, with
no inferred rename or automatic Entry update.

Evidence supplies deterministic catalog facts. A host model reads those facts
and relevant code to author the collection's tag and F/R/A/S. Mongoose models
and repositories stay Code objects, and collection-to-code R relationships
are model-authored. A collector never creates a semantic draft or infers those
relationships. The evidence and bundle boundary retains business_data_read=false.

## 7. Independently reviewable delivery and acceptance

After this contract step, source declaration, collection implementation, and
collection drift/authoring integration are separate reviewable steps. Source
declaration alone must not advertise a working collector. The collection step
requires official MongoDB 7.0 and 8.0 instances in CI and review of the official
Go driver's release-module graph, licenses, and six-target non-CGO builds.

Real-engine acceptance must cover collections with and without validators,
each supported kind and index family, stable repeat capture, ordered-key and
BSON-type preservation, explicit Baseline acceptance, component-specific
changes, unrelated-object stability, new and removed collections, and changed
source selection. It must also exercise permission denial, invalid credentials,
TLS rejection, timeout, cancellation, interrupted cursors, concurrent catalog
changes, unsupported services, and failure without Baseline advancement.

Use a runtime credential that cannot read business documents. Command
observation must prove that product collection stays inside the allowed
catalog operations. Row and credential sentinels must be absent from exported
Evidence, Bundles, Baselines, diagnostics, and CI logs. Test-administrator
setup operations are separate from the collector's command inventory.

## References

Maintainer-approved scope and delivery sequence
https://github.com/aoci-spec/aoci-code/issues/135#issuecomment-6445455757

MongoDB catalog command output, cursors, and access requirements
https://www.mongodb.com/docs/manual/reference/command/listCollections/
https://www.mongodb.com/docs/manual/reference/command/listIndexes/

BSON type preservation in Extended JSON
https://www.mongodb.com/docs/manual/reference/mongodb-extended-json/
