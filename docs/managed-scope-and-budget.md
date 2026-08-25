# Managed Scope and cognition budgets

Managed Scope decides which Safe Inventory objects receive cognition Entries,
which are fingerprinted as supporting evidence, and which are excluded before
content reads. The normative roles, authorization, budget, transaction, and
recovery contract is
[`aoci-managed-scope-and-budget-v1.txt`](../spec/public/aoci-managed-scope-and-budget-v1.txt).
Use current command output and Guide instructions for exact states and limits.

## Inspecting policy

```text
aoci scope show
aoci scope status
aoci scope explain <path>
aoci scope rule list
aoci scope budget show
```

Use `index` for objects that need a Whole-Index Entry, `observe` for evidence
whose exact changes matter without an Entry, and `exclude` for content that
must not be opened or fingerprinted. Safety exclusions outrank project rules.

## Changing policy

Add, update, or remove project rules with `aoci scope rule`. A configuration
edit is only a desired policy proposal: it does not retire Entries, reinterpret
the active Baseline, or authorize itself.

After editing policy:

1. Run `aoci scope status` and the live Guide.
2. Prepare the complete candidate and retention review requested by the current
   Scope workflow.
3. Use only the authorization path named by the current Preview.
4. Apply or recover the exact bound transaction, then complete Verify, Check,
   and Guide.

Do not use `scan --force`, manual Entry deletion, or direct Baseline edits to
make a proposed Scope change appear active. Risky reductions, sensitive
content, approval posture, budget changes, CAS conflicts, and Recovery are
machine-adjudicated; this guide intentionally does not copy their decision
table.

## Observe evidence

Observe changes do not create Entry debt automatically. Review each changed
path and the production, Spec, configuration, or cognition it can affect. If no
formal Entry needs a semantic update, acknowledge the exact reviewed evidence
through the command returned by the live workflow. Excluded content is not a
substitute for Observe.

## Budgets

Budget policy limits Whole-Index and field density without rewriting model
semantics. Inspect the current report before changing policy. Prefer removing
duplicate cognition and assigning low-semantic-density evidence an appropriate
Scope role before relaxing a machine limit.
