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
aoci scope activate
```

Where both semantics assigned the same roles the plan is identity-only: no role changes, no Entry changes, `aoci.txt` byte-identical, and policy-bound auto can authorize it without a human. Where a rule and a path genuinely differ only in case, the plan carries that real role change and is authorized as one.

## Directory and file names the index could not spell

From v0.1.0-rc14 a section header carries a directory whose name holds a space,
`=`, `(`, or `（`, and an Entry carries a file name that holds `[`. Upgrading
needs no action: an index is never rewritten, one that aligned before aligns
now, and a repository that was wedged by a directory name with a space reads as
aligned. An index that has a root section, which is every index this release
creates, reads the same at the origin and in every checkout; one without (an
earlier release could leave an index whose sections all live under one
directory) may still resolve differently in a copy, as it always could. (An
index that never aligned because an earlier release filed root files under a
directory section, the first file it met having lived in a directory whose name
begins with `(`, keeps that release's reading until the ordinary orphan repair
runs, which now completes.) Under a root whose own path holds one of those
characters, the root section spells the root in full and every later section
continues it as the original reading reads it back. Every release has written
that shape, this one included, so the two spellings of the root are deliberate;
do not edit the headers to make them match.

The reverse direction is not supported for a repository that *uses* such a
name. Checked with the released v0.1.0-rc13 binary, at the origin and in a copy:

- an index that holds a bracketed file name is refused as an invalid cognition
  layout (`code_parse_warning`);
- a directory whose name holds a space, `=`, `(`, or `（` resolves somewhere else, so
  its Entries are reported orphan and missing, and an agent that acts on that
  report removes and re-authors them under the wrong path;
- a scope rule that holds `[` invalidates the policy.

Everything else an index created by this release carries, under such a root
too, v0.1.0-rc13 reads aligned and can keep writing to. So
once a repository uses one of those names, move every host and teammate that
works on it to v0.1.0-rc14 or later together, and do not roll one of them back.

## An in-flight Scope Change approval does not survive an upgrade

A `scope approve` artifact binds the preview envelope digest, not the plan. The
host-independent path change removed two fields from that envelope, so a preview
and approval produced by v0.1.0-rc5 are rejected by v0.1.0-rc6 with
`managed_scope_preview_invalid`, and a preview produced by rc6 is rejected by rc5
with `managed_scope_preview_identity_invalid`. This is a refusal, not a
corruption: nothing is written and no state is damaged.

Finish or discard any pending preview/approval pair before replacing the binary.
After the upgrade, regenerate the preview and re-approve; the plan itself is
unchanged where `interaction_required` did not move.

## A repository initialized by rc20 or later needs rc20 on every machine

`init` now persists `exclude_root_dirs` in the team configuration. A binary
older than rc20 does not know the field: it evaluates the repository without
it, so its policy identity differs from the active one and every governance
surface answers `scope_change_required`; and because the configuration writer
keeps only the fields it knows, one `aoci config set` from that binary drops
the field from `.aoci/config.json`. Upgrade every machine that works on such
a repository before touching the team configuration. Repositories initialized
before rc20 carry no such field and are read identically by rc19 and rc20.

## A repository initialized by the 0.2.0 line needs that binary on every machine

`init` now persists `sensitive_name_rules: "v2"` in the team configuration
(#148). The rule set enters the Safe Inventory rules identity, so a binary
older than this release evaluates such a repository under the `v1` rules it
knows, computes a different policy identity, and answers
`scope_change_required` on every governance surface; one `aoci config set`
from that binary drops the key from `.aoci/config.json`. Upgrade every
machine that works on such a repository before touching the team
configuration. Repositories initialized earlier carry no key, are read as
`v1` by every binary, and keep their identity; a team opts in with
`aoci config set sensitive_name_rules v2` followed by `aoci scope activate`,
which previews the paths the new rule takes out of the index and pauses for
approval because that is a coverage reduction. `aoci doctor` lists those
paths beforehand without changing anything.

## The Legacy single-file layout is read-only from 0.2.0

A repository whose `aoci.txt` carries its own Header and Entries, with no
`#AOCI-ROOT-MANIFEST` line, is the Legacy layout that `init` wrote before
Cognition Volumes. From 0.2.0 it can be read but not authored. `verify`,
`check`, `status --deep`, `index score`, `index inventory`, `index header
show`, `aoci_overview`, `aoci_get_entries`, `aoci_search`, and `aoci_header`
keep working. `aoci_maintain` stops with `legacy_read_only_migrate_required`
and `next_action=migrate_legacy_repository`; `aoci_update_entry`,
`aoci_remove_entry`, `aoci_report`, `update-entry`, and `remove-entry` refuse
with that token and write nothing; `index agent guide` returns a blocked
guide whose commands carry the migration; `config set locale` and
`init --locale` cannot change the Locale the index was written in. The
retired commands are `ai` (the `ai` configuration block is still read for
compatibility and used by nothing), `index build`, `index update`,
`index entries`, `index header draft|diff|apply`,
`index agent plan|stage|header|curation`, and `doctor --net`.

The exit is the existing migration: `aoci cognition onboard start --json`
plans the Legacy-to-Volumes migration, approval and application follow its
next steps, and the migrated repository is maintained like any Volumes v1
repository. One Legacy write remains: a Managed Scope transaction can still
retire an Entry by disposition or carry a source-bound Entry candidate for a
stale Index source, so a Legacy repository can reach the active policy the
migration requires.

Close pending Legacy work before upgrading. A `header-*.json` receipt under
`.aoci/transactions`, left by a Legacy Header write, gates every write and
the migration until it is closed, and 0.2.0 has no command that closes it:
finish it with the 0.1.x binary that wrote it (`aoci index header diff`, then
`apply`, for that run). Entries, Header, and Curation draft runs that a
Legacy Stage left under `.aoci` are inert in 0.2.0; nothing applies them.

Do not regenerate `aoci.txt`, delete `.aoci`, or force a Baseline update merely because the executable changed. If a future version requires persistent-data migration, its release notes must state the schema boundary, automatic and manual steps, rollback constraints, and tests. In the absence of such notes, treat an unexplained migration request as a stop condition.
