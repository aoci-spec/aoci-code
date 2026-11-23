# openGauss Connector patch provenance

AOCI builds its openGauss database path from a complete, locally replaced copy
of the official `gitcode.com/opengauss/openGauss-connector-go-pq` v1.0.8 Go
module. The module path and upstream MIT license are unchanged. The local tree
is intentionally described as patched source, not as pristine upstream or an
official openGauss release artifact.

## Reviewed upstream identity

- Upstream repository: `https://gitcode.com/opengauss/openGauss-connector-go-pq.git`
- Upstream tag: `refs/tags/v1.0.8`
- Upstream tag commit: `1b2ca1e407de54157316fcb4a808d8a87e007dac`
- Upstream module sum: `h1:QQBQgXTOx7UP4krmxjBGTk/Sm4lh98GW3nRWkvxBBn4=`
- Upstream go.mod sum: `h1:EIVrn+q7Ip07RUWAQxg6ELVeeY3+TjgAytV+WUIyHTs=`
- Upstream LICENSE.md SHA-256: `3e2d79d27727d59ab1c9752f57654733d6c8824936c22800594dccfe8864ec28`
- Upstream module tree manifest SHA-256: `fb107e9c47effcd3efad47b88876e848e062ff8e423c92d2f54c07a91187ade9`

## AOCI patch identity

- Canonical patch: `third_party/patches/openGauss-connector-go-pq-v1.0.8-aoci.patch`
- Canonical patch SHA-256: `178d0d38217b2b4f9509ae4de275de9b7fedc90ca43adf4fbaf91fb281516b16`
- Checked-in patched tree: `third_party/openGauss-connector-go-pq`
- Patched tree manifest SHA-256: `17b6ce6eb03b04f10b3f316c74b8b4472d45b99cd88c3368e3d8b4aa48def98d`

The patch is limited to AOCI's production safety boundary: deterministic
allowlisted connection parsing without ambient files, strict TLS with no
remote downgrade and a TLS 1.2 minimum, caller-context bounds across dial/TLS/
authentication/startup, cancellation on its dedicated connection, stderr-only
default logging, and bounded/malformed authentication handling. Patch-owned
tests and `AOCI-PATCHES.md` are added to the local module. All other upstream
module files remain byte-identical to v1.0.8.

The tree-manifest SHA-256 is the SHA-256 of the byte stream produced by hashing
every regular file with `sha256sum`, in NUL-safe bytewise path order relative
to the applicable tree:

```bash
find . -type f -print0 | LC_ALL=C sort -z | xargs -0 -r sha256sum | sha256sum
```

## Reproduction gate

Run from the AOCI repository root:

```bash
bash scripts/check-opengauss-connector.sh
```

The gate obtains `module@v1.0.8` through the Go module command, checks the
origin URL, tag ref, commit, module and go.mod sums, and upstream license hash,
then applies the canonical patch to a fresh upstream tree. It compares that
replay with the complete checked-in module byte-for-byte and checks both tree
manifest hashes. It also requires the root `go.mod` to retain the exact v1.0.8
require plus the local `replace`. This check is necessary because `go mod
verify` verifies downloaded module-cache content but not the files behind a
local replacement.
