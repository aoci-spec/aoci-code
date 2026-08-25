# Cognition refresh in long-running tasks

This page is an operator-oriented summary. The normative checkpoint, receipt,
generation, and compatibility rules are
[`aoci-cognition-refresh-v1.txt`](../spec/public/aoci-cognition-refresh-v1.txt).
Overview transport is specified separately in
[`aoci-overview-delivery-v1.txt`](../spec/public/aoci-overview-delivery-v1.txt).
Use `aoci_rules`, the current tool schema, and live Guide output for the running
binary rather than copying machine values or status tables from this guide.

## Choosing a read

- Reuse an already reliable cognition set while it still matches the current
  repository, service, and required scope.
- Use `check_only=true` when only compact checkpoint facts are needed.
- Make an ordinary `aoci_overview` request when the task needs the complete
  selected scope. An explicit full request is not replaced by a checkpoint.
- If governed source has changed, first finish the nearest stable work unit.
  Then use the target-index finalizer or the live Maintain fallback, followed
  by Verify, Check, and Guide as instructed by the current result.

Do not repeatedly reload the Whole-Index for every function, test, or small
step. A major phase transition can justify a checkpoint, but the Agent still
decides whether another complete view is useful.

## Recovering after context compaction

Known Host context compaction invalidates the model's retained Whole-Index
knowledge. Resume as follows:

1. Read `aoci_rules` if the session contract is no longer reliable.
2. Call ordinary `aoci_overview` with `context_compaction` and a fresh event
   identity. Do not substitute `check_only` or a cognition probe.
3. Let the Host complete any required delivery handling. Once the body reaches
   `BODY_END`, continue the original source-bound task.

The compacted handoff may retain the identity needed to continue safely and
any unfinished write or Recovery state. It must not preserve, summarize, or
reconstruct the formal Whole-Index. Memory, source files, historical sessions,
search, and individual Entry reads cannot fill a delivery gap.

## Host integration

Repository integrations can install a compaction reminder or reload hook. See
[`agent-integrations.md`](agent-integrations.md) for the supported Host setup.
These hooks report the Host lifecycle event; they do not perform cognition
maintenance, mutate the compaction input, or decide that a model understands
the system.

## Reliability language

Receiving cognition, verifying transport, judging model usability, passing a
strict attestation, aligning governance, and claiming current-system
reliability are distinct statements. Report only the dimensions supplied by
the current result. The stable projection is documented in
[`aoci-cognition-state-v2.txt`](../spec/public/aoci-cognition-state-v2.txt).
