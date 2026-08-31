#!/usr/bin/env python3
"""AOCI upgrade-axis harness — a repository written by a previously released
binary must stay governable by the binary under test.

升级轴回归（每个已发布版本 14 项检查）：用**旧的已发布二进制**建仓、扫描、授权到
aligned,再让被测二进制跑上去,断言身份不变、不索要 Scope Change、不改写正式资产。
两种 config 形状各跑一遍,因为它们解析的是不同的预算 preimage。

Why this suite exists at all: the other three suites build every fixture with the
binary under test, so a preimage that changed between versions is invisible to
them by construction. `cognitionbudget.LegacyPolicy` is the worked example — its
literals look like defaults but every repository without a `cognition_budget`
block re-resolves them and compares the result against a `budget_policy_identity`
already stamped into its Baseline. Change them and every such repository demands
a Scope Change from the upgrade alone. Nothing in conformance, scenarios, or
lifecycle can see that, because all three mint their Baseline with the new binary.

Two config shapes are required, and the second one is the whole point. Every
released `aoci init` writes an explicit `cognition_budget` block, so a fixture
built only by `init` never resolves LegacyPolicy at all and stays green while
LegacyPolicy is being broken — this suite was verified against a deliberately
un-frozen binary and, with the `init` shape alone, missed it completely. The
`nobudget` shape removes that block before `scan`, reproducing every repository
created before the block existed and the state `config.MutateManagedScope` still
produces today, and it is what actually turns red: scope_change_required=true,
governance blocked, from the upgrade alone.

The published number is 14 checks *per released version* (7 per config shape),
not a total: a total would change on every release and stop being a property of
this suite.

Usage:  python3 scripts/blackbox/mcp_upgrade.py
        python3 scripts/blackbox/mcp_upgrade.py --versions v0.1.0-rc5,v0.1.0-rc7
        python3 scripts/blackbox/mcp_upgrade.py --allow-offline
Env:    AOCI_REPO / AOCI_BIN override the repository and binary under test;
        AOCI_UPGRADE_CACHE overrides the downloaded-binary cache directory;
        AOCI_UPGRADE_SLUG overrides the owner/repo the releases are fetched from.
Requires: python3, git, network access to the release assets (cached after the
first run). Unlike conformance and scenarios this suite is not a portable
compatibility check: it asserts *this* project's own upgrade path.
"""
import argparse, hashlib, json, os, platform, re, shutil, subprocess, sys, tarfile
import tempfile, time, urllib.error, urllib.request, zipfile

_REPO_DEFAULT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
REPO = os.environ.get("AOCI_REPO", _REPO_DEFAULT)
BIN = os.environ.get("AOCI_BIN", os.path.join(REPO, "build", "aoci"))
CACHE = os.environ.get("AOCI_UPGRADE_CACHE", os.path.join(_REPO_DEFAULT, "scripts", "blackbox", ".upgrade-cache"))
SLUG = os.environ.get("AOCI_UPGRADE_SLUG", "aoci-spec/aoci-code")

# Checks asserted for every released version. This is the published number: it
# describes the suite, while the version list grows with each release.
CHECKS_PER_SHAPE = 7
# "init" is config.json exactly as the released `aoci init` wrote it, which
# always carries an explicit cognition_budget block. "nobudget" removes that
# block before scan: that is the population LegacyPolicy exists for, and the
# only shape in which un-freezing it is observable.
SHAPES = ("init", "nobudget")
CHECKS_PER_VERSION = CHECKS_PER_SHAPE * len(SHAPES)
CHECK_NAMES = ("post_scan_identity_stable", "aligned_repo_stays_aligned",
               "composite_identity_unchanged", "no_scope_change_demanded",
               "mcp_maintain_reaches_terminal", "overview_delivers_complete_index",
               "read_only_commands_write_nothing")
assert len(CHECK_NAMES) == CHECKS_PER_SHAPE

# Formal cognition plus the two governed state files. Deliberately excludes
# ledger.jsonl, verify_history/ and transactions/: Verify and Maintain are
# documented as appending audit there, so guarding them would assert the
# opposite of the contract.
GUARDED = ["aoci.txt", "aoci.meta.txt", "aoci.code.txt",
           os.path.join(".aoci", "baseline.json"), os.path.join(".aoci", "config.json")]

PASS, FAIL = [], []


def ok(name, cond, detail=""):
    (PASS if cond else FAIL).append((name, detail))
    print(("PASS " if cond else "FAIL ") + name + ((" | " + detail[:160]) if detail and not cond else ""))


