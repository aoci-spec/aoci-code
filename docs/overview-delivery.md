# Overview delivery

This page is the short operator guide for `aoci_overview`. The normative
transport, receipt, compatibility, and reliability contract is
[`aoci-overview-delivery-v1.txt`](../spec/public/aoci-overview-delivery-v1.txt).
Use the current tool schema and `aoci_rules` for the behavior of the running
binary; this guide deliberately does not copy their field tables, numeric
limits, or state machine.

## Normal use

Call `aoci_rules` once when the current session contract is unavailable, then
make an ordinary `aoci_overview` request for the required scope. A complete
model-visible body runs from `BODY_BEGIN` through `BODY_END`. Transport and
status metadata belongs to the Host and is not additional cognition text.

When the body arrives in one response, continue the original task. Do not add
a model confirmation call merely to acknowledge delivery.

## Host continuation

If the running tool explicitly reports that continuation is required, the Host
follows the private cursor until completion. The model does not choose chunks,
ask the user to continue between chunks, or begin work from a partial body.
Search and individual Entry reads are not repairs for an incomplete delivery.

On truncation, cursor failure, snapshot change, or another delivery error, stop
that chain and follow the current structured error and `aoci_rules`. Never
reconstruct missing cognition from memory, source files, historical sessions,
or this document.

## Reliability

Delivery integrity, model cognition usability, strict attestation, governance
alignment, and current-system reliability are separate facts. Use the running
result fields and the public
[`cognition-state/v2` contract](../spec/public/aoci-cognition-state-v2.txt)
instead of inferring one from another.
