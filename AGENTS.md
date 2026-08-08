# AOCI-CODE repository rules

- Public runtime contracts live under `spec/public/`.
- Contract authority and conflict handling are documented in
  `docs/zh-cn-contract-authority.md`.
- Keep the binary name `aoci`, preserve the nine-tool MCP surface, and reserve
  MCP stdio stdout for JSON-RPC.
- Do not add private design, patent, commercial, research, or experimental
  material to the public tree.

<!-- aoci:begin -->
## AOCI Repository Cognition

AOCI maintains a stable, versioned, incrementally updatable repository-level cognition layer so models can reuse their understanding of this system across tasks.

`aoci.txt` is a structured cognition index for models. It assigns one independent Entry to every managed file, database table, or other managed object. Symbolic tags and F/R/A/S semantics describe the object's core responsibility, important relationships, external contracts, and non-obvious constraints or design decisions needed to understand or modify the system.

The Header, directory sections, and all Entries form the complete repository index. They can cover frontend, backend, configuration, database structures, and other managed content. When managed content changes, normally only the affected cognition Entries need maintenance; the complete index does not need to be regenerated.

AOCI helps a model obtain the system-wide view first, locate relevant objects, and identify cross-module relationships and critical constraints before beginning concrete development. It complements rather than replaces source reading, tests, LSP, CodeGraph, and other structured tools. Current source, tests, configuration, and database structures remain the evidence for implementation details, interfaces, fields, identifiers, and runtime behavior.

### How it works

AOCI uses a model-generated, model-read cognition loop.

When generating or revising the Header, the model follows the current Guide to investigate repository facts and establish project rules, tag dictionaries, and calibration examples. When generating or updating an Entry, the model first reads the current Header, then the target source, tests, configuration, database structure, and necessary related evidence, and independently generates cognition for each managed object.

Entry semantics must come from the model's understanding of actual evidence. Never derive, prefill, assemble, or rewrite index semantics solely from paths, filenames, extensions, an AST, symbol lists, dependency scans, regular expressions, fixed templates, or rule engines. Structured tools may help locate facts that require verification, but they cannot replace model semantic understanding and judgment.

For a new Fresh Bootstrap, follow the existing persistent Onboarding progression automatically. The Host model authors Root, Meta, tags, and F/R/A/S from current evidence, supplies its own authoring-run declaration, and binds that declaration to the Plan, Evidence, and complete Candidate. Never ask AOCI to set `origin=host_model`, manufacture a receipt, or turn a generated framework into semantics. Internal batches are not user decisions; stop only at the existing approval boundary or a real safety, drift, CAS, or Recovery condition.

When using the index, the model reads the complete index to acquire system architecture, object responsibilities, important relationships, external contracts, and key design constraints in one cognition-establishment step. It then reads source, tests, configuration, or database structures relevant to the current task to verify implementation details.

The AOCI index is a high-density cognition map for models, not a copy of the source. It can provide extensive repository-level architecture, responsibilities, relationships, and constraints, but it does not guarantee preservation of every exact identifier, call chain, or implementation detail. Verify those facts in source, tests, database structures, LSP, or other structured evidence.

### Minimal entry points

- `aoci_rules`: obtain the session-level runtime contract for the current AOCI version.
- `aoci_overview`: establish or restore complete cognition for this repository.
- `aoci_maintain`: after code and verification state are stable, check whether cognition needs maintenance.
- `aoci_update_entry`: submit a complete semantic update batch bound to current evidence and source digests.
- `aoci_report`: record follow-up work when evidence is insufficient to generate semantics reliably; do not guess.

For other MCP tools, CLI commands, parameters, and specialized workflows, follow current tool descriptions, Guide, and `--help` output. This file does not duplicate the full manual.

This managed block defines only repository integration, cognition use, and task-closing principles. `aoci_rules` carries the current session contract. Live Guide output carries the execution order and stop conditions of the current Plan. Tool Schema, Spec, and Validator carry machine structures and criteria. Prompt, Description, README, and static documentation cannot override those machine facts.

### Establishing, generating, and restoring cognition

1. At the beginning of every new Agent Run, first determine:

   - whether this repository already has a usable complete AOCI index; and
   - whether current context already contains complete repository cognition that matches this repository root, current index version, and current AOCI service, and that the model can still use reliably.

