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
`scope_change_required`; run `aoci scope activate` to activate a configuration-only
change. It uses an empty Candidate Set through the existing preview/apply flow
and does not edit rules or author Entries. Changes needing model-authored
candidates still use explicit `scope preview` and `scope apply`.

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

## Container directories and progressive onboarding

AOCI can use a directory that is not itself a Git repository as its repository
root. Safe Inventory traverses that directory, so files below multiple child Git
repositories enter one repository-relative namespace. Each child `.git`
directory remains excluded by the hard safety boundary. This is a practical
bridge for a directory such as `/work` that contains `svc-a/` and `svc-b/`, but
it is not a workspace identity model: the resulting Baseline does not retain
the child repository boundaries.

Three limits matter:

- Child-repository `.gitignore` files do not control this non-Git traversal.
  Use project `exclude_dirs` / `exclude_files` settings or Managed Scope rules
  for paths that must stay out. `aoci config set exclude_dirs` replaces the
  whole list, so keep the defaults in the value you set.
- `init` keeps the host configuration it writes (`.mcp.json`,
  `.codex/config.toml`, or `opencode.json`) out of Managed Scope through a Git
  ignore rule, and a non-Git root has none. Left alone, that machine-bound file
  takes the `index` role at the first `scan`, so exclude it with a rule first.
- If the selected root is itself a Git repository, Git is the inventory
  authority. Git does not enumerate the contents of submodules, so initialize
  each submodule separately. A parent repository cannot use this non-Git bridge
  to absorb them.

To start with only part of a large container, add one `exclude` rule per
deferred module, and one for the host configuration, before the first `scan`:

```text
aoci --repo /work init --agent <name>
aoci --repo /work scope rule add later-svc-b --action exclude --pattern svc-b --pattern-kind directory --reason "defer initial indexing"
aoci --repo /work scope rule add host-config --action exclude --pattern .mcp.json --pattern-kind file --reason "machine-bound host configuration"
aoci --repo /work scan
```

Order matters because roles freeze at the first `scan`: narrowing afterwards
reduces coverage, which needs `scope approve` on a TTY even when no Entry has
been written yet.

Do not build this policy as `exclude **` followed by `index svc-a`. A user
`index` rule overrides the production profile and would pull tests and fixtures
into the Whole-Index instead of preserving their ordinary `observe` or
`exclude` roles.

To add the deferred module later, remove its rule and activate the resulting
Scope Change:

```text
aoci --repo /work scope rule remove later-svc-b
aoci --repo /work scope activate
```

With effective approval mode `auto`, an ordinary safe expansion can use
policy-bound Auto. When the preview requires human approval, activation stops,
saves the preview under `.aoci/scope-change/`, and prints the approve and apply
commands to run in order. Replace `<id>` with your reviewer identity; approval
requires a real TTY. Existing safety refusals remain refusals. After Apply, the next ordinary
`aoci_maintain` batch plans the newly admitted files. Until then, excluded
modules are absent from the formal index and cannot support a complete
cross-module conclusion.

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

That edits the desired policy. Activate it through the governed Scope Change
transaction:

```
aoci scope activate
```

Raising a budget is a policy relaxation, so activation stops with exit 2 and
prints the approve and apply commands. The preview is retained under
`.aoci/scope-change/`; `scope approve` requires a real TTY and its digest phrase.
With `--json`, the error details contain `preview_file`, `approve_command`, and
`apply_command`. Successful activation returns the existing Scope Change result.

For explicit candidate workflows, also keep artifacts under `.aoci/scope-change/`.
A new worktree file between preview and apply can change the bound inventory
and cause `managed_scope_replay_mismatch`; `.aoci` is always excluded.

### The candidate set

`scope preview` and `scope plan` read one JSON document,
`managed-scope-candidate-set/v1`. Every field it may carry is listed here; an
unknown field is refused, and a refused entry or disposition names its position
and the field that disqualified it (`entries[1]: candidate_id is empty`).

| Field | Required | Content |
| --- | --- | --- |
| `version` | yes | `managed-scope-candidate-set/v1` |
| `entries` | no (defaults to empty) | Entry candidates: sources whose Entry this transaction writes or rewrites. Each carries `candidate_id` (non-empty, unique within the set), `path` (normalized repository-relative, forward slashes), `source_sha256` (SHA-256 of the live source bytes), `new_entry` (one complete Entry line), `review_status` (`reviewed`), and `current_entry_sha256` (SHA-256 of the Entry line the index holds now; required when the path already has an Entry, omitted when it does not). |
| `dispositions` | no (defaults to empty) | One `scope-entry-disposition/v1` per Entry that leaves the index role: `version`, `source_path`, `current_entry_sha256`, `target_role`, `unique_semantics` (present, may be an empty list), `disposition` (`no_unique_semantics`, `transfer_to_existing_entry`, `transfer_to_spec`, `transfer_to_header`, `explicit_drop_approved`), `target_entry` (the receiving path, for transfers), `review_status` (`reviewed`), `reviewer`. `retain_as_index` is not a disposition but a request to revise the policy. |
| `header` | no | A header candidate: `candidate_id`, `current_header_sha256`, `new_header`, `review_status`. |
| `curation` | no | A curation document to activate with the policy. |
| `observe_review` | no | Acknowledgement of changed Observe evidence: `paths`, `review_status`, `reviewer`. `scope acknowledge` writes this for you. |
| `safety_approval` | no | A recorded approval for high-risk opt-ins; `scope safety approve` produces it. |

A configuration-only change uses an empty set: `version` alone, or `entries`
and `dispositions` empty, nothing else. `scope activate` supplies it for you.

### What each layout lets the candidate set carry

Under the Legacy layout, one `aoci.txt` holds every Entry and the transaction
edits that document. An index source whose bytes changed since the Baseline
therefore needs an Entry candidate, and a plan without one fails closed with
`managed_scope_index_source_stale: <path>`.

Under Volumes v1, `aoci.txt` is the Root manifest and holds no Entries. Entries
live in the Code Volume and are written only through `aoci_maintain` and
`aoci_update_entry`. So a Volumes candidate set carries policy only: `entries`
and `dispositions` must be empty and `header` absent, and a set that carries
any of them is refused with `managed_scope_volumes_entry_candidates_unsupported`
before anything is projected. A changed index source does not block a Volumes
Scope Change. The plan keeps that source's old fingerprint in the postimage
Baseline and lists the path under `source_stale_retained`; every plan object
for that path carries the retained digest, and the live digest the plan was
minted against is in the preview's `source_guard`. After Apply the source is
still Stale, so the next `aoci_maintain` plans it and one ordinary batch
aligns it.

### Leaving `scope_change_required`

While desired policy differs from active, Verify, Check, Status, Maintain, and
Guide report `scope_change_required` and every authoring path refuses to
write. Two exits exist, in either layout:

1. Activate the edit with `aoci scope activate`.
   Under Volumes this works over changed sources (previous section); under
   Legacy, use explicit preview/apply with an Entry candidate for each path
   named as stale. Activation does not supply those semantics.
2. Revert the edit (`scope rule remove <rule-id>`, or `scope budget set` back
   to the active values) so desired equals active again.

Aligning the sources first is not an option: Maintain will not write until the
policy is active. Before 0.1.0-rc12 that ordering, combined with the stale
guard, left a Volumes repository with one changed file and one pending rule
with no legal move.

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
