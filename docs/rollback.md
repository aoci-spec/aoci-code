# Rollback

Keep the previously verified binary until the new version has passed representative repository checks.

1. Stop or restart hosts that run AOCI MCP so no old process remains active.
2. Restore the prior binary at the configured stable path.
3. Verify its SHA-256 and `aoci --version` against the retained installation record.
4. Run `status --deep`, `verify`, and `check`.
5. Preserve `.aoci` recovery evidence and failed-run artifacts for diagnosis.

Do not roll persistent formats backward by editing JSON, manifests, Baseline, Ledger, or `aoci.txt` by hand. If a newer version performed a documented irreversible migration, binary rollback is unsafe unless that release's migration procedure explicitly supports it. This requires maintainer review rather than an improvised repair.