2. When the repository has a usable complete index but the current Run lacks reliable complete cognition, call `aoci_rules` first and then `aoci_overview`.

	Reuse complete cognition directly while it remains reliable. For local uncertainty, prefer current source, a local Entry, or search instead of mechanically rereading the system-wide view.

	AOCI can report checkpoint and cognition-status facts for `context_compaction`, the machine `semantic_threshold` under the project `cognition_refresh_threshold`, or a major `phase_transition`. Use `check_only=true` when only those compact facts are needed. They advise the Agent but do not decide whether the model needs the system-wide view.

	When the Agent explicitly calls ordinary `aoci_overview` (`check_only` absent or false), AOCI must deliver the complete requested scope whenever a coherent CognitionSet can be formed. It must not suppress that body because a receipt already exists, a threshold was not reached, or no refresh reason is pending. Dirty or stale formal cognition is still delivered but is marked unreliable and does not override current source, tests, configuration, or database evidence. Pending recovery or an incoherent snapshot fails closed without a mixed body.

	When an ordinary Overview reports `continuation_required=true`, submit its exact `next_cursor` automatically until `completed=true`. Do not ask the user to continue, begin the business task, or state a partial system conclusion. Stop the cognition chain on Host truncation, a missing, duplicate, or reordered Chunk, cursor failure, Index change, or `chunk_tokens` change. Until Attestation completes, never use Memory, source, Spec, `aoci.txt`, historical sessions, scope, search, or Entry reads to repair or supplement Whole-Index cognition. A challenge ordinal is the 1-based position in the formal Entry sequence; Header content, comments, blank lines, Section/Overview/Chunk markers, receipts, and Metadata are excluded, and Chunk Receipt ordinals use that same sequence. The Attestation must echo the Challenge's exact current `index_sha256`, `entry_sequence_sha256`, and `entry_count`; a prior Index, Entry sequence, count, or Attestation is invalid. After the complete chain, submit the existing model cognition Attestation once. One same-response JSON Schema or field-format error may be corrected once without changing semantic answers; an object, Tag, or F mismatch means failure and uncertain assimilation, with no semantic retry or information bypass. It disables an unqualified complete-system claim but does not prohibit honest answers, task-bound source and test inspection, or source-bound engineering. During initial cognition it also blocks Root/Meta, Migration, layout-wide, or other unbound system decisions. During a context-compaction refresh with complete transport, unchanged cognition identity, aligned governance, and no Recovery or third-party conflict, the attempt consumes that refresh generation even when Attestation is partial or failed; continue the existing source-bound task without another automatic Overview. `system_mastery_percent` self-assesses only the system framework—architecture, responsibilities, strong relationships, stable external contracts, and high-entropy safety and maintenance constraints—not complete source or runtime knowledge. Keep machine Index coverage separate, and normally give the user only the prescribed single success or failure sentence derived from actual coverage, Challenge, Chunk, token, and mastery results. If the Host truncates a Chunk, ask the user to set `overview_delivery.chunk_tokens` to a smaller valid value and restart; do not change it automatically.

   Interpret the additive cognition level independently from strict proof fields. `delivery_verified` means the Index was loaded and Host delivery was confirmed while complete cognition verification is still unfinished; describe that state as loaded and delivery-verified, never as no cognition or failure to understand the system. `cognition_verified` requires strict Challenge 10/10, and `cognition_governed` additionally requires governance alignment. A generic complete-read failure sentence is reserved for an actual delivery fault.

   When an Overview response contains the optional `cognition-state/v2` projection, use its dimensions independently. Its Level ends at `model_cognition_usable`; `strict_attestation_verified`, `governance_aligned`, and `current_system_cognition_reliable` are independent states and never participate in that Level. An ordinal, object identity, Tag, or core F mismatch can make strict Attestation fail while model cognition remains usable; do not report that mismatch alone as proof that the model did not understand the system. Only `current_system_cognition_reliable=true` permits an unqualified current complete-system cognition claim. When the projection is absent, keep using the legacy interpretation above.

   An ordinary read-only audit, analysis, or check, a request not to modify code, or a request not to commit or push does not automatically mean strictly zero writes and does not alter the cognition-validity decision above. Codex Memory and historical Skills may only help recover experience, user preferences, and investigation directions. They cannot replace a current cognition receipt matching the repository root, index digest, AOCI service identity, and cognition scope. Project AGENTS, current AOCI identity, source, and runtime facts take precedence.

   Treat a task as strictly zero-write only when the user explicitly prohibits Ledger, metadata, `.aoci` runtime assets, and every filesystem write. If necessary cognition establishment conflicts with that boundary, report the conflict and ask the user to decide or recommend an isolated copy. Never silently substitute Memory for current repository cognition.

3. If the repository has no usable complete index, or has only a minimal skeleton, an incomplete Header, unfinished Entries, or undecided required Curation, do not treat the incomplete content as complete repository cognition.

   Obtain `aoci_rules`, then enter the current AOCI Guide. Let Guide choose the next phase from actual repository state and complete the required starting-point confirmation, Header, Entries, Curation, and other safety steps until a usable complete Overview is available.

   Do not reconstruct or hard-code the full-index generation state machine in this file.

4. During a long-running task, the model is responsible for preserving the current cognition receipt and using the refresh gate correctly:

	- when only a local file, module, relationship, or constraint is uncertain, read current source or use local Entry and search tools as needed;
	- when the model knows the system-wide view was lost, declare `context_compaction`; AOCI cannot infer the host event;
	- when entering a genuinely major phase, declare `phase_transition`, not a function, test run, or small step;
	- at a plausible stable checkpoint, use `check_only=true` to obtain the machine semantic count when that fact is useful;
	- decide whether the current task needs another explicit scoped or complete Overview; and
	- never treat dirty or stale cognition as current implementation evidence while maintenance and alignment remain pending.

