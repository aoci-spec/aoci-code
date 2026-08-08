# Safe Inventory and Baseline Scope Refresh

This page documents the legacy single-role Baseline transition. Projects using
`index`/`observe`/`exclude` roles use [Managed Scope and cognition budgets](managed-scope-and-budget.md);
both lifecycles share Safe Inventory v2, the global lock, CAS, AtomicWrite,
Ledger, and Recovery.

Safe Inventory v2 is the default source-discovery boundary used by Scan,
planning, Business Source Manifest, and onboarding. It keeps Git-ignored
secrets and generated runtime state out before content reads or hashes, while
still discovering non-ignored new source and non-Git projects.

Managed Scope can retain otherwise-safe ignored path names for rule evaluation
without weakening hard exclusions. Only a winning `index` or `observe` role
allows later fingerprinting.

Use the machine receipt when an AI Agent needs complete counts:

```text
aoci --json source manifest
```

The receipt includes the ordered source list, per-file identities, Safe
Inventory rules and selection identities, Curation identity, Git HEAD when
available, line-ending policy, and aggregate SHA. `generated_at` is audit
metadata and is not an identity input.

Tracked sensitive files are not silently opened. The default is fail closed
with required human review. An exceptional project can explicitly opt in one
exact sensitive-file path through configuration, even when Git ignores it;
the path remains visible as high risk. Globs are not accepted, and runtime,
generated, AOCI/VCS, or unsafe filesystem objects cannot use this exception.
Templates such as `.env.example` are not treated as secrets, and
`package-lock.json` remains governed by project policy/Curation.

## Changing an existing Baseline's scope

Do not use `scan --force` to conceal managed-set changes. Build a versioned
scope Preview instead:

```text
aoci --json baseline scope preview \
  --baseline-timestamp 2026-07-31T00:00:00Z
```

The Preview separates Added/Removed/Preserved objects from actual byte drift.
Changed or missing business source blocks Apply. Built-in secret, runtime, and
generated exclusions can move out of scope safely. The refreshed Managed Set
uses the final Curation-selected Business Source set plus formal cognition
assets, so a Curation exclusion is shown as an ordinary removal. An ordinary
configured or curated reduction requires one exact TTY confirmation:

```text
aoci baseline scope approve --preview-file preview.json --actor HUMAN_ID
aoci baseline scope apply --preview-file preview.json --approval-file approval.json
```

If the process stops after its immutable Intent, inspect and resume the same
transaction:

```text
aoci --json baseline scope status --transaction TRANSACTION_ID
aoci --json baseline scope resume --transaction TRANSACTION_ID
```

Apply reuses the existing Baseline, repository lock, CAS, Ledger, and Recovery
directories. Repeating the exact Apply is idempotent; unknown Baseline bytes
are a third-party conflict and are preserved.

The normative contract is
[`aoci-safe-inventory-and-scope-refresh-v1.txt`](../spec/public/aoci-safe-inventory-and-scope-refresh-v1.txt).
