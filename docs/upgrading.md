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

Managed Scope now uses case-sensitive Git path-name semantics on macOS,
Windows, and Linux. Existing v2 Baselines created with `case_sensitive=true`
keep the same identity. A Baseline created with the former host-dependent
`case_sensitive=false` preimage is not rewritten during startup, Status,
Verify, Check, or Maintain: those paths report `scope_change_required`. Review
and apply the existing governed Scope transaction; when roles are unchanged it
is an identity-only migration, while any actual role changes remain visible in
the same Preview and approval flow.

Do not regenerate `aoci.txt`, delete `.aoci`, or force a Baseline update merely because the executable changed. If a future version requires persistent-data migration, its release notes must state the schema boundary, automatic and manual steps, rollback constraints, and tests. In the absence of such notes, treat an unexplained migration request as a stop condition.
