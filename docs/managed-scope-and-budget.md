# Managed Scope and cognition budgets

Managed Scope assigns every Safe Inventory object one role: `index`, `observe`,
or `exclude`. It reduces low-value index density without losing deterministic
change awareness for test evidence.

Use `aoci scope show`, `aoci scope explain <path>`, and `aoci scope rule list`
to inspect policy. Add, update, remove, or reset rules with `aoci scope rule`.
`aoci scope budget show` and `aoci scope budget set` manage the project budget;
`aoci scope observe-policy` selects `review_required` or `informational`.
`aoci scope approval-mode` selects `inherit`, `auto`, or `review`; `inherit`
follows the team-owned `automation.mode`.

Configuration edits are proposals. They never silently delete Entries or
advance the
Baseline. The desired configuration is a CAS guard while the Baseline receipt
owns the active policy identity. `aoci scope status` reports
`scope_change_required`; then prepare a
model-authored Candidate Set and run `scope preview`.

With `automation.mode=auto` and inherited or explicit Auto approval, run
`aoci scope authorize --preview-file preview.json` to inspect or persist the
immutable `policy_bound_auto` Receipt, then run `scope apply` with that Receipt.
`scope apply` can also generate the Receipt internally. No TTY or digest phrase
is used. The Receipt binds the exact policy, Envelope, current formal preimages,
projected index and budget facts, Retention Review, guards, writes, and recovery
direction. It is stored in the transaction Intent, so Resume does not approve
again. `review` still uses `scope approve` in a real TTY, `legacy` retains its
compatibility boundary, and `off` produces Plan/Preview only and writes nothing.
Resume or roll back an interrupted transaction with the matching scope commands.

The Candidate Set may include a reviewed Curation postimage. A Scope Preview
is also the single versioned Apply Envelope; its digest binds candidates,
policy and budget identities, exact formal pre/postimages, risks, guards, and
recovery direction.

Risk separates `budget_policy_change` from `budget_relaxation`; a Legacy
observe-to-stricter-enforce transition is tightening and may proceed in Auto.
Raising a maximum, returning enforce to observe, weakening safety, including
sensitive content, incomplete Retention Review, P0/P1, source writes,
third-party bytes, or missing recovery is blocked rather than converted into a
routine approval prompt. Reduction size alone does not block Auto.

Automatic workflows never silently delete source files or ungoverned Entries.
When Scope Policy, complete model-owned Retention Review, exact Envelope, CAS,
budget gates, and recovery all agree, Auto may atomically retire Entries as a
role transition. It never deletes the corresponding business source.

Observe changes do not create Entries. Review their impact on production,
Spec, platform, or Header cognition, update formal Entries when needed, then
run `aoci scope acknowledge --reviewed-by <identity>`. Excluded content is never
opened and produces no drift.

For the normative lifecycle, retention dispositions, safety boundaries,
transaction order, and token gates, see
[`aoci-managed-scope-and-budget-v1.txt`](../spec/public/aoci-managed-scope-and-budget-v1.txt).
