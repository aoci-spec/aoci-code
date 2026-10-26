#!/usr/bin/env python3
"""Benchmark regression visibility for the seven first-party benchmarks.

Runs the same benchmark set the full-confidence performance fixtures step runs,
with -benchtime=3x -count=3, takes the median ns/op of every benchmark result
line, writes bench-results.json, and compares each median with the committed
baseline in scripts/bench-baseline.json. A benchmark more than THRESHOLD percent
slower than its baseline is reported as a GitHub Actions ::warning:: annotation.

One exception to 3x/3: the 10,000-object migration fixtures. One iteration of
each of their four sub-benchmarks takes about half a minute on a fast machine,
so they run one iteration, one sample, and the summary says so. Until 0.2.0
the migration mapping template was cubic in the Entry count: the 1000-object
fixtures took about a minute an iteration (3x/3 took 54 minutes) and the
10,000-object ones hours, so those were never run; both now scale linearly
and the 1000-object fixtures take the ordinary 3x/3.

The comparison never fails the run: shared runners are noisy and the baseline
was measured on one machine, so the numbers are advisory. Only a benchmark that
fails to build or run makes the exit status nonzero, exactly as the plain
go test invocation did before this script existed.

Usage:
  python3 scripts/bench-compare.py                     run, write, compare
  python3 scripts/bench-compare.py --update-baseline   run, then rewrite the baseline
  python3 scripts/bench-compare.py --from-results build/bench-results.json
                                                       compare an existing results
                                                       file (for example the CI
                                                       artifact) without running;
                                                       with --update-baseline it
                                                       becomes the new baseline
Options: --output PATH (default build/bench-results.json under the repository),
         --baseline PATH (default scripts/bench-baseline.json),
         --threshold PERCENT (default 30), --benchtime (3x), --count (3);
         the last two apply to every group without a pinned setting.
Env:     GO_BIN overrides the go executable; GITHUB_STEP_SUMMARY receives a
         Markdown table when set.
"""
import argparse
import datetime
import json
import os
import pathlib
import re
import statistics
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent

# One entry per go test invocation, mirroring the performance fixtures step.
# A group may pin benchtime and count; the others take the command line values.
GROUPS = [
    {"bench": "^BenchmarkMigrationScale$/^objects-(1|100|1000)$/", "packages": ["./internal/migrationapply"]},
    # About half a minute per iteration and four sub-benchmarks: one iteration, one sample.
    {"bench": "^BenchmarkMigrationScale$/^objects-10000$/", "packages": ["./internal/migrationapply"],
     "benchtime": "1x", "count": 1},
    {"bench": "Benchmark(BootstrapScale|BootstrapPlannerScale)$",
     "packages": ["./internal/bootstrapapply", "./internal/cognitionplan"]},
    {"bench": "Benchmark(DatabaseCognitionScale|ResolveImpactLargeIndependentSet|ResolveImpactTenThousandMixedSet)$",
     "packages": ["./internal/dbcognition", "./internal/cognition"]},
    {"bench": "^BenchmarkFacts1000CodeObjects$", "packages": ["./internal/volumegovernance"]},
]

RESULT_LINE = re.compile(r"^(Benchmark\S+)\s+(\d+)\s+([0-9.]+) ns/op")
PACKAGE_LINE = re.compile(r"^pkg: (\S+)")


def parse_args():
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n", 1)[0])
    parser.add_argument("--output", default=str(ROOT / "build" / "bench-results.json"))
    parser.add_argument("--baseline", default=str(ROOT / "scripts" / "bench-baseline.json"))
    parser.add_argument("--threshold", type=float, default=30.0, help="percent slower that earns a warning")
    parser.add_argument("--benchtime", default="3x")
    parser.add_argument("--count", type=int, default=3)
    parser.add_argument("--update-baseline", action="store_true", help="rewrite the baseline from this run")
    parser.add_argument("--from-results", metavar="PATH",
                        help="take the medians from this results file instead of running the benchmarks")
    return parser.parse_args()


def cpu_count():
    try:
        return len(os.sched_getaffinity(0))
    except AttributeError:
        return os.cpu_count() or 1


def go_version(go):
    try:
        return subprocess.run([go, "version"], capture_output=True, text=True, check=True).stdout.strip()
    except (OSError, subprocess.CalledProcessError):
        return "unknown"


def run_benchmarks(go, benchtime, count, cpus):
    """Return ({name: {"package": pkg, "samples": [ns, ...]}}, failed_groups)."""
    samples = {}
    failed = []
    # Go appends -<GOMAXPROCS> to every benchmark name unless it is 1. Pinning
    # -cpu makes that suffix known, so names stay stable across machines.
    suffix = "" if cpus == 1 else "-%d" % cpus
    for group in GROUPS:
        command = [go, "test", "-run", "^$", "-bench", group["bench"],
                   "-benchtime", group.get("benchtime", benchtime),
                   "-count", str(group.get("count", count)), "-cpu", str(cpus), *group["packages"]]
        print("bench-compare: " + " ".join(command), flush=True)
        completed = subprocess.run(command, cwd=ROOT, capture_output=True, text=True)
        sys.stdout.write(completed.stdout)
        if completed.returncode != 0:
            sys.stdout.write(completed.stderr)
            failed.append(group["bench"])
            print("bench-compare: go test exited %d for %s" % (completed.returncode, group["bench"]), flush=True)
            continue
        package = ""
        for line in completed.stdout.splitlines():
            matched = PACKAGE_LINE.match(line)
            if matched:
                package = matched.group(1)
                continue
            matched = RESULT_LINE.match(line)
            if not matched:
                continue
            name = matched.group(1)
            if suffix and name.endswith(suffix):
                name = name[: -len(suffix)]
            entry = samples.setdefault(name, {"package": package, "samples": []})
            entry["samples"].append(float(matched.group(3)))
    return samples, failed


