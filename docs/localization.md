# Localization

The project Locale controls production UI text, Host contracts, model prompts,
managed integration text, and the language requested for newly created or
genuinely updated Entries. The normative configuration and migration behavior
is [`aoci-config-runtime-v1.txt`](../spec/public/aoci-config-runtime-v1.txt).
Use the current CLI output for supported values and transaction state; this
guide does not duplicate those machine facts.

## Commands

```text
aoci init
aoci init --locale zh-CN
aoci config get locale
aoci config set locale en-US
aoci config set locale zh-CN
```

After changing Locale, restart each Host integration that still runs an older
AOCI MCP process. A new CLI invocation reads the project setting before it
renders command help or stable messages.

## Existing cognition

A Locale change does not bulk-translate existing Entries. Historical Entry
bytes remain valid; later Entries use the active project language when their
managed object is newly indexed or genuinely changes. Paths, protocol tokens,
status values, canonical object identities, and source-bound identifiers are
not translated.

For a planned Code change, write every new or updated target Entry in the
effective Root Locale. The target itself remains a Code Volume and does not get
its own Locale marker. An unchanged Entry is reused rather than translated only
because the project Locale changed.

Layout-specific marker, configuration, Baseline, migration, and Recovery steps
are performed by the governed command. Do not reproduce them with manual file
edits. If a switch stops, rerun only the action named by the current structured
result or Guide.

## Catalog integrity

Official Locale catalogs must remain structurally equivalent and complete; the
runtime never falls back to another Locale, a Golden, or a hard-coded prose
copy. Machine structure remains owned by schemas, validators, and
`internal/machinecontract`, while each Locale asset owns only its production
wording.
