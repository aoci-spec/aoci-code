#!/usr/bin/env bash
# Validate the public FSL identity and unresolved legal metadata, then audit
# every reachable external Go package after go mod verify.
set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "${repo_root}"

module_path="github.com/aoci-spec/aoci-code"
go_bin="${GO_BIN:-go}"
if [[ "${go_bin}" == */* ]]; then
  go_bin_path="${go_bin}"
else
  go_bin_path="$(command -v "${go_bin}" || true)"
fi
if [ ! -x "${go_bin_path}" ]; then
  echo "[check-licenses] Go executable not found: ${go_bin}" >&2
  exit 1
fi
# go-licenses resolves `go` through PATH internally. Bind it to the same
# toolchain selected by GO_BIN so local and CI results use one Go identity.
export PATH="$(dirname -- "${go_bin_path}"):${PATH}"

legal_findings=0
dependency_findings=0

# go mod verify does not cover a local replacement. Bind the license audit to
# the checked-in patched tree, its exact upstream MIT text, and its provenance
# checksums before enumerating reachable packages. Full Confidence separately
# performs the networked upstream download and complete patch replay.
if ! GO_BIN="${go_bin_path}" bash scripts/check-opengauss-connector.sh --local-only; then
  echo "[check-licenses] local openGauss Connector provenance check failed" >&2
  exit 1
fi

file_sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
    return
  fi
  echo "[check-licenses] no SHA-256 utility is available" >&2
  return 1
}

require_marker() {
  local file_path="$1"
  local marker="$2"
  if [ ! -f "${file_path}" ]; then
    echo "[check-licenses] missing public license metadata file: ${file_path}" >&2
    legal_findings=$((legal_findings + 1))
    return
  fi
  if ! grep -Fqx -- "${marker}" "${file_path}"; then
    echo "[check-licenses] FSL-1.1-MIT metadata mismatch: ${file_path} lacks exact line: ${marker}" >&2
    legal_findings=$((legal_findings + 1))
  fi
}

require_contains() {
  local file_path="$1"
  local marker="$2"
  if [ ! -f "${file_path}" ]; then
    echo "[check-licenses] missing public license metadata file: ${file_path}" >&2
    legal_findings=$((legal_findings + 1))
    return
  fi
  if ! grep -Fq -- "${marker}" "${file_path}"; then
    echo "[check-licenses] FSL-1.1-MIT metadata mismatch: ${file_path} lacks: ${marker}" >&2
    legal_findings=$((legal_findings + 1))
  fi
}

require_marker LICENSE "# Functional Source License, Version 1.1, MIT Future License"
require_marker LICENSE "FSL-1.1-MIT"
require_marker NOTICE "License: Functional Source License, Version 1.1, MIT Future License"
require_marker NOTICE "License identifier: FSL-1.1-MIT"
require_contains README.md "FSL-1.1-MIT"
require_contains README.zh-CN.md "FSL-1.1-MIT"

legal_assets=(LICENSE NOTICE PATENTS TRADEMARKS THIRD-PARTY-NOTICES)
for legal_asset in "${legal_assets[@]}"; do
  if [ ! -f "${legal_asset}" ]; then
    echo "[check-licenses] missing required legal asset: ${legal_asset}" >&2
    legal_findings=$((legal_findings + 1))
  fi
done

# Placeholders are deliberately explicit during legal preparation, but none
# may remain in a distributable release. Report every unresolved field without
# guessing or rewriting its value.
while IFS= read -r unresolved_placeholder; do
  echo "[check-licenses] unresolved legal metadata: ${unresolved_placeholder}" >&2
  legal_findings=$((legal_findings + 1))
done < <(grep -nH -o -E 'PLACEHOLDER_[A-Z0-9_]+' "${legal_assets[@]}" 2>/dev/null || true)

# Keep the audited target union locked to the release build matrix. A matrix
# change must update this gate in the same review instead of silently dropping
# a platform-specific dependency from the inventory.
if ! grep -Fqx -- '    goos: [linux, darwin, windows]' .goreleaser.yml ||
  ! grep -Fqx -- '    goarch: [amd64, arm64]' .goreleaser.yml ||
  ! grep -Fqx -- '      - CGO_ENABLED=0' .goreleaser.yml; then
  echo "[check-licenses] release target matrix differs from the audited license matrix" >&2
  dependency_findings=$((dependency_findings + 1))
fi
release_targets=(
  linux/amd64
  linux/arm64
  darwin/amd64
  darwin/arm64
  windows/amd64
  windows/arm64
)

license_tool="${GO_LICENSES:-$("${go_bin_path}" env GOPATH)/bin/go-licenses}"
if [ ! -x "${license_tool}" ]; then
  echo "[check-licenses] missing pinned go-licenses tool" >&2
  exit 1
fi

# Populate the complete locked module graph before the target-specific audit
# intentionally disables module lookup. Linux-only vet/list steps do not fetch
# Windows-only dependencies such as Cobra's mousetrap package on a fresh runner.
# -mod=readonly keeps this preparation from changing the declared module graph.
readonly_go_flags="${GOFLAGS:+${GOFLAGS} }-mod=readonly"
if ! env \
  GOTOOLCHAIN=local \
  GOWORK=off \
  GOFLAGS="${readonly_go_flags}" \
  "${go_bin_path}" mod download; then
  echo "[check-licenses] failed to prefetch the locked module graph" >&2
  exit 1
fi
if ! env \
  GOTOOLCHAIN=local \
  GOWORK=off \
  GOPROXY=off \
  GOFLAGS="${readonly_go_flags}" \
  "${go_bin_path}" mod verify; then
  echo "[check-licenses] prefetched module graph failed offline verification" >&2
  exit 1
fi

raw_report_path="$(mktemp)"
report_path="$(mktemp)"
target_report_path="$(mktemp)"
diagnostic_path="$(mktemp)"
raw_module_inventory_path="$(mktemp)"
module_inventory_path="$(mktemp)"
target_module_inventory_path="$(mktemp)"
notice_inventory_path="$(mktemp)"
trap 'rm -f "${raw_report_path}" "${report_path}" "${target_report_path}" "${diagnostic_path}" "${raw_module_inventory_path}" "${module_inventory_path}" "${target_module_inventory_path}" "${notice_inventory_path}"' EXIT
for release_target in "${release_targets[@]}"; do
  IFS=/ read -r target_os target_arch <<<"${release_target}"
  if ! env \
    CGO_ENABLED=0 \
    GOOS="${target_os}" \
    GOARCH="${target_arch}" \
    GOTOOLCHAIN=local \
    GOWORK=off \
    GOPROXY=off \
    GOFLAGS="${readonly_go_flags}" \
    "${license_tool}" report ./... >"${target_report_path}" 2>"${diagnostic_path}"; then
    echo "[check-licenses] dependency inventory failed for ${release_target}" >&2
    cat "${diagnostic_path}" >&2
    exit 1
  fi
  if [ ! -s "${target_report_path}" ]; then
    echo "[check-licenses] no dependency rows were reported for ${release_target}" >&2
    dependency_findings=$((dependency_findings + 1))
    continue
  fi
  cat "${target_report_path}" >>"${raw_report_path}"

  if ! env \
    CGO_ENABLED=0 \
    GOOS="${target_os}" \
    GOARCH="${target_arch}" \
    GOTOOLCHAIN=local \
    GOWORK=off \
    GOPROXY=off \
    GOFLAGS="${readonly_go_flags}" \
    "${go_bin_path}" list -deps \
      -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}}{{end}}{{end}}' \
      ./cmd/aoci >"${target_module_inventory_path}" 2>"${diagnostic_path}"; then
    echo "[check-licenses] release module inventory failed for ${release_target}" >&2
    cat "${diagnostic_path}" >&2
    exit 1
  fi
  sed '/^[[:space:]]*$/d' "${target_module_inventory_path}" >>"${raw_module_inventory_path}"
done
LC_ALL=C sort -u "${raw_report_path}" >"${report_path}"
LC_ALL=C sort -u "${raw_module_inventory_path}" >"${module_inventory_path}"

# go-licenses v1.6.0 identifies the locally replaced connector's unchanged MIT
# text but reports a blank or Unknown license URL because the module uses a
# local replacement and the upstream file is named LICENSE.md. Keep this a
# narrow, provenance- and content-bound exception instead of accepting an
# arbitrary missing source.
opengauss_connector_module="gitcode.com/opengauss/openGauss-connector-go-pq"
opengauss_connector_version="v1.0.8"
opengauss_connector_license_sha256="3e2d79d27727d59ab1c9752f57654733d6c8824936c22800594dccfe8864ec28"
opengauss_connector_record="$(env \
  GOTOOLCHAIN=local \
  GOWORK=off \
  GOPROXY=off \
  GOFLAGS="${readonly_go_flags}" \
  "${go_bin_path}" list -m -f '{{.Dir}} {{.Version}}' \
  "${opengauss_connector_module}" 2>/dev/null || true)"
opengauss_connector_dir="${opengauss_connector_record% *}"
opengauss_connector_resolved_version="${opengauss_connector_record##* }"
opengauss_connector_license_verified=false
if [ -z "${opengauss_connector_dir}" ] ||
  [ "${opengauss_connector_resolved_version}" != "${opengauss_connector_version}" ] ||
  [ "${opengauss_connector_dir}" != "${repo_root}/third_party/openGauss-connector-go-pq" ] ||
  [ ! -f "${opengauss_connector_dir}/LICENSE.md" ] ||
  [ "$(file_sha256 "${opengauss_connector_dir}/LICENSE.md")" != "${opengauss_connector_license_sha256}" ]; then
  echo "[check-licenses] pinned openGauss Connector MIT license evidence is missing or changed" >&2
  dependency_findings=$((dependency_findings + 1))
else
  opengauss_connector_license_verified=true
fi

# The distributable notice must describe exactly the modules reachable from
# the release binary matrix. A detector result without a matching notice is
# not sufficient release metadata, and stale extra inventory rows are also
# rejected so removals receive the same review as additions.
awk '
  NF == 2 &&
  $1 ~ /^(filippo\.io|gitcode\.com|github\.com|golang\.org)\// &&
  $2 ~ /^v[0-9]/ { print $1 " " $2 }
' THIRD-PARTY-NOTICES | LC_ALL=C sort -u >"${notice_inventory_path}"
if ! cmp -s "${module_inventory_path}" "${notice_inventory_path}"; then
  echo "[check-licenses] THIRD-PARTY-NOTICES does not match the release-matrix module inventory" >&2
  diff -u "${module_inventory_path}" "${notice_inventory_path}" >&2 || true
  dependency_findings=$((dependency_findings + 1))
fi

external_count=0
while IFS=, read -r package_path license_url license_name; do
  case "${package_path}" in
    "${module_path}"|"${module_path}"/*) continue ;;
  esac
  external_count=$((external_count + 1))

  normalized_license_name="$(printf '%s' "${license_name}" | tr '[:lower:]' '[:upper:]')"
  normalized_license_url="$(printf '%s' "${license_url}" | tr '[:lower:]' '[:upper:]')"
  if [ "${package_path}" = "${opengauss_connector_module}" ] &&
    [ "${normalized_license_name}" = "MIT" ] &&
    { [ -z "${normalized_license_url}" ] || [ "${normalized_license_url}" = "UNKNOWN" ]; } &&
    [ "${opengauss_connector_license_verified}" = true ]; then
    normalized_license_url="LOCAL-PATCHED-MODULE-LICENSE.MD-PROVENANCE-VERIFIED"
  fi
  case "${normalized_license_name}" in
    ""|UNKNOWN|NOASSERTION|NONE|UNLICENSED|PROPRIETARY)
      echo "[check-licenses] invalid external license state: ${package_path} (${license_name:-missing name})" >&2
      dependency_findings=$((dependency_findings + 1))
      continue
      ;;
  esac
  case "${normalized_license_url}" in
    ""|UNKNOWN|NOASSERTION|NONE)
      echo "[check-licenses] invalid external license source: ${package_path} (${license_url:-missing URL})" >&2
      dependency_findings=$((dependency_findings + 1))
      continue
      ;;
  esac
  case "${normalized_license_name}" in
    AGPL-*|GPL-*|SSPL-*)
      echo "[check-licenses] strong-copyleft dependency requires review: ${package_path} (${license_name})" >&2
      dependency_findings=$((dependency_findings + 1))
      ;;
  esac
done <"${report_path}"

if [ "${external_count}" -eq 0 ]; then
  echo "[check-licenses] no reachable external packages were reported" >&2
  dependency_findings=$((dependency_findings + 1))
fi
if [ "${legal_findings}" -ne 0 ] || [ "${dependency_findings}" -ne 0 ]; then
  echo "[check-licenses] failed: ${legal_findings} public legal metadata finding(s), ${dependency_findings} external dependency license finding(s) across ${external_count} release-matrix row(s)" >&2
  exit 1
fi
echo "[check-licenses] passed: FSL-1.1-MIT metadata is consistent and ${external_count} release-matrix external package rows have identified licenses with no configured invalid state"