def medians(samples):
    return {
        name: {
            "package": entry["package"],
            "median_ns_per_op": statistics.median(entry["samples"]),
            "samples_ns_per_op": entry["samples"],
        }
        for name, entry in sorted(samples.items())
    }


def human(ns):
    for unit, scale in (("s", 1e9), ("ms", 1e6), ("µs", 1e3)):
        if ns >= scale:
            return "%.3g %s" % (ns / scale, unit)
    return "%.3g ns" % ns


def load_json(path):
    with open(path, encoding="utf-8") as handle:
        return json.load(handle)


def write_json(path, payload):
    pathlib.Path(path).parent.mkdir(parents=True, exist_ok=True)
    with open(path, "w", encoding="utf-8") as handle:
        json.dump(payload, handle, indent=2, sort_keys=False)
        handle.write("\n")


def compare(results, baseline, threshold):
    """Print one line per benchmark and return the Markdown rows."""
    rows = []
    benchmarks = results.get("benchmarks", {})
    known = baseline.get("benchmarks", {}) if baseline else {}
    for name, entry in benchmarks.items():
        median = entry["median_ns_per_op"]
        count = len(entry.get("samples_ns_per_op", []))
        basis = "median of %d" % count if count > 1 else "single sample"
        reference = known.get(name, {}).get("median_ns_per_op")
        if not reference:
            print("::notice title=no benchmark baseline::%s has no baseline entry (%s per op, %s)" % (name, human(median), basis))
            rows.append((name, human(median), "-", "-", "no baseline, " + basis))
            continue
        delta = (median / reference - 1.0) * 100.0
        status = "ok"
        if delta > threshold:
            status = "slower"
            print("::warning title=benchmark slower than baseline::%s is %.1f%% slower than its baseline (%s vs %s per op, %s)"
                  % (name, delta, human(median), human(reference), basis))
        print("bench-compare: %-72s median %12s baseline %12s %+7.1f%% %s (%s)" % (name, human(median), human(reference), delta, status, basis))
        rows.append((name, human(median), human(reference), "%+.1f%%" % delta, status + ", " + basis))
    for name in sorted(set(known) - set(benchmarks)):
        print("::notice title=benchmark missing::%s is in the baseline but produced no result" % name)
        rows.append((name, "-", human(known[name]["median_ns_per_op"]), "-", "missing"))
    return rows


def write_step_summary(rows, results, threshold, failed):
    path = os.environ.get("GITHUB_STEP_SUMMARY")
    if not path or not rows and not failed:
        return
    lines = ["### Benchmark medians against scripts/bench-baseline.json (advisory, warn above +%g%%)" % threshold, "",
             "| Benchmark | median | baseline | delta | status |", "| --- | ---: | ---: | ---: | --- |"]
    lines += ["| `%s` | %s | %s | %s | %s |" % row for row in rows]
    lines.append("")
    lines.append("%s, %s, benchtime %s, count %d, cpu %s" % (results.get("go_version", "go"), results.get("goos_goarch", ""),
                                                            results.get("benchtime"), results.get("count", 0), results.get("cpu")))
    for pattern in failed:
        lines.append("")
        lines.append("go test failed for `%s`; see the job log." % pattern)
    with open(path, "a", encoding="utf-8") as handle:
        handle.write("\n".join(lines) + "\n")


def main():
    args = parse_args()
    go = os.environ.get("GO_BIN", "go")
    failed = []
    if args.from_results:
        results = load_json(args.from_results)
    else:
        cpus = cpu_count()
        samples, failed = run_benchmarks(go, args.benchtime, args.count, cpus)
        results = {
            "version": 1,
            "generated_at": datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0).isoformat(),
            "go_version": go_version(go),
            "goos_goarch": "%s/%s" % (sys.platform, os.uname().machine if hasattr(os, "uname") else "unknown"),
            "benchtime": args.benchtime,
            "count": args.count,
            "cpu": cpus,
            "benchmarks": medians(samples),
        }
        write_json(args.output, results)
        print("bench-compare: wrote %s (%d benchmark result names)" % (args.output, len(results["benchmarks"])))
    if args.update_baseline:
        baseline = {key: results[key] for key in ("version", "generated_at", "go_version", "goos_goarch", "benchtime", "count", "cpu")
                    if key in results}
        baseline["source"] = "scripts/bench-compare.py --update-baseline"
        baseline["benchmarks"] = {
            name: {"package": entry["package"], "samples": len(entry.get("samples_ns_per_op", [])),
                   "median_ns_per_op": entry["median_ns_per_op"]}
            for name, entry in results["benchmarks"].items()
        }
        write_json(args.baseline, baseline)
        print("bench-compare: rewrote baseline %s" % args.baseline)
        return 1 if failed else 0
    baseline = None
    if os.path.exists(args.baseline):
        baseline = load_json(args.baseline)
    else:
        print("::notice title=no benchmark baseline::%s does not exist; run with --update-baseline to create it" % args.baseline)
    rows = compare(results, baseline, args.threshold)
    write_step_summary(rows, results, args.threshold, failed)
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
