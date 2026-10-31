#!/usr/bin/env python3
"""Run-by-name test ladder: the registry of every executable test contract that
someone must run by name, with its cadence, preconditions, and the last
verification on record.

Package tests run under `make fast` and `make full`, and CI runs its own
workflows; neither needs a registry. The contracts here are the ones that are
easy to forget: suites with preconditions (Docker, cached release binaries,
local clones, a real agent), release-time runs that compare against the
previous release, and nightly-only gates. `ladder.json` is the single list;
a suite that is not in it is a suite nobody is asked to run.

  python3 scripts/blackbox/ladder.py --check          # overdue and never-run contracts (exit 0)
  python3 scripts/blackbox/ladder.py --check --strict # exit 1 when anything is overdue
  python3 scripts/blackbox/ladder.py --list           # the whole registry
  python3 scripts/blackbox/ladder.py --mark <id> --version v0.1.0-rcN --date YYYY-MM-DD --evidence <path or note>

Cadences: change (every change, CI-owned), closure (every task closure through
make verify), release (once per release, before or right after the tag),
nightly (CI schedule), quarterly (a review no gate runs, such as the
dependency review), manual (on demand, with the precondition listed).
A release-cadence contract is overdue when its last verified version is not
the newest released heading in CHANGELOG.md (an Unreleased section above it
is skipped); a closure, nightly, or quarterly contract when
its record is older than the age limit; manual contracts are listed, never
overdue. The registry is maintained by hand through --mark, so a stale record
means "nobody wrote it down", which is the fact worth seeing."""
import argparse, datetime, json, os, re, sys

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.dirname(os.path.dirname(HERE))
PATH = os.path.join(HERE, "ladder.json")
AGE_DAYS = {"closure": 21, "nightly": 7, "change": 21, "quarterly": 92}


def top_release():
    with open(os.path.join(REPO, "CHANGELOG.md"), encoding="utf-8") as fh:
        for line in fh:
            m = re.match(r"^## (v\d+\.\d+\.\d+(?:-rc\d+)?)\s*$", line)
            if m:
                return m.group(1)
    return None


def load():
    with open(PATH, encoding="utf-8") as fh:
        doc = json.load(fh)
    if doc.get("version") != "test-ladder/v1":
        sys.exit(f"{PATH}: unsupported version {doc.get('version')!r}")
    ids = [e["id"] for e in doc["entries"]]
    if len(ids) != len(set(ids)):
        sys.exit(f"{PATH}: duplicate ids")
    return doc


def save(doc):
    with open(PATH, "w", encoding="utf-8") as fh:
        json.dump(doc, fh, indent=1, ensure_ascii=False)
        fh.write("\n")


def check(doc, today, strict):
    release = top_release()
    overdue = []
    for e in doc["entries"]:
        last = e.get("last_verified") or {}
        cadence = e["cadence"]
        reason = ""
        if cadence == "manual":
            continue
        if not last:
            reason = "never recorded"
        elif cadence == "release":
            if release and last.get("version") != release:
                reason = f"last verified at {last.get('version')}, current release is {release}"
        elif cadence in AGE_DAYS:
            try:
                age = (today - datetime.date.fromisoformat(last["date"])).days
            except (KeyError, ValueError):
                age = None
            if age is None or age > AGE_DAYS[cadence]:
                reason = f"last record {last.get('date', '?')}, older than {AGE_DAYS[cadence]} days"
        if reason:
            overdue.append((e, reason))
    print(f"ladder: {len(doc['entries'])} contracts, top release {release or 'unreleased'}, {len(overdue)} overdue")
    for e, reason in overdue:
        pre = ("; needs " + ", ".join(e["preconditions"])) if e.get("preconditions") else ""
        print(f"  OVERDUE {e['id']:<22} {reason}{pre}\n          {e['command']}")
    return 1 if (strict and overdue) else 0


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--check", action="store_true")
    ap.add_argument("--strict", action="store_true")
    ap.add_argument("--list", action="store_true")
    ap.add_argument("--mark", metavar="ID")
    ap.add_argument("--version")
    ap.add_argument("--date", default=datetime.date.today().isoformat())
    ap.add_argument("--evidence", default="")
    a = ap.parse_args()
    doc = load()
    if a.list:
        for e in doc["entries"]:
            last = e.get("last_verified") or {}
            print(f"{e['id']:<22} {e['cadence']:<8} {last.get('version', '-'):<13} {last.get('date', '-'):<10} {e['command']}")
        return 0
    if a.mark:
        for e in doc["entries"]:
            if e["id"] == a.mark:
                if not a.version:
                    sys.exit("--mark needs --version")
                e["last_verified"] = {"version": a.version, "date": a.date, "evidence": a.evidence}
                save(doc)
                print(f"marked {a.mark}: {a.version} {a.date} {a.evidence}")
                return 0
        sys.exit(f"unknown id {a.mark!r}; ids: {', '.join(e['id'] for e in doc['entries'])}")
    return check(doc, datetime.date.today(), a.strict)


if __name__ == "__main__":
    sys.exit(main())
