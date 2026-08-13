# AOCI connector patch set

This module is based on the official
`gitcode.com/opengauss/openGauss-connector-go-pq` v1.0.8 source release. AOCI
keeps the upstream module path and legacy `ParseConfig` behavior while applying
the following narrowly scoped production hardening:

- reserve process stdout for the host protocol and send the default connector
  logger to stderr;
- perform TLS query cancellation on the dedicated cancel socket without
  closing or upgrading the main connection;
- provide `ParseConfigStrict`, an opt-in deterministic configuration entrypoint
  that does not consume ambient libpq or home-directory configuration and does
  not permit TLS downgrade or multi-host fallback, rejects unknown parameters,
  requires an absolute explicit CA path when supplied, and enforces TLS 1.2 or
  later;
- keep dial, TLS negotiation, authentication, and startup reads within the
  caller's connection context; and
- reject truncated SHA256/SM3 authentication challenges and server-controlled
  PBKDF2 iteration counts outside `1..1,000,000` before parsing or computation.

The upstream `LICENSE.md` remains authoritative for this vendored module.
