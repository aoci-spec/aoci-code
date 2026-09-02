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

Keep every artifact under `.aoci/scope-change/`. A file written into the
worktree between preview and apply changes the state the plan was minted
against, and apply then refuses with `managed_scope_replay_mismatch`; `.aoci` is
excluded from the Safe Inventory unconditionally, so artifacts there are
invisible to the plan.

```
mkdir -p .aoci/scope-change
printf '{"version":"managed-scope-candidate-set/v1","entries":[],"dispositions":[]}' > .aoci/scope-change/candidates.json
aoci scope preview --candidate-file .aoci/scope-change/candidates.json --json > .aoci/scope-change/preview.json
aoci scope approve --preview-file .aoci/scope-change/preview.json --actor <id> --out-file .aoci/scope-change/approval.json
aoci scope apply --preview-file .aoci/scope-change/preview.json --approval-file .aoci/scope-change/approval.json
```

`scope approve` requires a real TTY and the digest phrase the preview carries.
Raising a budget is a policy relaxation, so it is never applied silently.

### What a raised budget does not buy

The budget governs what may be written. It does not govern what a model can
receive, and those two ceilings are not the same number.

A Whole-Index is delivered as a chunk chain, `overview_delivery.chunk_tokens`
per chunk with a default of 8000, so the index size decides how many round trips
a complete delivery takes: about 8 chunks at 58000 tokens, about 50 at 400000,
about 92 at 733000. Every chunk is a place where a host context compaction can
void the chain, and only the closing attestation detects that it happened. There
is no partial-repository delivery to fall back on — `aoci_overview` scopes by
domain (`code`, `database`, `all`), never by subset.

So a repository whose plan estimate runs to several hundred thousand tokens has
a role problem before it has a budget problem. `aoci scope preview` reports
`estimated_whole_index_tokens` as `1800 + index_count * 110` before any Entry is
authored; read it early. If most of the tracked tree is entering the index role,
move test fixtures, generated samples and vendored sources to `observe` or
`exclude` first. Raise the budget for a repository that is genuinely that large
in the parts that matter — not to postpone the role decision.
Judgement still applies in the other direction: a Whole-Index far above the
ceiling is one a model cannot assimilate in a single delivery, so role reduction
is the first answer and a raise is the second.

### Content-volatile files whose cognition never changes

Some generated files change bytes on every build while their Entry text never
moves — a compatibility matrix, a rendered report, a checksum listing. Every
such change makes the Entry stale, and stale means the model is asked to
re-confirm.

There is deliberately no per-file bypass for this. An Entry's binding to its
source SHA is the property the whole write chain protects: a file exempted from
staleness is a file whose real semantic change would never reach review again,
and nothing in the machine can tell a harmless regeneration from a meaningful
one. That judgement is exactly what the binding exists to force.

Two governed levers cover the case instead:

- **Change the role once.** If the file's cognition is generic, it does not
  need an Entry: move it to `observe` (drift becomes an acknowledgement, not an
  authoring round) or `exclude` through the ordinary Scope Change flow. One
  approval, and the per-build cost is gone.
- **Resubmit the same text.** If the Entry is worth keeping, answer the stale
  candidate with the identical Entry text. A byte-identical resubmission is
  recognized, applies zero formal writes, and advances the Baseline —
  `duplicate_applies` in the result is that path confirming itself. The cost is
  one maintain/update round, and current-state enumeration is already folded
  out of that round's transport.

For the normative lifecycle, retention dispositions, safety boundaries,
transaction order, and token gates, see
[`aoci-managed-scope-and-budget-v1.txt`](../spec/public/aoci-managed-scope-and-budget-v1.txt).
