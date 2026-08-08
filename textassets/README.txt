textassets/ contains embedded, versioned natural-language assets used by AOCI.

The package is intentionally independent from internal CLI, workflow, model, draft,
and storage packages. Deterministic core packages may depend on textassets without
acquiring an AI or network dependency.

Asset classes:

1. prompts
   Text sent to a model to generate semantic cognition such as Header or Entry
   candidates.

2. contracts
   Text returned to host agents or local users to define workflow, safety
   boundaries, recovery, cognition reuse, initialization, and maintenance behavior.

3. messages
   Keyed, localizable CLI and MCP messages whose format signatures are identical
   in every official Locale.

4. templates
   Files materialized into a repository, including the AGENTS.md managed block,
   minimal aoci.txt skeleton, .aoci/.gitignore, and supported host configuration
   or Hook templates. There is no separate root templates package.

Manifest layout:

- manifest.json contains the base catalog introduced by the first migration batch.
- manifests/*.json contains function-domain catalog fragments such as Host-Agent,
  Maintain, Init, or repository templates.
- Locale lifecycle is declared once in manifest.json. official_locales are
  production-selectable and complete; development_locales may contain a partial
  asset subset but cannot be selected by production Load.
- en-US is the default Locale for new projects. en-US and zh-CN are both complete,
  production-selectable official_locales. A project selects exactly one team-wide
  Locale; loading never falls back to another Locale.
- The Locale directory embed pattern automatically includes two-letter
  language-region directories such as zh-CN and en-US.
- Fragments contain only the catalog version and assets. The loader rejects
  unknown fragment fields and merges fragments by file name.
- Asset IDs remain globally unique across the base catalog and every fragment.
- kind is the machine-verifiable use class and must match the asset ID namespace
  (contracts/, prompts/, or templates/). description is explanatory only and is
  never treated as an authority for dispatch or validation.
- used_by contains canonical path.go:Symbol consumers. Release tests resolve each
  path and symbol and prove that it references the matching typed resource ID.
- protocol_tokens are language-independent fields, status values, commands, and
  protocol keywords required in every locale that contains the asset.
- locale_anchors protect approved locale-specific wording without forcing another
  language to retain Chinese sentences, punctuation, or example titles.
- variables is the sorted, exact text/template placeholder set. An omitted field
  means that the resource must contain no placeholders.
- enforced_by records real path.go:Symbol deterministic validation boundaries for
  model prompts. Release tests resolve every referenced production symbol. An
  explicit empty array means the rule is semantic or snapshot-only; it must not
  be presented as machine-proven.

Catalog integrity:

- Production Load validates only the requested official asset. A broken unrelated
  resource body cannot block version, help, deterministic commands, or another
  resource family. Loads are uncached, so concurrent first reads and retry after a
  failure cannot observe partial global state or a permanently cached error.
- ValidateForRelease is the strict complete-catalog gate. Every official locale
  must be complete. Every present development asset is still validated, while
  absent development assets are allowed until promotion.
- There is no fallback to source literals or an older resource copy.
- Duplicate IDs, duplicate resource paths, missing locale files, unregistered
  locale files, unsupported kinds, malformed templates, unknown placeholders,
  declared-but-unused placeholders, duplicate placeholders, missing protocol
  tokens or locale anchors, undeclared locale directories, and malformed manifest
  metadata are fatal release defects.
- Every file below each declared Locale directory is a resource and must be
  registered exactly once. Manifest fragments are merged deterministically by
  file name.
- testdata/golden contains derived compatibility snapshots. Goldens are test
  oracles, never production inputs or alternate authoritative sources.

Unified model Prompt boundary:

- All messages sent to a configured model endpoint live below the selected
  textassets/<locale>/prompts directory and are registered in the unified catalog.
- Header and Entry system fragments, Header and Entry dynamic user templates,
  and the AI connectivity probe therefore use the same loader and validation.
- The former top-level prompts package and its second manifest were removed;
  production consumers load only stable textassets IDs.
- Unknown Locale values, missing keys, incomplete official catalogs, and mismatched
  message format signatures fail closed. The development/official boundary permits
  an additional Locale to land incrementally without becoming production-selectable
  before it passes the complete-catalog gate.

Template migration boundary:

- textassets/<locale>/templates/AGENTS.md.tmpl is the unique AGENTS managed-block
  source for that official Locale.
- textassets/<locale>/templates/minimal-index.txt.tmpl is the unique minimal
  aoci.txt skeleton source for that official Locale.
- The remaining repository and host templates are likewise owned by
  textassets/<locale>/templates. A root templates directory has no production
  consumer and is not required to exist in a clean clone.
- internal/hooks retains rendering, managed-block governance, external-byte
  preservation, Safety validation, backup, and atomic-write behavior.
- No package-level compatibility variable loads either resource during init.
- Repository-level text overrides remain disabled until an explicit trust boundary
  is designed.

Locale selection and migration discipline:

- New projects default to en-US. Explicit zh-CN initialization remains supported,
  and a pre-Locale project is classified as zh-CN by the legacy compatibility path.
- The repository config Locale is team-wide and cannot be overridden by local
  configuration. Changing it starts a resumable formal migration; completion
  requires the Header, ordinary Entries, managed index metadata, and AGENTS managed
  block to match the target Locale, with governance-only .aoci Entries handled by
  their explicit reclassification contract.
- Locale migration rewrites only AOCI-managed natural language. Paths, source code,
  identifiers, APIs, commands, project-specific names, and AGENTS bytes outside the
  managed block remain unchanged.
- Machine protocol identifiers, JSON fields, command names, configuration keys,
  status values, error codes, hashes, and audit operation names are never
  translated.
- Runtime safety continues to be enforced by Go code and tests. Text assets explain
  rules but do not replace validators, CAS, P-23, atomic writes, or approval gates.

Texts intentionally retained in Go:

- json/jsonschema struct tags remain beside their Go request fields because the
  MCP SDK generates the machine schema from those compile-time tags. They are
  short schema annotations, not independently rendered Prompt documents.
- Machine status constants, JSON field names, protocol envelopes, counters, and
  state transitions remain in typed Go structures. Code is their authoritative
  source; manifests protect any corresponding tokens mentioned by resources.
- Dynamic findings, structured fields, paths, safety decisions, permissions,
  validation outcomes, CAS facts, and write counts remain program-generated because
  they depend on current runtime facts. User-visible natural-language shells around
  those facts are loaded from the selected Locale resources.
- Keyed short runtime messages use contracts/ui/messages. Missing keys, unknown
  Locale values, malformed bundles, and argument-count or format-signature drift
  are errors; production code does not fall back to source literals or another
  Locale.
- Tests evaluate production Go string expressions through the AST, including
  split constant concatenation and strings.Join. Complete long assets are rejected;
  short action text is rejected only when several members of one stable family
  reappear together, so an ordinary single phrase does not create noise.
- Manifest consumers are checked against real production paths and symbols, and
  each symbol must reference the matching resource ID (directly or through the
  typed task-action ID binding).
