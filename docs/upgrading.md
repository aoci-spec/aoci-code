# Upgrading

An AOCI upgrade is a binary replacement, not an index rewrite.

1. Record the current binary path, `aoci --version`, and SHA-256.
2. Ensure the target repository has no pending Guide recovery or unresolved governance transaction.
3. Back up the current binary without deleting it.
4. Verify the new artifact at the required assurance level. At minimum verify
   the selected archive against `SHA256SUMS` and run `aoci --version`; publisher
   signature, provenance, SBOM, and manifest checks are additional assurance
   layers described in [`install.md`](install.md).
5. Place the new binary beside the old one and run `--version` plus `doctor` against a disposable repository.
6. Update the stable path atomically where the host permits it.
7. Check whether each active host has loaded the replacement. Refresh or restart
   the AOCI MCP integration only when the current host still exposes the old
   server; a running server process retains the old binary identity even though
   the file on disk has changed.
8. After any required refresh, compare `serverInfo.version` with the exact
   binary's `--version`, and inspect the host process's actual executable and
   `--repo` command line. A `volume_read_only` response by itself identifies an
   unsupported command path, not a proven CLI/MCP version mismatch.
9. Run `verify`, `check`, and the current Guide on representative Volumes
   repositories. Run `status --deep` only on a Legacy repository.

## Managed Scope path semantics became host-independent

Path matching now uses Git semantics -- exact and case-sensitive -- on every host. Earlier versions probed the filesystem and folded the result into the applied scope identity, so the same repository could carry different governance identities on Linux and Windows.

Nothing to do if your Baseline was established on a case-sensitive filesystem: the identity preimage is unchanged and your receipt stays valid.

If it was established under the case-insensitive semantics, `aoci scope status` reports `scope_change_required`. Run the ordinary governed flow:

```bash
aoci scope preview --candidate-file <empty-candidate-set.json>
```

Where both semantics assigned the same roles the plan is identity-only: no role changes, no Entry changes, `aoci.txt` byte-identical, and policy-bound auto can authorize it without a human. Where a rule and a path genuinely differ only in case, the plan carries that real role change and is authorized as one.

Do not regenerate `aoci.txt`, delete `.aoci`, or force a Baseline update merely because the executable changed. If a future version requires persistent-data migration, its release notes must state the schema boundary, automatic and manual steps, rollback constraints, and tests. In the absence of such notes, treat an unexplained migration request as a stop condition.
