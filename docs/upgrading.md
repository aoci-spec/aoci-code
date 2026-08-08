# Upgrading

An AOCI upgrade is a binary replacement, not an index rewrite.

1. Record the current binary path, `aoci --version`, and SHA-256.
2. Ensure the target repository has no pending Guide recovery or unresolved governance transaction.
3. Back up the current binary without deleting it.
4. Verify the new artifact's signature, checksum, manifest, and provenance.
5. Place the new binary beside the old one and run `--version` plus `doctor` against a disposable repository.
6. Update the stable path atomically where the host permits it.
7. Restart MCP host processes; a running server retains the old binary identity.
8. Run `status --deep`, `verify`, and `check` on representative repositories.

Do not regenerate `aoci.txt`, delete `.aoci`, or force a Baseline update merely because the executable changed. If a future version requires persistent-data migration, its release notes must state the schema boundary, automatic and manual steps, rollback constraints, and tests. In the absence of such notes, treat an unexplained migration request as a stop condition.
