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
    GOFLAGS="${GOFLAGS:+${GOFLAGS} }-mod=readonly" \
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
    GOFLAGS="${GOFLAGS:+${GOFLAGS} }-mod=readonly" \
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

# The distributable notice must describe exactly the modules reachable from
# the release binary matrix. A detector result without a matching notice is
# not sufficient release metadata, and stale extra inventory rows are also
# rejected so removals receive the same review as additions.
awk '
  NF == 2 &&
  $1 ~ /^(filippo\.io|github\.com|golang\.org)\// &&
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
