# Cognition Volumes v1

This page is the short operating guide for Volume-first repositories. The
normative layout, object, authoring, compatibility, and transaction contract is
[`aoci-cognition-volumes-v1.txt`](../spec/public/aoci-cognition-volumes-v1.txt).
The text format is defined by
[`aoci-index-format-v1.txt`](../spec/public/aoci-index-format-v1.txt). Use the
current Guide and tool schemas for the running binary; this guide does not copy
their state tables or numeric limits.

## Repository shape

A Volume-first repository has a Root manifest, shared Meta rules, and a Code
Volume. A Database Volume is optional and is enabled only through its governed
workflow. Root and Meta provide project and authoring context; model-authored
business Entries live in their object Volumes.

Fresh repositories should use `aoci init` and then follow the live Guide.
Legacy repositories remain readable and use the governed migration workflow;
do not emulate migration by manually splitting the old index.

## Daily Code workflow

For a planned feature or bug fix:

1. Run `aoci cognition plan diff --target-index aoci.code.target.txt`. If the
   conventional target is absent, the CLI initializes it from formal Code.
2. Update the complete target Entry immediately as each managed source file is
   planned or changed. Add a complete Entry with every new file; use the exact
   reuse marker when source changes but Entry semantics do not.
3. Implement and verify the source change.
4. Run `aoci update-entry` once. Go binds the final source hashes, validates the
   complete target batch, applies it through the governed transaction, and
   synchronizes the consumed target from formal Code.
5. Follow the returned Verify, Check, and Guide instructions until Guide is
   complete.

The target is a generated planning asset, not formal cognition. Never treat it
as Apply authority or add a separate Locale marker to it.

## Fallback authoring

If target finalization stops because the target is incomplete, source or Scope
has drifted, deletion is involved, validation fails, or Recovery is pending,
stop that write attempt and follow the structured result. The ordinary fallback
is one `aoci_maintain` call for the current complete machine batch, followed by
one `aoci_update_entry` submission of that exact batch. Do not maintain files
one by one or reduce a batch to fit a model preference.

## Database cognition

Database cognition is authored from accepted, saved Schema Evidence and never
from business rows. See
[`database-evidence.md`](database-evidence.md) for evidence collection and
[`database-cognition-authoring.md`](database-cognition-authoring.md) for the
Agent workflow.

## Derived system views

Relation, lineage, impact, evolution, and module views are read-only projections
over formal cognition. They do not create another Volume, Baseline, Apply path,
or persistent module index. Their contract is
[`aoci-system-cognition-runtime-v1.txt`](../spec/public/aoci-system-cognition-runtime-v1.txt).
