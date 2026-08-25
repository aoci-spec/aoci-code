# Localization contract

AOCI-CODE supports two complete official project locales: `en-US` and `zh-CN`.
The default for a new project is `en-US`. One shared `locale` setting controls
the runtime UI and contracts, managed Locale-migration surfaces, and the
language requested for every newly created or genuinely updated Entry.
It is persisted in `.aoci/config.json`; `.aoci/config.local.json` cannot
override it.

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

When the team configuration is absent but cognition already exists, one valid
explicit `#Locale` marker supplies the project Locale and is materialized in
the shared configuration without rewriting the index. A markerless historical
index, or a pre-Locale configuration that has no `locale` field, retains the
compatibility value `zh-CN`. This reads only the formal marker; it does not
infer a language from Entry prose.

## Changing an indexed project

Changing Locale activates the target Catalog immediately for the current CLI
process and for newly started MCP processes. It never bulk-translates ordinary
Entries and never schedules unchanged source Entries merely because their prose
uses the previous Locale. Existing Entry bytes remain exact; a repository may
therefore contain historical Entries in one language and later Entries in the
other.

For a Legacy layout, the command records a versioned, resumable migration
receipt for the Header, post-Header managed index text, legacy `.aoci`
governance Entries, persisted Curation, and the AGENTS managed block. The
ordinary Host-Agent Plan and Guide then require, in order:

1. a target-Locale Header plus every AOCI-managed explanation and directory
   label after the Header, applied atomically through the governed Header path;
2. every persisted Curation role and reason regenerated and applied through
   the governed Curation path.

Ordinary Entry paths are not migration backlog. When the normal source-bound
workflow later creates an Entry or updates one because its managed object
actually changed, its F/R/A/S natural-language prose uses the active project
Locale. Unaffected Entries remain byte-for-byte unchanged.

For a Volumes v1 layout, changing Locale immediately replaces only Root's
machine-readable `#Locale` line through an exact-preimage Root → config →
Baseline transaction. Meta, Code, and Database Volume bytes remain unchanged,
and no layout-wide Entry authoring debt is created. The runtime Catalog,
managed AGENTS block, and subsequent Guide/Maintain authoring contracts read
the same configured Locale. Formal Meta remains the exact repository authority
even if its historical dictionary prose predates the new runtime Locale.

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

When a Legacy migration receipt exists, its coverage object is emitted by Plan
and Guide. Check fails while the receipt exists, and Guide cannot report
`aligned` or `complete` while a receipted managed category is unresolved.
Historical ordinary Entry language is not such a category. Runtime Prompt,
Guide, Rules, Overview, and tool Description text is supplied from the
target-Locale official Catalog and is reported separately from repository
files.

Each successful Apply advances only its own receipt items. Receipt updates use
the project's atomic CAS pipeline and are retry-safe. AOCI cannot report
`aligned` while any receipt item remains. The same rules apply in both
directions.

The eagerly migrated managed surface is Legacy Header prose and dictionaries,
Curation role/reason, the AOCI-managed AGENTS block, AOCI-generated Legacy
index explanations and directory labels, and official Prompt, Guide, Rules,
Overview, and tool Description text. Entry F/R/A/S is prospective: only a new
or genuinely updated Entry uses the active Locale. Paths, source identifiers,
APIs, tag symbols, FRAS/ABCDE markers, command and tool names, JSON fields,
machine states, project names, source code and source comments, the AGENTS
unmanaged region, and user-maintained documents are not automatic translation
targets. Completion is therefore proven by the receipted managed surface,
never by a repository-wide or Overview-wide language heuristic.

## Catalog guarantees

Both official locales share one language-independent Manifest, Catalog,
Guide, MCP registration, and state machine. Release validation requires the
same asset IDs, kinds, variable schemas, consumers, enforcement relationships,
and protocol tokens. A missing asset, duplicate key, unknown key, invalid
format signature, or template-rendering failure is fatal. There is no Catalog
fallback between official locales. One runtime UI or authoring-contract shell
uses one Locale; the formal index may intentionally retain older Entry prose.

These gates establish structural and protocol parity. They do not claim to
prove the semantic quality of a translation; that remains a review and
real-product acceptance responsibility.
