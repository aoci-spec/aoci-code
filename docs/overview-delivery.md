# Overview Delivery Integrity

An ordinary `aoci_overview` call delivers one complete formal Whole-Index. The
body is only the start marker, the exact `aoci.txt` bytes, and the end marker;
the former generated directory inventory and explanatory wrapper are absent.
Repository root, counts, token estimate, Index SHA, body bytes/SHA, and delivery
state remain in Metadata. `check_only=true` remains a compact checkpoint with
no body.

`overview_delivery.chunk_tokens` is the only delivery-size setting. It defaults
to 8,000 tokens and accepts 4,000 through 24,000. Missing configuration also
uses 8,000. An invalid value fails
instead of being adjusted. This setting affects transport only; it does not
change formal cognition or any governance identity.

If the framed body fits, the tool returns it once. Otherwise the first ordinary
call immediately returns Chunk 1. Each Chunk preserves UTF-8 and complete Entry
boundaries, every Entry appears once in formal order, the Header appears only
in Chunk 1, and only the final Chunk has the end marker. A single Entry larger
than the configured budget fails without truncation.

The existing `overview-chunk-receipt/v1` proves Chunk order, Entry ordinals,
bytes, token estimate, SHA, cursor, and completion. A cursor binds the Index,
Chunk budget, next Entry ordinal, and prior Chunk SHA. Invalid, skipped,
reordered, cross-Index, or cross-configuration chains fail closed; an exact
replay of a genuine cursor idempotently re-serves the identical Chunk.
No persistent delivery Session or transaction is created. Because a cursor is
deterministically re-derivable from those bound facts, an unchanged Index and
Chunk budget accept the same cursor across MCP process restarts; the Volumes
governance binding below remains in-memory session state and does not carry
across processes.

For Volumes v1, Overview reads live governance from the same read-only facts
calculator as Verify, Check, Guide, and Maintain. It does not reuse a prior
Ledger conclusion or require Verify History. A clean Code-only repository has a
legal Database-absent state, while an enabled Database Volume must have current
Evidence and cognition. Missing/orphan/stale/unbaselined objects, pending
Curation or observe review, ownership conflict, budget failure, pending
transaction/Recovery, third-party conflict, or invalid structure keep
`governance_aligned=false`.

The first body or Chunk binds that assessment to its scope/body identity and
the current formal composite and Baseline bytes in small in-memory MCP session
state. Continuation and Attestation compare the current snapshot to the bound
one, so a changed source, formal Volume, Baseline, transaction, Recovery, or
conflict cannot be combined with the old body for Level 4. Git HEAD alone is
not part of this binding; commit-only changes do not invalidate otherwise
identical cognition. Ledger and Verify History remain optional audit side
effects, not alignment prerequisites or formal identity inputs.

There is one ordinal sequence. For Legacy Whole-Index,
`formal_entry_ordinal` is the 1-based position among formal Entries; Header
content, comments, blank lines, Section/Overview/Chunk markers, receipts, and
Metadata do not count. Volumes uses the existing canonical object order within
the requested scope. Chunk first/last ordinals, Challenge ordinals,
Attestation answers, and coverage all consume this exact shared sequence; there
is no display or implicit-offset ordinal.

When `continuation_required=true`, an AI Agent must submit the exact returned
cursor and continue automatically. It must not ask the user to continue,
start the business task, or state a partial system conclusion. Search, scoped
reads, and Entry lookup can answer local questions only after acceptance; they
cannot repair an incomplete Whole-Index chain or failed Attestation. Host
truncation, missing/duplicate/reordered Chunks, or cursor or
Index/configuration changes stop the cognition chain. Attestation failure
stops complete-cognition acceptance and semantic retry, not honest answers or
source-bound work. Before Attestation completes,
Memory, source, Spec, direct `aoci.txt`, historical sessions, and local AOCI
reads are prohibited as cognition supplements. Host truncation stops delivery;
the user may set
a smaller valid `overview_delivery.chunk_tokens` value and restart. AOCI does
not detect Host capacity or shrink Chunks automatically.

After the final Chunk, the Host verifies the aggregate
`overview-delivery-receipt/v1` and submits the existing one-ordinal
`model-cognition-attestation/v1`. Delivery integrity, machine Entry coverage,
Challenge binding, and model-reported mastery remain separate. Only a complete,
continuous, Host-confirmed, aligned chain with a passing Attestation can set
complete cognition reliable.

