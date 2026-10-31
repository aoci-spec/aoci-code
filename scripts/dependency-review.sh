#!/usr/bin/env bash
# Quarterly dependency review: a read-only report, never an upgrade.
#
# Compares what this repository pins against what upstream publishes:
#   1. the go directive in go.mod against the newest patch of that minor and
#      the newest stable minor on go.dev;
#   2. every direct Go module against the newest version the module proxy
#      lists (go list -m -u), the locally replaced connector excluded;
#   3. the openGauss connector tag and commit recorded in its PROVENANCE
#      against the tags upstream publishes, naming every upstream tag that
#      points at the pinned commit;
#   4. every GitHub Action pin (sha # vX.Y.Z) under .github/workflows against
#      the newest release tag, with the commit that tag names, and whether
#      the pinned sha is the commit of the version its comment claims;
#   5. the open dependabot proposals, when gh is installed and authenticated.
#
# Upgrading stays a supply-chain decision a person takes: a module bump
# changes the release license inventory (make licenses), a connector bump
# its provenance proof (make opengauss-connector), an Action bump the pins
# internal/safety compares across workflows. The ladder records this review
# under the quarterly contract dependency-review; redirect the output to the
# evidence file named in the record.
#
# Network: go.dev, the Go module proxy, gitcode.com and github.com, through
# the proxy the environment names (HTTPS_PROXY), read-only. Exit status: 0
# when every lookup answered, 1 when one did not, so an incomplete review is
# never recorded as a complete one; the dependabot section is skipped, not
# failed, without gh.

set -u
cd "$(dirname "$0")/.." || exit 1
GO_BIN="${GO_BIN:-go}"
failed=0
fail() { echo "  FAILED: $*"; failed=1; }

# newest_semver reads vX.Y.Z lines on stdin and prints the newest one.
newest_semver() {
  grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sed 's/^v//' | sort -t. -k1,1n -k2,2n -k3,3n | tail -n 1 | sed 's/^/v/'
}

# remote_tags prints "sha<TAB>tag" for every tag of a git remote, annotated
# tags peeled to the commit they name.
remote_tags() {
  git ls-remote --tags "$1" 2>/dev/null | awk '
    /\^\{\}$/ { sub(/\^\{\}$/, "", $2); peeled[$2] = $1; next }
    { sha[$2] = $1 }
    END { for (t in sha) { s = (t in peeled) ? peeled[t] : sha[t]; sub("refs/tags/", "", t); print s "\t" t } }'
}

echo "dependency review $(date -u +%Y-%m-%d) on $(git rev-parse --short HEAD 2>/dev/null || echo '?')"

echo
echo "1. Go toolchain"
go_directive="$(sed -n 's/^go \([0-9][0-9.]*\)$/\1/p' go.mod)"
go_minor="${go_directive%.*}"
echo "  go.mod go directive: ${go_directive}"
if releases="$(curl -fsS --max-time 60 'https://go.dev/dl/?mode=json')"; then
  printf '%s' "${releases}" | python3 -c '
import json, sys
minor = sys.argv[1]; pinned = "go" + sys.argv[2]
stable = [r["version"] for r in json.load(sys.stdin) if r.get("stable")]
same = [v for v in stable if v.startswith("go" + minor + ".") or v == "go" + minor]
newest_patch = same[0] if same else "(not listed: go.dev lists only the two newest minors)"
print("  newest %s.x on go.dev: %s%s" % (minor, newest_patch, " (current)" if newest_patch == pinned else " (a patch behind: bump the go directive and the base toolchain every machine installs)" if same else ""))
print("  newest stable: %s%s" % (stable[0], " (current)" if stable[0] == pinned else " (a newer minor exists; moving is a go.mod and toolchain decision)"))
' "${go_minor}" "${go_directive}" || fail "could not parse the go.dev release list"
else
  fail "go.dev release list unreachable"
fi

echo
echo "2. Direct Go modules (go list -m -u; the replaced connector is section 3)"
if ! "${GO_BIN}" list -m -u -f '{{if and (not .Indirect) (not .Main) (not .Replace)}}  {{.Path}} {{.Version}}{{with .Update}}  ->  {{.Version}} available{{else}}  (current){{end}}{{end}}' all 2>"${TMPDIR:-/tmp}/dependency-review.$$" | sed '/^$/d'; then
  fail "go list -m -u failed: $(tr '\n' ' ' <"${TMPDIR:-/tmp}/dependency-review.$$")"
fi
rm -f "${TMPDIR:-/tmp}/dependency-review.$$"
"${GO_BIN}" list -m -f '{{if and (not .Indirect) (not .Main) .Replace}}  {{.Path}} {{.Version}}  (replaced by {{.Replace.Path}}){{end}}' all 2>/dev/null | sed '/^$/d'
echo "  a module bump changes the release license inventory: run make licenses with it"

