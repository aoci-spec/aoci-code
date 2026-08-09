# AOCI-CODE Installation

## Build from source

Build the current canonical source checkout using the Go version declared in
`go.mod` and `make`. From a source checkout of
`github.com/aoci-spec/aoci-code`, run:

```bash
go mod verify
mkdir -p build
make build
./build/aoci --version
```

The build produces `build/aoci` on POSIX hosts and `build/aoci.exe` on Windows. Copy it to a stable absolute path owned by the current user or administrator, then configure hosts to call that exact path.

The signed Release binary reports `aoci version 0.1.0-rc1`. A source build
reports the version derived from its exact Git checkout, such as
`v0.1.0-rc1-1-g<short-commit>` after the Release tag, and reports that Git
commit separately. These are distinct build identities, not conflicting
versions.

## Signed GitHub Release packages

Published release-candidate packages are available from the
[v0.1.0-rc1 GitHub Release](https://github.com/aoci-spec/aoci-code/releases/tag/v0.1.0-rc1). The
`v0.1.0-rc1` release has exactly 16 uploaded assets:

- six archives: Linux, macOS, and Windows, each for amd64 and arm64;
- six SPDX JSON SBOMs, one named after each archive with `.sbom.json`
  appended;
- `SHA256SUMS` and `RELEASE-MANIFEST.json`;
- `SHA256SUMS.sigstore.json`, the keyless Cosign signature bundle for the
  original `SHA256SUMS` bytes; and
- `AOCI-CODE_v0.1.0-rc1.provenance.sigstore.json`, the GitHub build-provenance
  bundle.

The 12 archives and SBOMs are the checksum subjects. The 14 provenance subjects
are those 12 files plus `SHA256SUMS` and `RELEASE-MANIFEST.json`. The signature
bundle is not included in the file it signs, and the provenance bundle is not a
subject of its own attestation.

The following verification sequence requires GitHub CLI, Cosign, Git, GNU
`sha256sum`, and the Go version declared in `go.mod`. It pins the exact release
workflow identity rather than accepting an arbitrary workflow in the
repository:

GitHub CLI commands require an authenticated session. Run `gh auth login`
before the sequence below. For an anonymous download, use the
[tagged Release page](https://github.com/aoci-spec/aoci-code/releases/tag/v0.1.0-rc1)
in a browser to download all 16 uploaded assets;
the later `gh api` and `gh attestation verify` steps still require GitHub CLI
authentication.

```bash
gh auth login
```

```bash
REPOSITORY=aoci-spec/aoci-code
TAG=v0.1.0-rc1
VERSION=${TAG#v}
RELEASE_DIR="$PWD/aoci-code-$VERSION-release"
CERTIFICATE_IDENTITY="https://github.com/$REPOSITORY/.github/workflows/release.yml@refs/tags/$TAG"
OIDC_ISSUER=https://token.actions.githubusercontent.com

mkdir -p "$RELEASE_DIR"
gh release download "$TAG" --repo "$REPOSITORY" --dir "$RELEASE_DIR"
cd "$RELEASE_DIR"
```

First, require the declared 16-file Release asset inventory and verify the 12
checksum subjects:

```bash
CHECKSUM_SUBJECTS=(
  "aoci_${VERSION}_linux_amd64.tar.gz"
  "aoci_${VERSION}_linux_arm64.tar.gz"
  "aoci_${VERSION}_darwin_amd64.tar.gz"
  "aoci_${VERSION}_darwin_arm64.tar.gz"
  "aoci_${VERSION}_windows_amd64.zip"
  "aoci_${VERSION}_windows_arm64.zip"
  "aoci_${VERSION}_linux_amd64.tar.gz.sbom.json"
  "aoci_${VERSION}_linux_arm64.tar.gz.sbom.json"
  "aoci_${VERSION}_darwin_amd64.tar.gz.sbom.json"
  "aoci_${VERSION}_darwin_arm64.tar.gz.sbom.json"
  "aoci_${VERSION}_windows_amd64.zip.sbom.json"
  "aoci_${VERSION}_windows_arm64.zip.sbom.json"
)
RELEASE_ASSETS=(
  "${CHECKSUM_SUBJECTS[@]}"
  SHA256SUMS
  RELEASE-MANIFEST.json
  SHA256SUMS.sigstore.json
  "AOCI-CODE_${TAG}.provenance.sigstore.json"
)

test "${#RELEASE_ASSETS[@]}" -eq 16
test "$(find . -maxdepth 1 -type f | wc -l | tr -d ' ')" -eq 16
for asset in "${RELEASE_ASSETS[@]}"; do test -f "$asset"; done
test "$(wc -l < SHA256SUMS | tr -d ' ')" -eq 12
sha256sum -c SHA256SUMS
```

Second, verify that the exact `SHA256SUMS` bytes were signed by the Tag release
workflow through the GitHub Actions OIDC issuer. No long-lived signing key is
used:

```bash
cosign verify-blob \
  --bundle SHA256SUMS.sigstore.json \
  --certificate-identity "$CERTIFICATE_IDENTITY" \
  --certificate-oidc-issuer "$OIDC_ISSUER" \
  SHA256SUMS
```

Third, resolve the annotated Tag to its commit and verify all 14 provenance
subjects against the downloaded bundle. The exact certificate identity already
locks the repository, workflow path, and Tag ref; the remaining constraints
also bind the issuer, source ref, and Tag commit:

```bash
TAG_OBJECT=$(gh api "repos/$REPOSITORY/git/ref/tags/$TAG" --jq .object.sha)
TAG_COMMIT=$(gh api "repos/$REPOSITORY/git/tags/$TAG_OBJECT" --jq .object.sha)
PROVENANCE_SUBJECTS=(
  "${CHECKSUM_SUBJECTS[@]}"
  SHA256SUMS
  RELEASE-MANIFEST.json
)

for subject in "${PROVENANCE_SUBJECTS[@]}"; do
  gh attestation verify "$subject" \
    --bundle "AOCI-CODE_${TAG}.provenance.sigstore.json" \
    --repo "$REPOSITORY" \
    --cert-identity "$CERTIFICATE_IDENTITY" \
    --cert-oidc-issuer "$OIDC_ISSUER" \
    --signer-digest "$TAG_COMMIT" \
    --source-ref "refs/tags/$TAG" \
    --source-digest "$TAG_COMMIT" \
    --deny-self-hosted-runners
done
```

Fourth, inspect the SBOM corresponding to the archive you intend to use. For
example:

```bash
less "aoci_${VERSION}_linux_amd64.tar.gz.sbom.json"
```

Finally, fetch the verifier source at the same Tag, then verify the manifest and
the exact 16-file asset set. The verification command itself runs with
`GOPROXY=off` and makes no network request:

```bash
RELEASE_DIR=$PWD
SOURCE_DIR=$(mktemp -d)
git clone --quiet --depth 1 --branch "$TAG" \
  "https://github.com/$REPOSITORY.git" "$SOURCE_DIR"
(
  cd "$SOURCE_DIR"
  GOPROXY=off go run ./scripts/release/manifest \
    --verify "$RELEASE_DIR/RELEASE-MANIFEST.json"
)
```

After these checks, extract the selected archive, run `aoci --version`, and
retain the binary SHA-256 in operational or experiment records. Checksums detect
byte changes, keyless signature verification authenticates the checksum
publisher workload, and provenance binds subjects to the repository, workflow,
Tag, and commit. Artifact attestation does not guarantee that software is free
of vulnerabilities or otherwise absolutely safe.

## Host configuration

Use an absolute binary path in MCP configuration. This avoids accidentally selecting a different `aoci` from `PATH` after an upgrade. Run:

```bash
aoci --repo /path/to/repository doctor
```

Use `doctor --net` only when an intentional endpoint connectivity test is acceptable.