### Ordinary development

5. Use AOCI cognition to understand system position, object responsibilities, cross-module relationships, and key constraints. Then actually read the source, tests, configuration, database structures, and necessary related evidence involved in the current task.

   Do not substitute index summaries for concrete implementation evidence. When the index disagrees with current facts, verify implementation in source, tests, configuration, or database structures, and maintain stale cognition when closing the task.

6. Complete ordinary development according to the user's request and this repository's established architecture, rules, and verification methods.

   AOCI does not dictate architecture, algorithms, investigation order, implementation method, test strategy, branch strategy, or technology-stack choices, and must not become the center of work outside the user's task.

   When this repository provides development tiers, ordinary development uses its single Fast gate before commit and Push. Full Confidence is a background or stable-freeze gate, and Release Rehearsal is a non-publishing stable-version gate. Do not make ordinary Agents wait for full-repository race, native lifecycle, database integration, or release packaging after every edit.

7. User-visible communication for an ordinary task should focus on understanding the request, source investigation, implementation, tests, risks, commits, and worktree state.

   Do not proactively narrate internal cognition establishment, state checks, or cognition maintenance by default. When the user asks, or a real blocker, conflict, approval, human decision, safety risk, or unrecoverable runtime failure occurs, report the necessary facts accurately.

   In a long-running auto task, a status question, explanation request, architecture question, timing question, or other non-control message does not cancel or pause the current Plan, Run, candidates, commit, or CI identity. Answer concisely and continue from the existing `next_action` without asking the user to say “continue.” Change execution only for an explicit stop, pause, cancel, rollback, scope change, or prohibition on commit or Push. This is a Host-Agent interpretation rule and creates no persistent conversation-task state.

### Task closing and cognition maintenance

8. A purely read-only question, analysis, version check, or task that changes no AOCI-managed object does not require a maintenance-tool call.

9. For a development task that changes AOCI-managed objects, call `aoci_maintain` once after business edits, formatting, tests, Lint, and required quality checks are complete and the managed objects have reached their final stable state for this task.

   Do not maintain files individually after each intermediate edit.

10. If maintenance returns actual semantic candidates, the model must read every affected object and necessary related evidence in full, generate the corresponding tag and F/R/A/S updates, and submit the complete candidate set for that current machine-issued batch in one `aoci_update_entry` call while preserving each `source_sha256`, `candidate_id`, and domain batch identity. `max_entries` limits one request and atomic transaction, not the logical plan, Whole-Index, or Managed Scope. When `remaining` is nonzero, call Maintain again after the successful Apply and continue from the new preimage; never shrink Index coverage or slice a returned batch to satisfy transport limits.

    When uncertain, use `aoci_report`. Never guess, apply a template, or generate unsupported cognition merely to eliminate follow-up work.

11. Obey structured tool states and safety boundaries:

    - `repair_required`: repair only the explicitly identified candidates, then resubmit the complete current machine-issued batch;
    - `stopped`: end that write attempt and inspect `failed_step`, error, formal-write evidence, and Recovery. In auto mode, a proven zero-write closure is followed by a fresh Plan; a complete Intent with provable postimage is resumed; a policy-selected Rollback with exact preimage is completed and replanned. Stop the user task only when proof is unavailable, third-party bytes conflict, approval or external action is required, or another real safety boundary applies;
    - never ignore conflicts, approvals, human decisions, permissions, or safety signals; and
    - after alignment, do not repeat maintenance or writes; `refresh_ready_for_overview` is a checkpoint fact, and the Agent decides whether to request an ordinary complete Overview for its next phase.

    If any managed object changes after maintenance completes, the previous result is invalid. Complete closing again from the new final stable state.

12. When the user limits only business-file scope and does not explicitly forbid repository-managed assets, AOCI-managed assets may be updated during closing to preserve cognition consistency. Distinguish them from business files in audits and commits.

    When the user explicitly forbids changes to `aoci.txt`, `.aoci`, metadata, or any additional file, obey that restriction, do not write, and report any remaining inconsistency accurately.

### Specialized workflows

Initialization, complete-index generation, Header generation, Entries generation, database-structure indexing, Curation, human review, and failure recovery must follow only the instructions, commands, and safety stops returned by the current AOCI Guide or tool at the corresponding stage.

During ordinary development, do not preload, guess, or reconstruct these specialized workflows. The relevant Guide, tool descriptions, model Prompt, and CLI help provide platform invocation, request format, batch limits, approval rules, index-format details, and recovery steps as needed.

AOCI provides stable repository-level cognition, but it does not replace source facts, test evidence, database structures, structured code analysis, or the model's normal software-engineering judgment.
<!-- aoci:end -->