Initial establishment keeps that strict standard and also blocks unbound
Root/Meta, Migration, layout-wide, and other system decisions after a partial
or failed Attestation. A context-compaction refresh with complete transport,
unchanged cognition identity, aligned governance, and no pending Recovery or
third-party conflict consumes its refresh generation even when Attestation is
partial or failed. It records an uncertain receipt, returns
`full_system_claim_disabled_source_bound_task_continuation_allowed`, and lets
the Host continue the existing source-bound task without another automatic
Overview. Local or historical evidence still cannot repair the Attestation.

The model submits semantic Challenge answers once from the delivered index. It
may correct one same-response JSON Schema or field-format error without
changing answers; any object, Tag, or F mismatch is a final fail/uncertain
result. Challenge Metadata includes only ordinals and binding data, never target
identities, Tags, F, or answer summaries.

Each Challenge and Attestation explicitly binds the current `index_sha256`, the
SHA-256 of the current ordered formal Entry/object sequence, and the current
Entry count. The digest covers all three facts plus the selected current
ordinals. Selection is recomputed only from the current sequence: one object is
chosen from every current stratum by a stable object-identity key, then reported
at its current ordinal. Nearby additions or removals therefore do not
randomize all unaffected targets, but no prior ordinal, prior Entry set, or old
Attestation can validate against the new Index.

For a Code object, the Attestation accepts either the exact delivered
repository-relative path or its one-to-one `code:<path>` canonical identity.
It performs no path cleaning, case folding, basename lookup, or fuzzy
resolution. Database objects still require the exact `database://` identity
and Tag matching remains exact. Core F is judged as a semantic match (a
paraphrase or dropped clause of the delivered F still counts; another Entry's F
does not). The generic pass rule remains at least 80 percent fully correct with
at most one object-identity miss. The default Challenge selects one
deterministic ordinal, so strict recall requires 1/1. The Challenge measures
assimilation, while the Host receipt already proves the bytes arrived; a
foreign envelope or 0/1 fails, while other complete-answer claim shortfalls may
grade partial.

`host_delivery_confirmation` and `model_cognition_attestation` bind the same
delivered body and may be submitted together or in separate calls in either
order; the server remembers each half for the delivery until both are present,
and a fresh complete delivery attempt resets that memory.

`system_mastery_percent` is a self-assessment of system-framework knowledge:
architecture, module responsibilities, strong relationships, stable external
contracts, and high-entropy safety and maintenance constraints. It is not
complete source, branch, identifier, test, or runtime knowledge, and does not
remove the need to read source before modifying an object. Report it separately
from machine Index coverage.

Overview also reports an additive human status without changing the existing
reliability fields: Level 0 `no_cognition`, Level 1 `index_loaded`, Level 2
`delivery_verified`, Level 3 `cognition_verified`, and Level 4
`cognition_governed`. Level 2 explicitly means that project cognition was
loaded and Host delivery was verified while strict complete-cognition
verification is unfinished. It must not be described as no cognition or as a
failure to understand the system. `model_full_cognition_reliable`,
`cognition_assimilation`, and `governance_aligned` retain their existing
meanings.

Successful ordinary user feedback is one short sentence with actual Entry,
token, Chunk, Challenge, coverage, and mastery results. A delivery failure is
one short sentence with received/total Chunks, approximate coverage, the
reason, and an explicit statement that complete system cognition cannot be
claimed. Confirmed delivery with an absent, partial, or failed Attestation is
reported instead as loaded and delivery-verified with complete cognition
verification unfinished. Detailed cursors, SHA values, Metadata, and
Attestation JSON remain machine evidence.

With a normal AOCI MCP, use Rules, the automatic Chunk chain, then Attestation.
Direct segmented reading of `aoci.txt` is only a no-MCP fallback or diagnostic;
it lacks equivalent Chunk-chain integrity, governance and Baseline state,
pending-transaction protection, and Attestation proof. No second file-read
protocol is defined.

The normative contract is
[`aoci-overview-delivery-v1.txt`](../spec/public/aoci-overview-delivery-v1.txt).
