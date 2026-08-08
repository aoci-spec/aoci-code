# Security Policy

## Supported versions

Security fixes are evaluated for the latest maintained AOCI-CODE version and
the current default branch. Older development snapshots are not maintained as
separate security lines unless a release notice says otherwise.

## Reporting a vulnerability

Use GitHub private vulnerability reporting for the canonical repository:

https://github.com/aoci-spec/aoci-code/security/advisories/new

Do not disclose an exploitable vulnerability, credential, private endpoint,
proprietary source, or personal data in a public issue. If private reporting is
temporarily unavailable, open a public issue containing only a request for a
confidential contact; do not include sensitive details.

A useful report includes:

- affected version, commit, binary SHA-256, operating system, and architecture;
- the affected boundary, such as path containment, MCP stdout isolation, secret
  handling, CAS, atomic writes, archive extraction, or update verification;
- minimal reproduction steps and expected impact;
- whether the issue is public or actively exploited;
- evidence with secrets and third-party source removed.

Maintainers will acknowledge the report, assess affected versions, coordinate
remediation and disclosure, and publish guidance when disclosure is safe.

## Security boundaries

- MCP stdio stdout is reserved for JSON-RPC; logs and diagnostics use stderr.
- Configuration stores credential environment-variable names, not values.
- Optional model access uses only an explicitly configured endpoint.
- Formal asset updates use validation, locking, CAS, atomic writes, and recovery.
- Database Evidence collectors are limited to schema/catalog metadata and do
  not issue DDL or DML.
- Checksums establish artifact integrity but do not identify the publisher;
  release provenance and signing are separate controls.

Dependency and vulnerability scan findings require reachability and impact
review. Suppressions must be narrow, documented, and reviewed.
