# Rollback

Keep the previously verified binary until the new version has passed representative repository checks.

Before switching versions, check whether an active Recovery receipt records
an Intent. If it does, finish Recovery with the version that wrote the receipt
before downgrading, even if the transaction has yet to write a formal asset.
Keep the receipt and its evidence available until Recovery completes.

1. Stop the old AOCI MCP process, or refresh/reopen only the host integration
   that still uses it. A full host restart is not required when that host can
   reload MCP configuration and prove the new process identity.
2. Restore the prior binary at the configured stable path.
3. Verify its SHA-256 and `aoci --version` against the retained installation record.
4. Run `verify`, `check`, and the current Guide for Cognition Volumes. Run
   `status --deep` only for a Legacy repository.
5. Preserve `.aoci` recovery evidence and failed-run artifacts for diagnosis.

If only parallel batch drafts were in flight, call `aoci_maintain` without
arguments after downgrading to reissue the pending work. During an upgrade, a
reply can still show the old `remaining` count for an orphan removal assigned
by the previous version. Redeliver the first sibling by `batch_id` with the
new version to record that assignment.

A repository that uses directory or file names the index could not spell before
v0.1.0-rc14 (a directory holding a space, `=`, `(`, or `（`, a file holding `[`) cannot
be read by an older binary; see [`upgrading.md`](upgrading.md). Rolling back
below rc14 there makes the older binary refuse the layout or report those
Entries orphan and missing. `verify` changes nothing, but do not let an older
agent act on that report: it removes and re-authors the Entries under the wrong
path. Rolling forward again reads the repository as aligned.

Do not roll persistent formats backward by editing JSON, manifests, Baseline, Ledger, or `aoci.txt` by hand. If a newer version performed a documented irreversible migration, binary rollback is unsafe unless that release's migration procedure explicitly supports it. This requires maintainer review rather than an improvised repair.