# ---------- released-version discovery ----------
# The CHANGELOG is the release record the repository already maintains, so the
# matrix follows it instead of a hand-kept list that would silently miss a
# release. "## Unreleased" is skipped; it names no downloadable asset.
def released_versions():
    path = os.path.join(_REPO_DEFAULT, "CHANGELOG.md")
    with open(path, encoding="utf-8") as fh:
        found = re.findall(r"^## (v\d+\.\d+\.\d+\S*)\s*$", fh.read(), re.M)
    return list(dict.fromkeys(found))[::-1]  # oldest first


def platform_asset(version):
    bare = version.lstrip("v")
    system = platform.system().lower()
    machine = platform.machine().lower()
    arch = {"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}.get(machine)
    if arch is None or system not in ("linux", "darwin", "windows"):
        raise RuntimeError(f"unsupported platform {system}/{machine}")
    if system == "windows":
        return f"aoci_{bare}_windows_{arch}.zip", "aoci.exe"
    return f"aoci_{bare}_{system}_{arch}.tar.gz", "aoci"


def _download(url, dest):
    with urllib.request.urlopen(url, timeout=180) as resp, open(dest, "wb") as out:
        shutil.copyfileobj(resp, out)


# Fetch one released binary into the cache, checksum-verified against the
# release's own SHA256SUMS. Everything stays inside a temporary directory until
# the checksum passes and only then moves to its cached name, so an interrupted
# or corrupted download can never be picked up as cached by the next run.
def fetch_release_binary(version):
    archive_name, member = platform_asset(version)
    target = os.path.join(CACHE, f"aoci-{version}" + (".exe" if member.endswith(".exe") else ""))
    if os.path.exists(target):
        return target
    os.makedirs(CACHE, exist_ok=True)
    base = f"https://github.com/{SLUG}/releases/download/{version}"
    with tempfile.TemporaryDirectory(dir=CACHE) as work:
        archive = os.path.join(work, archive_name)
        sums = os.path.join(work, "SHA256SUMS")
        _download(f"{base}/{archive_name}", archive)
        _download(f"{base}/SHA256SUMS", sums)
        want = None
        with open(sums, encoding="utf-8") as fh:
            for line in fh:
                parts = line.split()
                if len(parts) == 2 and parts[1] == archive_name:
                    want = parts[0]
        if want is None:
            raise RuntimeError(f"{version}: {archive_name} absent from SHA256SUMS")
        digest = hashlib.sha256()
        with open(archive, "rb") as fh:
            for block in iter(lambda: fh.read(1 << 20), b""):
                digest.update(block)
        if digest.hexdigest() != want:
            raise RuntimeError(f"{version}: checksum mismatch for {archive_name}")
        # Read exactly the one member out and write it ourselves. TarFile.extract
        # grew a `filter` argument whose default changed across 3.12-3.14 and does
        # not exist at all on older interpreters; copying the stream has none of
        # that history and cannot write a path the archive chose.
        extracted = os.path.join(work, "extracted-binary")
        opener = zipfile.ZipFile if archive_name.endswith(".zip") else tarfile.open
        with opener(archive) as archive_file:
            source = (archive_file.open(member) if archive_name.endswith(".zip")
                      else archive_file.extractfile(member))
            if source is None:
                raise RuntimeError(f"{version}: {member} is not a regular file in {archive_name}")
            with source, open(extracted, "wb") as out:
                shutil.copyfileobj(source, out)
        os.chmod(extracted, 0o755)
        shutil.move(extracted, target)
    return target


# ---------- fixture ----------
FIXTURE = {
    "go.mod": "module example.com/upgradeprobe\n\ngo 1.24\n",
    "main.go": 'package main\n\nimport "fmt"\n\nfunc main() { fmt.Println(Greet()) }\n',
    "greet.go": 'package main\n\n// Greet returns the banner.\nfunc Greet() string { return "hi" }\n',
    os.path.join("internal", "foo", "foo.go"):
        "package foo\n\n// Sum adds two ints.\nfunc Sum(a, b int) int { return a + b }\n",
}
ENTRY_F = "Fixture object authored by the upgrade-axis regression track"


def make_fixture(path):
    for rel, body in FIXTURE.items():
        full = os.path.join(path, rel)
        os.makedirs(os.path.dirname(full), exist_ok=True)
        with open(full, "w", encoding="utf-8", newline="\n") as fh:
            fh.write(body)
    env = dict(os.environ, GIT_AUTHOR_NAME="probe", GIT_AUTHOR_EMAIL="probe@example.invalid",
               GIT_COMMITTER_NAME="probe", GIT_COMMITTER_EMAIL="probe@example.invalid")
    for args in (["init", "-q"], ["add", "-A"], ["commit", "-qm", "fixture"]):
        subprocess.run(["git", "-C", path] + args, check=True, env=env,
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


def run(binary, repo, *args, check=False):
    proc = subprocess.run([binary, "--repo", repo] + list(args), capture_output=True, text=True, timeout=300)
    if check and proc.returncode != 0:
        raise RuntimeError(f"{os.path.basename(binary)} {' '.join(args)} -> {proc.returncode}: {proc.stderr[:400]}")
    return proc


def verify_facts(binary, repo):
    proc = run(binary, repo, "verify", "--json")
    try:
        doc = json.loads(proc.stdout)
    except json.JSONDecodeError:
        return None
    gov = doc.get("governance") or {}
    scope = gov.get("managed_scope") or {}
    budget = gov.get("budget") or {}
    code_objects = 0
    for vol in doc.get("volumes") or []:
        if vol.get("id") == "code":
            code_objects = vol.get("object_count") or 0
    return {
        "code_object_count": code_objects,
        "aligned": gov.get("governance_aligned"),
        "composite_identity": doc.get("composite_identity"),
        "layout_mode": doc.get("layout_mode"),
        "policy_identity": scope.get("policy_identity"),
        "active_policy_identity": scope.get("active_policy_identity"),
        "scope_change_required": scope.get("scope_change_required"),
        "budget_mode": budget.get("mode"),
        "budget_max_tokens": budget.get("max_tokens"),
        "next_required_action": gov.get("next_required_action"),
        "finding_count": len(gov.get("findings") or []),
    }


def snapshot(repo):
    out = {}
    for rel in GUARDED:
        full = os.path.join(repo, rel)
        if os.path.exists(full):
            with open(full, "rb") as fh:
                out[rel] = hashlib.sha256(fh.read()).hexdigest()
    return out


# Author one Entry per missing target with the *old* binary, so the formal index
# is written entirely by the released version under test.
def author_to_aligned(binary, repo):
    for _ in range(8):
        proc = run(binary, repo, "verify", "--json")
        try:
            report = json.loads(proc.stdout)
        except json.JSONDecodeError:
            return False
        gov = report.get("governance") or {}
        if gov.get("governance_aligned"):
            return True
        targets = [f["target"] for f in gov.get("findings") or []
                   if isinstance(f, dict) and f.get("code") == "code_missing" and f.get("target")]
        if not targets:
            return False  # misaligned for a reason authoring cannot clear
        for rel in targets:
            with open(os.path.join(repo, rel), "rb") as fh:
                sha = hashlib.sha256(fh.read()).hexdigest()
            entry = f"{os.path.basename(rel)}[CG5T]: F:{ENTRY_F} | R:- | A:- | S:-"
            run(binary, repo, "update-entry", "--path", rel, "--source-sha256", sha, "--entry", entry)
    return False


# ---------- minimal MCP stdio client ----------
class Session:
    def __init__(self, binary, repo):
        self.p = subprocess.Popen([binary, "--repo", repo, "mcp"], stdin=subprocess.PIPE,
                                  stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                  text=True, encoding="utf-8", bufsize=1)
        self.next_id = 1

    def rpc(self, method, params=None, timeout=180):
        rid = self.next_id
        self.next_id += 1
        msg = {"jsonrpc": "2.0", "id": rid, "method": method}
        if params is not None:
            msg["params"] = params
        self.p.stdin.write(json.dumps(msg) + "\n")
        self.p.stdin.flush()
        deadline = time.time() + timeout
        while time.time() < deadline:
            line = self.p.stdout.readline()
            if not line:
                raise RuntimeError("server closed stdout")
            line = line.strip()
            if not line:
                continue
            try:
                obj = json.loads(line)
            except json.JSONDecodeError:
                continue
            if obj.get("id") == rid:
                return obj
        raise RuntimeError(f"timeout waiting for {method}")

    def __enter__(self):
        self.rpc("initialize", {"protocolVersion": "2025-06-18", "capabilities": {},
                                "clientInfo": {"name": "aoci-upgrade-harness", "version": "1"}})
        self.p.stdin.write(json.dumps({"jsonrpc": "2.0", "method": "notifications/initialized"}) + "\n")
        self.p.stdin.flush()
        return self

    def __exit__(self, *_):
        try:
            self.p.stdin.close()
            self.p.wait(timeout=20)
        except Exception:
            self.p.kill()

    def call(self, tool, args=None):
        res = self.rpc("tools/call", {"name": tool, "arguments": args or {}})
        blocks = ((res.get("result") or {}).get("content") or [])
        return "".join(b.get("text", "") for b in blocks if b.get("type") == "text")


def maintain_facts(text):
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        return None


# Overview answers in two shapes and both must be read here: an index that fits
# chunk_tokens comes back as a `key: value` metadata header (delivery_mode=full),
# and only a chunked delivery leads with a JSON receipt. A parser that knew just
# one of them would report the other as a delivery failure.
def parse_overview(text):
    head = text.split("<<<")[0].strip()
    if head.startswith("{"):
        return maintain_facts(head)
    doc = {}
    for line in head.splitlines():
        if ":" not in line:
            continue
        key, _, value = line.partition(":")
        key, value = key.strip(), value.strip()
        if not key or " " in key:
            continue
        if value in ("true", "false"):
            doc[key] = value == "true"
        elif value.isdigit():
            doc[key] = int(value)
        else:
            doc[key] = value
    return doc or None


# Walk the Overview to completion. A tiny fixture normally fits one chunk; the
# loop exists so a future fixture growing past chunk_tokens does not silently
# turn this into a one-chunk check.
def overview_delivery(session):
    doc = parse_overview(session.call("aoci_overview", {"scope": "all"}))
    if doc is None:
        return None
    guard = 0
    while doc.get("continuation_required") and guard < 64:
        guard += 1
        cursor = doc.get("next_cursor")
        if not cursor:
            return None
        doc = parse_overview(session.call("aoci_overview", {"scope": "all", "cursor": cursor}))
        if doc is None:
            return None
    return doc


def strip_budget_block(repo):
    path = os.path.join(repo, ".aoci", "config.json")
    with open(path, encoding="utf-8") as fh:
        config = json.load(fh)
    removed = config.pop("cognition_budget", None) is not None
    with open(path, "w", encoding="utf-8", newline="\n") as fh:
        json.dump(config, fh, indent=2)
        fh.write("\n")
    return removed


def check_version(version, shape, old_binary, workdir):
    tag = f"{version}[{shape}]"
    repo = os.path.join(workdir, f"{version}-{shape}")
    os.makedirs(repo, exist_ok=True)
    make_fixture(repo)
    run(old_binary, repo, "init", "--locale", "en-US", check=True)
    # The block must go before scan: scan is what stamps budget_policy_identity
    # into the Baseline, and stamping the explicit block would defeat the shape.
    if shape == "nobudget" and not strip_budget_block(repo):
        for name in CHECK_NAMES:
            ok(f"{tag}.{name}", False, "this release wrote no cognition_budget block to remove")
        return
    run(old_binary, repo, "scan", check=True)

    # 1. Identity stability is observable before a single Entry exists: scan is
    #    what stamps the policy and budget identities into the Baseline.
    old_scan = verify_facts(old_binary, repo)
    new_scan = verify_facts(BIN, repo)
    keys = ("policy_identity", "active_policy_identity", "budget_mode", "budget_max_tokens", "layout_mode")
    same = old_scan is not None and new_scan is not None and all(old_scan[k] == new_scan[k] for k in keys)
    ok(f"{tag}.post_scan_identity_stable", same,
       "" if same else f"old={old_scan} new={new_scan}")

    if not author_to_aligned(old_binary, repo):
        for name in CHECK_NAMES[1:]:  # post_scan_identity_stable already reported
            ok(f"{tag}.{name}", False, "the released binary could not author this fixture to aligned")
        return

    old = verify_facts(old_binary, repo)
    before = snapshot(repo)
    new = verify_facts(BIN, repo)

    # 2-4. The upgrade must not move alignment, identity, or governance posture.
    ok(f"{tag}.aligned_repo_stays_aligned", bool(new and new["aligned"]),
       "" if (new and new["aligned"]) else f"new={new}")
    ok(f"{tag}.composite_identity_unchanged",
       bool(new and old and new["composite_identity"] == old["composite_identity"]),
       "" if (new and old and new["composite_identity"] == old["composite_identity"])
       else f"old={old and old['composite_identity']} new={new and new['composite_identity']}")
    ok(f"{tag}.no_scope_change_demanded", bool(new and new["scope_change_required"] is False),
       "" if (new and new["scope_change_required"] is False) else f"scope_change_required={new and new['scope_change_required']}")

    # 5-7. The read paths a host actually drives, then the no-write guard over
    #      everything they touched.
    maintain, overview = None, None
    with Session(BIN, repo) as s:
        maintain = maintain_facts(s.call("aoci_maintain"))
        overview = overview_delivery(s)
    run(BIN, repo, "status")

    terminal = bool(maintain and maintain.get("aligned") is True
                    and maintain.get("next_action") == "none"
                    and not (maintain.get("governance") or {}).get("findings"))
    ok(f"{tag}.mcp_maintain_reaches_terminal", terminal,
       "" if terminal else f"aligned={maintain and maintain.get('aligned')} next={maintain and maintain.get('next_action')}")

    code_objects = (old or {}).get("code_object_count") or 0
    delivered = bool(overview and overview.get("completed") and overview.get("entry_count") == code_objects
                     and code_objects > 0)
    ok(f"{tag}.overview_delivers_complete_index", delivered,
       "" if delivered else f"completed={overview and overview.get('completed')} "
                            f"entry_count={overview and overview.get('entry_count')} objects={code_objects}")

    after = snapshot(repo)
    drift = sorted(k for k in set(before) | set(after) if before.get(k) != after.get(k))
    ok(f"{tag}.read_only_commands_write_nothing", not drift,
       "" if not drift else "rewrote " + ", ".join(drift))


def main():
    ap = argparse.ArgumentParser(description="AOCI upgrade-axis black-box suite")
    ap.add_argument("--versions", default="", help="comma-separated release tags (default: every CHANGELOG release)")
    ap.add_argument("--allow-offline", action="store_true",
                    help="report a download failure as a skip instead of failing the run")
    args = ap.parse_args()

    if not os.path.exists(BIN):
        print(f"binary under test not found: {BIN} (run make build, or set AOCI_BIN)")
        return 2

    versions = [v.strip() for v in args.versions.split(",") if v.strip()] or released_versions()
    if not versions:
        print("no released versions discovered in CHANGELOG.md")
        return 2
    print(f"binary under test: {BIN}")
    print(f"upgrade matrix ({len(versions)}): {', '.join(versions)}\n")

    skipped = []
    with tempfile.TemporaryDirectory(prefix="aoci-upgrade-") as workdir:
        for version in versions:
            try:
                old_binary = fetch_release_binary(version)
            except (urllib.error.URLError, OSError, RuntimeError) as err:
                # A download failure is a real gate failure by default: a suite
                # that quietly skips its whole matrix is a false green.
                if args.allow_offline:
                    skipped.append(f"{version}: {err}")
                    print(f"SKIP {version} | {err}")
                    continue
                ok(f"{version}.release_binary_available", False, str(err))
                continue
            for shape in SHAPES:
                try:
                    check_version(version, shape, old_binary, workdir)
                except Exception as err:  # a crash while probing one shape is that shape's failure
                    ok(f"{version}[{shape}].upgrade_probe_completed", False, f"{type(err).__name__}: {err}")

    # ---------- documentation binding ----------
    # Same discipline as the conformance and scenario suites: the run is the
    # authority and enforces the documents. The published number is the
    # per-version check count, because a total would move on every release and
    # stop describing this suite. Kept outside PASS/FAIL so the number counts
    # upgrade checks rather than counting itself.
    doc_counts = {
        "README.md": r"\*\*Upgrade axis\*\* — (\d+) checks per released version",
        "README.zh-CN.md": r"\*\*升级轴\*\* —— 每个已发布版本 (\d+) 项检查",
        os.path.join("scripts", "blackbox", "README.md"): r"Upgrade axis \((\d+) checks per released version",
        os.path.join("scripts", "blackbox", "mcp_upgrade.py"): r"每个已发布版本 (\d+) 项检查",
    }
    drift = []
    for rel, pattern in (doc_counts.items() if REPO == _REPO_DEFAULT else []):
        try:
            with open(os.path.join(_REPO_DEFAULT, rel), encoding="utf-8") as fh:
                match = re.search(pattern, fh.read())
        except OSError as err:
            drift.append(f"{rel}: unreadable ({err})")
            continue
        if match is None:
            drift.append(f"{rel}: no documented per-version check count found")
        elif int(match.group(1)) != CHECKS_PER_VERSION:
            drift.append(f"{rel}: documents {match.group(1)}, this suite asserts {CHECKS_PER_VERSION}")
    print(("PASS " if not drift else "FAIL ") + "docs.published_check_count_matches_this_suite"
          + ((" | " + "; ".join(drift)) if drift else f" | {CHECKS_PER_VERSION} per version"))

    print()
    print(f"RESULT: {len(PASS)} passed, {len(FAIL)} failed" + (f", {len(skipped)} skipped" if skipped else ""))
    for name, detail in FAIL:
        print("  FAILED:", name, "|", detail[:220])
    for line in skipped:
        print("  SKIPPED:", line)
    if drift:
        print("  FAILED: docs.published_check_count_matches_this_suite |", "; ".join(drift))
    return 1 if (FAIL or drift) else 0


if __name__ == "__main__":
    sys.exit(main())
