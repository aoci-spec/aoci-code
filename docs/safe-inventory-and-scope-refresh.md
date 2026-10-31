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

A Git-ignored directory whose every file is a hard exclusion anyway, such as
`node_modules`, a directory named in `exclude_dirs`, or a directory named in
`exclude_root_dirs` at the root or wherever git ignores it, is one
line in the inventory, and Git is never asked to list it (#97: before rc18 a
large `node_modules` cost seconds on every call, and a pnpm workspace whose
links form a cycle never finished on Windows). Every other ignored directory,
`gen/` or `out/` for example, is listed file by file as before, so a rule or
an exact opt-in reaches its files exactly as it did.

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

The built-in secret-name rule has two sets, chosen by the team key
`sensitive_name_rules`: `v1`, the original names, and `v2`, which also
matches `prod.env`-style names and credential words in data and
configuration file names (#148). `init` persists `v2` for a new repository;
an absent key means `v1` and keeps an old repository's identity, and
changing it is a Scope Change. [Managed Scope and cognition
budgets](managed-scope-and-budget.md) lists the exact shapes.

## Changing an existing Baseline's scope

Do not use `scan --force` to conceal managed-set changes; it refuses over a
Managed Scope Baseline. Edit the policy with the `aoci scope` rule commands,
read what the change would do with `aoci --json scope status`, and activate
it with `aoci scope activate`. A coverage reduction pauses for a person:
approve it with `aoci scope approve` and apply it with `aoci scope apply`, as
[Managed Scope and cognition budgets](managed-scope-and-budget.md) shows, or
answer the host dialog when the team channel admits one. That transaction
reuses the existing Baseline, repository lock, CAS, Ledger, and Recovery
directories; repeating the exact Apply is idempotent, and unknown Baseline
bytes are a third-party conflict and are preserved.

The Legacy `baseline scope` refresh (`plan`, `preview`, `approve`, `apply`,
`status`, `resume`) is gone since 0.2.0-rc2. It had refused every Managed
Scope repository since rc20 and named these commands instead; a repository
created before Managed Scope establishes it with `aoci scope activate`.

The normative contract is
[`aoci-safe-inventory-and-scope-refresh-v1.txt`](../spec/public/aoci-safe-inventory-and-scope-refresh-v1.txt).
