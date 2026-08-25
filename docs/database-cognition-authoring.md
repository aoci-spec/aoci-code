# Database Cognition authoring

This is the short Agent workflow for turning saved Database Evidence into
model-authored table cognition. The normative contract is
[`aoci-database-cognition-authoring-v1.txt`](../spec/public/aoci-database-cognition-authoring-v1.txt),
and the evidence format is
[`aoci-database-evidence-v1.txt`](../spec/public/aoci-database-evidence-v1.txt).
Use the current Guide and schemas for exact states, fields, limits, and recovery
actions.

## Preconditions

- The repository uses Cognition Volumes and its ordinary Code governance is
  aligned.
- Database sources are explicitly configured and their catalog-only Evidence
  has been captured and accepted.
- The declared Database Volume is present. Authoring never creates a missing
  Volume or edits Root and Meta as a side effect.

Database collection is the only network step. Status, Maintain, authoring, and
Apply consume saved Evidence and do not reconnect or read business rows.

```bash
# Explicit catalog collection.
aoci database snapshot --source primary --json

# Offline cognition status.
aoci database cognition status --json
```

## Agent workflow

1. Call `aoci_maintain` without narrowing the ordinary Volumes scope. It returns
   the current complete machine-owned batch and the evidence binding for every
   target.
2. For each returned table, read its complete Evidence, any existing Entry, and
   only the code, migrations, configuration, or tests needed to understand its
   real role.
3. Author one complete canonical Entry: compact tag plus F/R/A/S. Do not derive
   semantics mechanically from names, columns, foreign keys, or timestamps.
4. Submit the exact complete batch through `aoci_update_entry`, preserving every
   machine-issued candidate and batch identity.
5. If more work remains, call Maintain again against the new preimage. When the
   final result is applied, follow Verify, Check, and Guide to closure.

Repair only the fields named by a structured `repair_required` result. A
`stopped` result ends that write attempt and must be handled from its current
CAS and Recovery evidence; do not guess a replacement Entry or replay a batch
blindly.

## Responsibility boundary

The model owns table meaning. AOCI selects and binds targets, transports saved
Evidence, validates complete candidates, computes deterministic impact, and
applies the existing atomic transaction. It never generates, truncates,
translates, or repairs F/R/A/S, and it never turns catalog facts into business
semantics.
