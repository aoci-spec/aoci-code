# AOCI connector patch set

This module is based on the official
`gitcode.com/opengauss/openGauss-connector-go-pq` v1.0.8 source release. AOCI
keeps the upstream module path and legacy `ParseConfig` behavior while applying
the following narrowly scoped production hardening:

- reserve process stdout for the host protocol and send the default connector
  logger to stderr;
- perform TLS query cancellation on the dedicated cancel socket without
  closing or upgrading the main connection, and when that CancelRequest
  cannot reach the server, end a read still blocked on the main connection
  with a deadline so the query honors its context;
- provide `ParseConfigStrict`, an opt-in deterministic configuration entrypoint
  that does not consume ambient libpq or home-directory configuration and does
  not permit TLS downgrade or multi-host fallback, rejects unknown parameters,
  requires an absolute explicit CA path when supplied, and enforces TLS 1.2 or
  later;
- keep dial, TLS negotiation, authentication, and startup reads within the
  caller's connection context; and
- reject truncated SHA256/SM3 authentication challenges and server-controlled
  PBKDF2 iteration counts outside `1..1,000,000` before parsing or computation;
  and
- refuse a backend message whose length is below its own header or above the
  server's 1 GiB allocation limit as a driver error, instead of panicking on
  a negative slice bound or allocating whatever the frame names;
- refuse a connection string whose quoted value ends in a backslash instead of
  stepping past the end of the input, which panicked in every parser entry
  point (found by fuzzing `ParseConfigStrict`).

The upstream `LICENSE.md` remains authoritative for this vendored module.
