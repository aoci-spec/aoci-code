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

Do not regenerate `aoci.txt`, delete `.aoci`, or force a Baseline update merely because the executable changed. If a future version requires persistent-data migration, its release notes must state the schema boundary, automatic and manual steps, rollback constraints, and tests. In the absence of such notes, treat an unexplained migration request as a stop condition.
