#!/usr/bin/env bash
# Reproduce and verify AOCI's reviewed openGauss Connector v1.0.8 patch set.
set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "${repo_root}"

module="gitcode.com/opengauss/openGauss-connector-go-pq"
version="v1.0.8"
upstream_commit="1b2ca1e407de54157316fcb4a808d8a87e007dac"
module_sum="h1:QQBQgXTOx7UP4krmxjBGTk/Sm4lh98GW3nRWkvxBBn4="
gomod_sum="h1:EIVrn+q7Ip07RUWAQxg6ELVeeY3+TjgAytV+WUIyHTs="
license_sha256="3e2d79d27727d59ab1c9752f57654733d6c8824936c22800594dccfe8864ec28"
local_module="third_party/openGauss-connector-go-pq"
patch_file="third_party/patches/openGauss-connector-go-pq-v1.0.8-aoci.patch"
provenance_file="third_party/openGauss-connector-go-pq.PROVENANCE.md"
go_bin="${GO_BIN:-go}"

fail() {
  echo "[check-opengauss-connector] $*" >&2
  exit 1
}

file_sha256() {
  sha256sum "$1" | awk '{print $1}'
}

tree_sha256() {
  local tree="$1"
  (
    cd "${tree}"
    find . -type f -print0 |
      LC_ALL=C sort -z |
      xargs -0 -r sha256sum
  ) | sha256sum | awk '{print $1}'
}

require_provenance_line() {
  local line="$1"
  grep -Fqx -- "${line}" "${provenance_file}" ||
    fail "provenance is missing or stale: ${line}"
}

command -v sha256sum >/dev/null 2>&1 || fail "sha256sum is required"
command -v git >/dev/null 2>&1 || fail "git is required"
if [[ "${go_bin}" == */* ]]; then
  [ -x "${go_bin}" ] || fail "Go executable not found: ${go_bin}"
else
  command -v "${go_bin}" >/dev/null 2>&1 || fail "Go executable not found: ${go_bin}"
fi

[ -d "${local_module}" ] || fail "missing local patched module: ${local_module}"
[ -f "${patch_file}" ] || fail "missing canonical patch: ${patch_file}"
[ -f "${provenance_file}" ] || fail "missing provenance record: ${provenance_file}"
[ -f "${local_module}/LICENSE.md" ] || fail "missing local upstream LICENSE.md"
[ "$(file_sha256 "${local_module}/LICENSE.md")" = "${license_sha256}" ] ||
  fail "local upstream LICENSE.md hash changed"
[ "$(sed -n '1p' "${local_module}/go.mod")" = "module ${module}" ] ||
  fail "local module identity changed"

require_line="$(printf '\t%s %s' "${module}" "${version}")"
grep -Fqx -- "${require_line}" go.mod ||
  fail "go.mod must require ${module} ${version} directly"
grep -Fqx -- "replace ${module} => ./${local_module}" go.mod ||
  fail "go.mod must replace the connector with the reviewed local module"

if find "${local_module}" -mindepth 1 ! -type d ! -type f -print -quit | grep -q .; then
  fail "local patched module contains a symlink or non-regular object"
fi
if find "${local_module}" -type d -empty -print -quit | grep -q .; then
  fail "local patched module contains an empty directory that the patch cannot reproduce"
fi

local_tree_sha256="$(tree_sha256 "${local_module}")"
patch_sha256="$(file_sha256 "${patch_file}")"
require_provenance_line "- Patched tree manifest SHA-256: \`${local_tree_sha256}\`"
require_provenance_line "- Canonical patch SHA-256: \`${patch_sha256}\`"
require_provenance_line "- Upstream LICENSE.md SHA-256: \`${license_sha256}\`"
require_provenance_line "- Upstream module sum: \`${module_sum}\`"
require_provenance_line "- Upstream go.mod sum: \`${gomod_sum}\`"

if [ "${1:-}" = "--local-only" ]; then
  echo "[check-opengauss-connector] local patched module, license, checksums, and replacement are consistent"
  exit 0
fi
if [ "$#" -ne 0 ]; then
  fail "usage: $0 [--local-only]"
fi

download_json="$({
  GOTOOLCHAIN=local GOWORK=off "${go_bin}" mod download -json "${module}@${version}"
} 2>&1)" || fail "could not download the pinned upstream module: ${download_json}"

printf '%s\n' "${download_json}" | grep -Fq '"Version": "v1.0.8"' ||
  fail "downloaded module version does not match ${version}"
printf '%s\n' "${download_json}" | grep -Fq "\"Sum\": \"${module_sum}\"" ||
  fail "downloaded module checksum does not match the reviewed sum"
printf '%s\n' "${download_json}" | grep -Fq "\"GoModSum\": \"${gomod_sum}\"" ||
  fail "downloaded go.mod checksum does not match the reviewed sum"
printf '%s\n' "${download_json}" | grep -Fq '"URL": "https://gitcode.com/opengauss/openGauss-connector-go-pq.git"' ||
  fail "downloaded module origin URL is not the reviewed upstream"
printf '%s\n' "${download_json}" | grep -Fq "\"Hash\": \"${upstream_commit}\"" ||
  fail "downloaded module origin commit is not the reviewed tag commit"
printf '%s\n' "${download_json}" | grep -Fq '"Ref": "refs/tags/v1.0.8"' ||
  fail "downloaded module origin is not refs/tags/v1.0.8"

upstream_dir="$(printf '%s\n' "${download_json}" | sed -n 's/^[[:space:]]*"Dir": "\(.*\)",$/\1/p')"
[ -d "${upstream_dir}" ] || fail "download JSON did not identify an extracted upstream directory"
[ "$(file_sha256 "${upstream_dir}/LICENSE.md")" = "${license_sha256}" ] ||
  fail "downloaded upstream LICENSE.md hash changed"

upstream_tree_sha256="$(tree_sha256 "${upstream_dir}")"
require_provenance_line "- Upstream module tree manifest SHA-256: \`${upstream_tree_sha256}\`"
require_provenance_line "- Upstream tag commit: \`${upstream_commit}\`"

temp_root="$(mktemp -d)"
trap 'rm -rf -- "${temp_root}"' EXIT
replay_dir="${temp_root}/replay"
cp -a -- "${upstream_dir}" "${replay_dir}"
chmod -R u+w "${replay_dir}"
(
  cd "${replay_dir}"
  git apply --check "${repo_root}/${patch_file}"
  git apply "${repo_root}/${patch_file}"
)

if ! diff -qr --no-dereference "${replay_dir}" "${local_module}"; then
  fail "applying the canonical patch did not reproduce the checked-in connector tree byte-for-byte"
fi
replay_tree_sha256="$(tree_sha256 "${replay_dir}")"
[ "${replay_tree_sha256}" = "${local_tree_sha256}" ] ||
  fail "replayed connector tree hash does not match the checked-in tree"

echo "[check-opengauss-connector] verified ${module} ${version} (${upstream_commit}), replayed patch, and matched the complete local tree"
