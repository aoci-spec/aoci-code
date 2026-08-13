# Contract Authority Boundaries

This is the English rendering of
[`zh-cn-contract-authority.md`](zh-cn-contract-authority.md), which remains the
original. This page only assigns fact sources and conflict handling; it copies
no machine vocabulary, numeric value, status table, or complete workflow. The
concrete stable contracts live in the sources listed below.

| Layer | Authority scope | Must not carry |
| --- | --- | --- |
| Current source, configuration, and tests | Implementation, interfaces, fields, and runtime facts of the currently compiled version | Must not silently diverge from published Spec; a divergence is handled as a compatibility defect |
| `spec/` | Public formats, JSON, state semantics, and compatibility boundaries | Holds no machine-consumable vocabulary copies and does not replace current runtime state |
| `internal/machinecontract` | Machine numbers, vocabularies, and sets consumed jointly by validators, scanners, and protocol code | Carries no long-form explanation, entry tutorial, or experimental conclusion |
| Validator and deterministic state machines | Machine adjudication of Apply, Warning, exit codes, state transitions, and safety rejections | Generates no F/R/A/S, Header, or Curation business semantics |
| `textassets` manifest-registered `en-US` and `zh-CN` resources | Current production Prompt, host contracts, MCP Descriptions, Guide text, and repository template bodies; both official locales must keep isomorphic asset sets, variables, and machine tokens | Manifest notes, Goldens, another locale, or historical copies must not become runtime fallback sources |
| Prompt | Tells the model how to generate semantic candidates from current evidence | Does not expand machine capability or override Validator, Spec, permission, or safety adjudication |
| Live Guide output | Next steps, command order, and safety stops for the current Plan, mode, and repository state | Historical Guide and static documents must not replace current output; Guide text sets no second machine criterion |
| MCP Description and CLI Help | Tool purpose, input boundaries, side effects, and entry selection | Does not rebuild the complete state machine or override the real Input Schema, result JSON, or current Guide |
| `aoci_rules` runtime rules | The session-level contract for establishing, reusing, and maintaining cognition | Does not copy specialized stage manuals; stages follow only the current Guide, tool Schema, and Help |
| `AGENTS.md` | Repository integration plus start and closing guidance | Fixes no single run state and copies no machine vocabulary or specialized state machine |
| README and `docs/` | Explanation, examples, and operational background for people and host integrators | Not a production load source; cannot adjudicate alone against Spec, machine contracts, or current output |
| `testdata/golden` | Derived compatibility probes of production output or digests | Never read by production code; not a body-of-fact source |
| Private archives (outside the public tree) | Non-normative evidence bound to internal history, models, or environments | Must not enter production embedding, manifests, Prompt, Spec, or compatibility Goldens |

## Conflict handling

1. When a current implementation fact disagrees with a published Spec, treat it
   as a defect and stop silently reinterpreting either side as the "new
   standard"; repair through the compatibility-change process and synchronize
   tests.
2. When static text disagrees with the current Guide, real Schema, or Validator
   result, locate the problem from the current machine result and correct the
   document or resource; never adjust the machine result to match an old copy.
3. When any production locale resource is missing, duplicated, damaged,
   variable-inconsistent, or manifest-inconsistent, fail explicitly; never fall
   back to the other locale, Go string literals, Goldens, experimental Prompts,
   or historical documents.
4. When a machine vocabulary, number, or set must change, modify only the
   corresponding authority in `internal/machinecontract` and update its
   semantic explanation and equivalence tests; shell scripts, Spec, Prompt, and
   tests must not re-save complete machine sets.
5. Compatibility snapshots of text and behavior must bind real production
   assembly results. Direct resources and long Prompts prefer SHA-256; only
   multi-resource assembly outputs or complete protocol structures keep
   full-text Goldens.
