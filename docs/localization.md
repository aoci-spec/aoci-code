# Localization contract

AOCI-CODE supports two complete official project locales: `en-US` and `zh-CN`.
The default for a new project is `en-US`. Locale is persisted in the shared
`.aoci/config.json` file because it controls formal repository cognition and
host-agent contracts; `.aoci/config.local.json` cannot override it.

## Commands

```text
aoci init                         # new project, en-US
aoci init --locale zh-CN          # new Chinese project
aoci config get locale
aoci config set locale en-US
aoci config set locale zh-CN
```

Only `en-US` and `zh-CN` are accepted. An unknown value fails explicitly. The
CLI invocation reads the project Locale before constructing command help and
stable messages. The MCP server fixes the project Locale at process startup;
after changing Locale, restart the host's AOCI MCP process.

## Existing projects

An existing project created before the Locale field was introduced, with an
index but no Locale field, is deterministically treated as `zh-CN`. An ordinary
CLI command materializes `"locale": "zh-CN"` in the shared configuration
without rewriting `aoci.txt`. The MCP server and the hooks resolve the same
Locale without writing the configuration, so starting a server cannot change
the bytes a prepared migration or Scope Change preview is bound to. This rule
uses project state, not language detection or model inference.

## Changing an indexed project

On a Volumes v1 layout, `aoci config set locale <target>` is one resumable
transaction over exactly three files: the Root `aoci.txt`, whose
machine-readable `#Locale` line is the only line it rewrites; the team
`.aoci/config.json`; and `.aoci/baseline.json`, whose record of the Root
moves with it when the Baseline binds the Root. The Meta, Code, and Database
Volumes are guards, not targets: their bytes are checked unchanged before
every publish and are never rewritten, so the switch creates no authoring
debt. The three files are published in that order under the index lock, each
against its exact preimage, after the postimages were staged and an immutable
receipt `locale-<id>.json` was written under `.aoci/transactions`. A crash at
any point leaves that receipt: every other command then waits, the Agent
Guide names the receipt, and running the same command with the same target
resumes at whatever was already published. A different target is refused
until the pending switch completes. The managed AGENTS block is re-rendered
in the new Locale once the transaction has completed, which makes that one
file's Entry the only authoring work the switch creates, and the notice to
restart running MCP processes stays.

Locale is prospective. Every Entry already in the Volumes keeps the language
it was written in; only an Entry created or genuinely updated afterwards is
authored in the new Locale, and the Maintain authoring contract says so in
the Locale it is built for. A repository may therefore hold Entries in both
official languages, and no governance check reads prose to judge language.
What governance does check is that the Root's `#Locale` line and the
configured `locale` name one language: a hand edit of either file reports
`root_locale_mismatch`, with the same command as the repair, run with the
configured Locale (Root follows configuration) or with the Root's
(configuration follows Root). When the team file is absent, an explicit Root
marker is materialized as the project Locale; only a markerless historical
index keeps the `zh-CN` compatibility value.

Paths, source identifiers, APIs, tag symbols, FRAS/ABCDE markers, command and
tool names, JSON fields, machine states, project names, source code and
source comments, the AGENTS unmanaged region, and user-maintained documents
are never translation targets in either direction.

A Legacy single-file index keeps the Locale it was written in: the pipeline
that re-authored a Header and every Entry in the new language is retired
since 0.2.0, so `config set locale` to a different Locale exits 3 before any
write and the migration to Volumes v1 is the one next step. A repository that
still carries a Locale-migration receipt from an earlier release keeps
reporting that receipt's coverage through `check` until it is resolved.

## Catalog guarantees

Both official locales share one language-independent Manifest, Catalog,
Guide, MCP registration, and state machine. Release validation requires the
same asset IDs, kinds, variable schemas, consumers, enforcement relationships,
and protocol tokens. A missing asset, duplicate key, unknown key, invalid
format signature, or template-rendering failure is fatal. There is no fallback
between official locales and no output may combine them.

One documented exception: when a Safe Inventory git query fails, the CLI
prints the tail of git's own stderr on the line after the error, verbatim and
untranslated, introduced by a localized label that says so. git's text is the
only evidence of why it failed (a corrupt index, a killed process, a wall of
`Filename too long`), and suppressing it under a mismatched locale would hide
exactly what the operator needs. It is written to stderr only, never into a
`--json` result and never into the MCP stdout stream.

These gates establish structural and protocol parity. They do not claim to
prove the semantic quality of a translation; that remains a review and
real-product acceptance responsibility.
