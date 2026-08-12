# Changelog

All notable public changes to AOCI-CODE will be documented in this file.

## Unreleased

- Report exact Code candidate binding mismatches and distinguish
  `code_plan.batch_id` from the cross-domain `authoring_batch.batch_identity`
  without changing the nine-tool MCP surface or request Schema; actual source
  drift remains a stopped replan condition instead of a copied-field repair.
- Expose the aggregate Check command in an authoring-required Volumes Guide and
  close the final successful batch through Verify, Check, and Guide while
  preserving intermediate-batch and Legacy Entries Stage behavior.

## v0.1.0-rc3

- Add an explicit `cognition_optimization` intent to `aoci_maintain` so a user
  can ask the model to review already-aligned Code Entries without source
  drift, while keeping ordinary maintenance behavior unchanged.
- Select optimization candidates deterministically from current Entry cost and
  C-band budget facts, require complete Entry submissions, and preserve the
  rule that AOCI itself does not generate, truncate, or compress semantics.
- Keep unchanged optimization submissions free of formal Index or Baseline
  writes, and reuse the existing atomic update, Baseline, Managed Scope,
  Recovery, and checkpoint paths for replacements and retries.
- Fix explicit Volumes Code and all-scope maintenance routing without changing
  Legacy or Database Cognition boundaries.
- Improve the bilingual public README, release-first one-step installation
  guidance, and self-contained Release archive branding.
- Preserve the nine MCP tool names and their stable identity, FRAS v2, and the
  existing Index and Baseline formats.

The first public availability date for v0.1.0-rc3 is 2026-08-10.

## v0.1.0-rc2

- Improve evidence-backed S-field authoring guidance for high-importance
  cognition objects while preserving `S:-` when no qualifying constraint exists.
- Prevent neighboring Entries from determining the current object's S field,
  and add bilingual authoring-contract and compatibility coverage.
- Make signed-package installation and verification links usable from Release
  archives, and clarify source-build versus signed-binary version identity.
- Preserve the existing public Specs, FRAS v2 machine contract, and nine-tool
  MCP surface.

The first public availability date for v0.1.0-rc2 is 2026-08-10.

## v0.1.0-rc1

- Establish the public AOCI-CODE CLI and MCP runtime under the canonical Go
  module `github.com/aoci-spec/aoci-code`.
- Preserve the `aoci` binary and the reviewed nine-tool MCP contract.
- Publish the public runtime contracts under `spec/public/`.
- Add public build, test, security, integration, and supply-chain guidance.
- Prepare the FSL-1.1-MIT legal assets for authorized public distribution.

v0.1.0-rc1 was first made publicly available on 2026-08-08.
