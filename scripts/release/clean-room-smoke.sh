#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

go_bin=${GO_BIN:-}
if [ -z "$go_bin" ]; then
  go_bin=$(command -v go 2>/dev/null || true)
fi
if [ -z "$go_bin" ] && [ -x /usr/local/go/bin/go ]; then
  go_bin=/usr/local/go/bin/go
fi
if [ -z "$go_bin" ] || { [ ! -x "$go_bin" ] && ! command -v "$go_bin" >/dev/null 2>&1; }; then
  echo "go is required for clean-room smoke" >&2
  exit 1
fi

SMOKE_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/aoci-clean-room.XXXXXX")
case "$SMOKE_ROOT" in
  "${TMPDIR:-/tmp}"/aoci-clean-room.*) ;;
  *) echo "unexpected temporary directory: $SMOKE_ROOT" >&2; exit 1 ;;
esac
cleanup() {
  rm -rf -- "$SMOKE_ROOT"
}
trap cleanup EXIT

commit=$(git rev-parse HEAD)
build_date=$(git show -s --format=%cI HEAD)
binary="$SMOKE_ROOT/aoci"
repository="$SMOKE_ROOT/repository"

CGO_ENABLED=0 "$go_bin" build -trimpath \
  -ldflags "-s -w -buildid= -X github.com/aoci-spec/aoci-code/internal/cli.version=clean-room -X github.com/aoci-spec/aoci-code/internal/cli.commit=$commit -X github.com/aoci-spec/aoci-code/internal/cli.buildDate=$build_date" \
  -o "$binary" ./cmd/aoci

mkdir -p "$repository"
cp -R examples/minimal-repository/. "$repository/"
mkdir -p "$repository/tests/fixtures"
printf '{}\n' >"$repository/tests/fixtures/case.json"
git -C "$repository" init --quiet
git -C "$repository" config user.name "AOCI Release Rehearsal"
git -C "$repository" config user.email "release-rehearsal@example.invalid"
git -C "$repository" add .
git -C "$repository" commit --quiet -m "initial fixture"

"$binary" --version
"$binary" --repo "$repository" init --locale en-US --agent codex
grep -qx '#AOCI-ROOT-MANIFEST: 1' "$repository/aoci.txt"
grep -qx '#AOCI-META-VOLUME: 1' "$repository/aoci.meta.txt"
grep -qx '#AOCI-CODE-VOLUME: 1' "$repository/aoci.code.txt"
if [ "$(wc -l <"$repository/aoci.code.txt")" -ne 1 ]; then
  echo "fresh Volume-first Code must start with zero Entries" >&2
  exit 1
fi
if [ -e "$repository/aoci.database.txt" ]; then
  echo "fresh Volume-first Database must remain absent" >&2
  exit 1
fi
if [ -e "$repository/.aoci/onboarding/active.json" ]; then
  echo "fresh Volume-first init unexpectedly started Legacy onboarding" >&2
  exit 1
fi
"$binary" --repo "$repository" scan
capabilities=$("$binary" --repo "$repository" --json capabilities)
case "$capabilities" in
  *'"mcp_tool_count": 9'*'"current_layout": "volumes-v1"'*) ;;
  *) echo "capabilities did not preserve the Volume-first nine-tool contract" >&2; exit 1 ;;
esac
echo "$capabilities"
database_status=$("$binary" --repo "$repository" --json database cognition status)
case "$database_status" in
  *'"database_volume_state": "absent"'*'"cognition_current": true'*'"next_action": "no_database_configuration"'*) ;;
  *) echo "absent Database was not reported as a legal current state" >&2; exit 1 ;;
esac
echo "$database_status"
source_manifest=$("$binary" --repo "$repository" --json source manifest)
case "$source_manifest" in
  *'"path": "tests/fixtures/case.json"'*) echo "excluded fixture entered Business Source evidence" >&2; exit 1 ;;
esac
echo "$source_manifest"
"$binary" --repo "$repository" --json scope show
"$binary" --repo "$repository" --json scope status
"$binary" --repo "$repository" --json scope budget show
cp "$repository/aoci.txt" "$SMOKE_ROOT/index-before-observe-review.txt"
printf '\n// release rehearsal observe evidence change\n' >>"$repository/counter/counter_test.go"
set +e
observe_verify=$("$binary" --repo "$repository" --json verify)
observe_verify_exit=$?
set -e
if [ "$observe_verify_exit" -ne 1 ]; then
  echo "observe Verify returned unexpected exit code: $observe_verify_exit" >&2
  exit 1
fi
case "$observe_verify" in
  *'"layout_mode": "volumes-v1"'*'"structure_valid": true'*'"governance_aligned": false'*'"code": "observed_pending"'*) ;;
  *) echo "observe Verify did not report the expected governance blocker" >&2; exit 1 ;;
esac
echo "$observe_verify"
observe_status=$("$binary" --repo "$repository" --json scope status)
case "$observe_status" in
  *'"stage": "observed_evidence_review_required"'*) ;;
  *) echo "observe test change was not detected" >&2; exit 1 ;;
esac
observe_apply=$("$binary" --repo "$repository" --json scope acknowledge --reviewed-by release-rehearsal)
case "$observe_apply" in
  *'"authorization_mechanism": "policy_bound_auto"'*) ;;
  *) echo "Auto observe review omitted policy-bound authorization" >&2; exit 1 ;;
esac
cmp "$SMOKE_ROOT/index-before-observe-review.txt" "$repository/aoci.txt"
echo "$observe_apply"
post_observe_status=$("$binary" --repo "$repository" --json scope status)
case "$post_observe_status" in
  *'"stage": "authoring_required"'*'"authoring_targets": 8'*) ;;
  *) echo "observe acknowledgement washed indexed authoring debt" >&2; exit 1 ;;
esac
echo "$post_observe_status"
guide=$("$binary" --repo "$repository" --json index agent guide --agent codex)
case "$guide" in
  *'"stage": "authoring_required"'*'"next_action": "call_no_argument_aoci_maintain_for_current_machine_batch"'*'"database_volume_state": "absent"'*'"max_entries": 200'*'"included": 5'*'"remaining": 0'*) ;;
  *) echo "Guide did not return the unique current Volume-first authoring action" >&2; exit 1 ;;
esac
case "$guide" in
  *'counter/counter_test.go'*) echo "observe test became a formal authoring target" >&2; exit 1 ;;
esac
echo "$guide"
if [ -e "$repository/.aoci/onboarding/active.json" ]; then
  echo "fresh Volume-first lifecycle created a Legacy onboarding Session" >&2
  exit 1
fi
status=$("$binary" --repo "$repository" --json status)
case "$status" in
  *'"index_entries": 0'*'"baseline_exists": true'*'"layout_mode": "volumes-v1"'*'"database": "absent"'*) ;;
  *) echo "status did not report the current fresh Volume-first phase" >&2; exit 1 ;;
esac
echo "$status"
doctor=$("$binary" --repo "$repository" --json doctor)
case "$doctor" in
  *'Index parsing: 2 sections / 0 Entries / 0 parse warnings'*'Diagnostics complete: all checks passed.'*) ;;
  *) echo "doctor did not report a healthy zero-Entry Volume-first repository" >&2; exit 1 ;;
esac
echo "$doctor"
