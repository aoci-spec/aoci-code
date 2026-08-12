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
index but no Locale field, is deterministically treated as `zh-CN`. AOCI
materializes `"locale": "zh-CN"` in the shared
configuration without rewriting `aoci.txt`. This rule uses project state, not
language detection or model inference.

## Changing an indexed project

Changing Locale records a versioned, resumable migration receipt containing
the source and target Locale plus machine-readable totals and pending state for
the Header, ordinary Entries, legacy `.aoci` governance Entries, Curation,
post-Header managed index text, and the AGENTS managed block. It does not
translate or rewrite formal index cognition at configuration time.

The ordinary Host-Agent Plan and Guide then require, in order:

1. a target-locale Header plus every AOCI-managed explanation and directory
   label after the Header, applied atomically through the governed Header path;
2. every ordinary Entry regenerated from current source evidence and applied
   through the governed Entries path;
3. every persisted Curation role and reason regenerated and applied through
   the governed Curation path.

The Header transaction also deterministically reclassifies receipt-bound
Entries below `.aoci` as non-semantic machine governance assets and removes
those legacy Entry lines. They are never silently skipped, and their volatile
Ledger, report, configuration, or Verify History bytes are never used as
translation evidence. Ordinary source Entries retain their Plan,
`source_sha256`, CAS, and atomic-Apply boundaries.

For ordinary Entries, the Locale gate preserves the filename and tag symbols,
repository paths, and code-like facts that are present in the exact
`source_sha256`-bound source evidence. This evidence intersection prevents
target-language compound prose from becoming a new API merely because it
resembles an identifier. Natural-language wording in every F/R/A/S field may
change, while project-specific names that cannot be identified mechanically
remain an explicit semantic-review responsibility of the generating model.

The migration coverage object is emitted by Plan and Guide. Check fails while
the receipt exists, and Guide cannot report `aligned` or `complete` while any
managed category is unresolved. Runtime Prompt, Guide, Rules, Overview, and
tool Description text is supplied from the target-locale official Catalog and
is reported separately from repository files.

Each successful Apply advances only its own receipt items. Receipt updates use
the project's atomic CAS pipeline and are retry-safe. AOCI cannot report
`aligned` while any receipt item remains. The same rules apply in both
directions.

The managed surface is Header prose and dictionaries; Entry F/R/A/S natural
language; Curation role/reason; the AOCI-managed AGENTS block; AOCI-generated
index explanations and directory labels; and official Prompt, Guide, Rules,
Overview, and tool Description text. Paths, source identifiers, APIs, tag
symbols, FRAS/ABCDE markers, command and tool names, JSON fields, machine
states, project names, source code and source comments, the AGENTS unmanaged
region, and user-maintained documents are not automatic translation targets.
Completion is therefore proven by the managed-surface receipt, never by a
repository-wide or Overview-wide “zero Han characters” heuristic.

## Catalog guarantees

Both official locales share one language-independent Manifest, Catalog,
Guide, MCP registration, and state machine. Release validation requires the
same asset IDs, kinds, variable schemas, consumers, enforcement relationships,
and protocol tokens. A missing asset, duplicate key, unknown key, invalid
format signature, or template-rendering failure is fatal. There is no fallback
between official locales and no output may combine them.

These gates establish structural and protocol parity. They do not claim to
prove the semantic quality of a translation; that remains a review and
real-product acceptance responsibility.
