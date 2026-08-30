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

Tightening the posture needs nothing special, but **lowering** it — for example
`review` back to `auto` — is an `approval_policy_relaxation`. A posture may not
ratify its own weakening, so `policy_bound_auto` refuses it and the transition
is authorized under the posture the current Baseline receipt proves: the plan
reports `interaction_required`, and one `scope approve` in a real TTY ratifies
it. From the next transaction onward the receipt reads the relaxed posture and
ordinary changes are automatic again. A posture is therefore reversible through
the same governed path that tightened it, at the cost of exactly one review.

Pass `--out-file` to `scope approve` so the approval it mints lands in a file
that `scope apply --approval-file` can read. Without it the artifact goes to
stdout and has to be redirected by hand; forgetting the redirect discards a
confirmation that cannot be reused. The file is created only if nothing is
already there, and is written readable by its owner alone, because until the
change is applied anything that can read it can stand in for the human who
typed the phrase. `scope safety approve` and `baseline scope approve` take the
same flag.

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

## Scale boundary

A **new** project's Whole-Index budget defaults to 200000 target / 300000
warning / 400000 max tokens, and every Entry the model reads back rides that
budget. These numbers are the default written into `.aoci/config.json` by `aoci
init`; they are not a system limit. An existing repository's effective budget is
whatever its own `.aoci/config.json` records, and a repository whose config
carries no `cognition_budget` block keeps the original 120000 / 180000 / 240000
permanently, because that policy's identity is stamped in its Baseline and
moving it would force a Scope Change the repository did not earn.

At the density this repository averages (~122 tokens per Entry), a 400000 ceiling
corresponds to roughly three thousand managed objects and the original 240000 to
roughly two thousand. Repositories approaching their ceiling should first spend
the reduction levers — Scope roles that keep non-cognition files out of Index,
tighter S under the C-driven quotas, and an explicit `cognition_optimization`
review pass. Splitting the Code Volume itself into partitions is deliberately
not supported in v1.

When those levers are spent, raising the budget is a supported change, not a
workaround:

```
aoci scope budget set --max-tokens <n> --warning-tokens <n> --target-tokens <n>
```

That edits the desired policy. Activating it goes through the governed Scope
Change transaction, and `scope preview` emits the artifact the rest of the flow
consumes only when it is given a candidate set — a configuration-only change
still needs one, empty:

```
printf '{"version":"managed-scope-candidate-set/v1","entries":[],"dispositions":[]}' > candidates.json
aoci scope preview --candidate-file candidates.json --json > preview.json
aoci scope approve --preview-file preview.json --actor <id> --out-file approval.json
aoci scope apply --preview-file preview.json --approval-file approval.json
```

`scope approve` requires a real TTY and the digest phrase the preview carries.
Raising a budget is a policy relaxation, so it is never applied silently.
Judgement still applies in the other direction: a Whole-Index far above the
ceiling is one a model cannot assimilate in a single delivery, so role reduction
is the first answer and a raise is the second.

For the normative lifecycle, retention dispositions, safety boundaries,
transaction order, and token gates, see
[`aoci-managed-scope-and-budget-v1.txt`](../spec/public/aoci-managed-scope-and-budget-v1.txt).
