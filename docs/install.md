# AOCI-CODE Installation

## AOCI-CODE v0.1.0-rc1 source build

The current release candidate is built from the canonical source using the Go
version declared in `go.mod` and `make`. From a source checkout of
`github.com/aoci-spec/aoci-code`, run:

```bash
go mod verify
mkdir -p build
make build
./build/aoci --version
```

The build produces `build/aoci` on POSIX hosts and `build/aoci.exe` on Windows. Copy it to a stable absolute path owned by the current user or administrator, then configure hosts to call that exact path.

## GitHub Releases after public release

GitHub Releases will provide verified packages only after the public release.
This release-candidate documentation does not claim a downloadable package or
provide a release URL.

A public package must include a platform archive, `SHA256SUMS`, an SBOM, a release artifact manifest, a signature, and provenance. Before executing a downloaded binary:

1. verify the checksum file's publisher identity using the documented signature policy;
2. verify the archive against `SHA256SUMS`;
3. inspect the release artifact manifest and SBOM as required by local policy;
4. run `aoci --version` and retain the binary SHA-256 in operational or experiment records.

Checksums alone detect corruption but do not establish who published an artifact.

## Host configuration

Use an absolute binary path in MCP configuration. This avoids accidentally selecting a different `aoci` from `PATH` after an upgrade. Run:

```bash
aoci --repo /path/to/repository doctor
```

Use `doctor --net` only when an intentional endpoint connectivity test is acceptable.