echo
echo "3. openGauss connector (third_party/openGauss-connector-go-pq.PROVENANCE.md)"
prov=third_party/openGauss-connector-go-pq.PROVENANCE.md
pinned_tag="$(sed -n 's/^- Upstream tag: `refs\/tags\/\([^`]*\)`.*/\1/p' "${prov}" | head -n 1)"
pinned_commit="$(sed -n 's/^- Upstream tag commit: `\([0-9a-f]*\)`.*/\1/p' "${prov}" | head -n 1)"
echo "  pinned: ${pinned_tag:-?} at ${pinned_commit:-?}"
if tags="$(remote_tags https://gitcode.com/opengauss/openGauss-connector-go-pq.git)" && [ -n "${tags}" ]; then
  newest="$(printf '%s\n' "${tags}" | cut -f2 | newest_semver)"
  newest_sha="$(printf '%s\n' "${tags}" | awk -F'\t' -v t="${newest}" '$2 == t { print $1 }')"
  same_commit="$(printf '%s\n' "${tags}" | awk -F'\t' -v c="${pinned_commit}" '$1 == c { print $2 }' | sort | tr '\n' ' ')"
  echo "  newest upstream tag: ${newest} at ${newest_sha}"
  echo "  upstream tags naming the pinned commit: ${same_commit:-none}"
  if [ "${newest_sha}" = "${pinned_commit}" ]; then
    echo "  the newest tag is the pinned commit (current)"
  else
    echo "  a newer commit is tagged upstream: a move needs the patch replayed and PROVENANCE, the patch and tree hashes, and THIRD-PARTY-NOTICES redone (make opengauss-connector)"
  fi
else
  fail "gitcode.com tag listing unreachable"
fi

echo
echo "4. GitHub Action pins (.github/workflows)"
grep -hoE 'uses: *[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+@[0-9a-f]{40} *# *v[0-9]+\.[0-9]+\.[0-9]+' .github/workflows/*.yml |
  sed -E 's/uses: *//; s/@/ /; s/ *# */ /' | sort -u |
  while read -r action pinned_sha pinned_version; do
    if ! tags="$(remote_tags "https://github.com/${action}")" || [ -z "${tags}" ]; then
      fail "${action}: github.com tag listing unreachable"
      continue
    fi
    newest="$(printf '%s\n' "${tags}" | cut -f2 | newest_semver)"
    newest_sha="$(printf '%s\n' "${tags}" | awk -F'\t' -v t="${newest}" '$2 == t { print $1 }')"
    claimed_sha="$(printf '%s\n' "${tags}" | awk -F'\t' -v t="${pinned_version}" '$2 == t { print $1 }')"
    state="current"
    [ "${newest}" != "${pinned_version}" ] && state="update available: ${newest} at ${newest_sha}"
    if [ -z "${claimed_sha}" ]; then
      state="${state}; the comment names ${pinned_version}, a tag upstream no longer lists"
    elif [ "${claimed_sha}" != "${pinned_sha}" ]; then
      state="${state}; PIN MISMATCH: ${pinned_version} is ${claimed_sha} upstream, not the pinned sha"
    fi
    echo "  ${action} ${pinned_version} (${pinned_sha:0:12})  ${state}"
  done
# The while loop ran in a subshell, so its failures are re-derived here.
if grep -hoE 'uses: *[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+@[0-9a-f]{40} *# *v[0-9]+\.[0-9]+\.[0-9]+' .github/workflows/*.yml | sed -E 's/uses: *//; s/@.*//' | sort -u |
  while read -r action; do git ls-remote --tags "https://github.com/${action}" >/dev/null 2>&1 || exit 1; done; then :; else failed=1; fi
echo "  every pin must stay equal across workflows (internal/safety); dependabot proposes the bumps, a person merges them"

echo
echo "5. Open dependabot proposals"
if command -v gh >/dev/null 2>&1; then
  if gh pr list --author app/dependabot --state open --json number,title,createdAt \
       --jq 'if length == 0 then "  none" else .[] | "  #\(.number) \(.createdAt[0:10]) \(.title)" end' 2>/dev/null; then :; else
    echo "  skipped: gh could not list pull requests (not authenticated, or no GitHub remote)"
  fi
else
  echo "  skipped: gh is not installed"
fi

echo
if [ "${failed}" -ne 0 ]; then
  echo "review incomplete: a lookup failed above; do not record it in the ladder"
  exit 1
fi
echo "review complete: record it with python3 scripts/blackbox/ladder.py --mark dependency-review --version <version> --evidence <this file>"
