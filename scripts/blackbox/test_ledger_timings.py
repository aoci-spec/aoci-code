"""Exercise the timing CLI against mixed valid and malformed Ledger rows."""
import json
import pathlib
import subprocess
import sys
import tempfile
import unittest


SCRIPT = pathlib.Path(__file__).with_name("ledger_timings.py")


class LedgerTimingsTests(unittest.TestCase):
    def summarize(self, rows, *args):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            (root / ".aoci").mkdir()
            ledger = root / ".aoci/ledger.jsonl"
            contents = "\n".join(row if isinstance(row, str) else json.dumps(row) for row in rows) + "\n"
            ledger.write_text(contents, encoding="utf-8")
            result = subprocess.run([sys.executable, str(SCRIPT), str(root), *args],
                                    capture_output=True, text=True, encoding="utf-8", timeout=15)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stderr, "")
            self.assertEqual(len(result.stdout.splitlines()), 1)
            self.assertEqual(ledger.read_text(encoding="utf-8"), contents)
            return json.loads(result.stdout)

    @staticmethod
    def event(ms, **fields):
        return {"ts": "2026-11-09T00:00:00Z", "op": "maintain", "duration_ms": ms, **fields}

    def test_non_object_json_and_broken_json_are_counted_and_skipped(self):
        malformed = [None, [], 17, True, False, '"text"', "not JSON", "[" * 2000 + "]" * 2000]
        result = self.summarize([self.event(10), *malformed, self.event(30)])
        self.assertEqual(result["calls"], {"maintain": {"n": 2, "median_ms": 30, "p90_ms": 30, "max_ms": 30}})
        self.assertEqual(result["unreadable_lines"], len(malformed))

    def test_invalid_known_fields_do_not_contribute_partial_statistics(self):
        malformed = [self.event(500, ts=123), self.event(500, ts=False),
                     self.event(500, op=[]), self.event(500, op={}),
                     self.event("500"), self.event(True), self.event(-1), self.event(1.5),
                     self.event(float("nan")), self.event(float("inf")),
                     self.event(500, paths_count=True), self.event(500, paths_count=-1),
                     self.event(500, paths_count="999"), self.event([], paths_count=999)]
        result = self.summarize([self.event(10, paths_count=4), *malformed, self.event(30, paths_count=5)])
        self.assertEqual(result["index_sources"], 5)
        self.assertEqual(result["calls"]["maintain"]["n"], 2)
        self.assertEqual(result["calls"]["maintain"]["max_ms"], 30)
        self.assertEqual(result["unreadable_lines"], len(malformed))

    def test_zero_metrics_and_optional_missing_fields_remain_usable(self):
        result = self.summarize([{}, {"paths_count": 9},
                                 {"ts": None, "op": None, "duration_ms": None},
                                 {"op": "maintain", "duration_ms": 0},
                                 {"op": "unselected", "duration_ms": 20, "paths_count": 12}])
        self.assertEqual(result["index_sources"], 12)
        self.assertEqual(result["calls"], {"maintain": {"n": 1, "median_ms": 0, "p90_ms": 0, "max_ms": 0}})
        self.assertEqual(result["events_from"], "")
        self.assertEqual(result["events_to"], "")
        self.assertNotIn("unreadable_lines", result)

    def test_filters_and_percentiles_keep_the_existing_output_contract(self):
        result = self.summarize([
            self.event(999, ts="2026-11-08T23:59:59Z", paths_count=999),
            self.event(10, paths_count=5), self.event(20), self.event(30), self.event(40),
            self.event(900, op="overview", ts="2026-11-09T01:00:00Z", paths_count=8)],
            "--since=2026-11-09", "--ops=maintain")
        self.assertEqual(result["version"], "ledger-timings/v1")
        self.assertEqual(result["platform"], sys.platform)
        self.assertEqual(result["events_from"], "2026-11-09T00:00:00")
        self.assertEqual(result["events_to"], "2026-11-09T00:00:00")
        self.assertEqual(result["index_sources"], 8)
        self.assertEqual(result["calls"], {"maintain": {"n": 4, "median_ms": 30, "p90_ms": 40, "max_ms": 40}})
        self.assertNotIn("unreadable_lines", result)

    def test_all_malformed_rows_produce_an_empty_summary(self):
        result = self.summarize([None, [], "broken JSON"])
        self.assertEqual(result["calls"], {})
        self.assertEqual(result["index_sources"], 0)
        self.assertEqual(result["events_from"], "")
        self.assertEqual(result["events_to"], "")
        self.assertEqual(result["unreadable_lines"], 3)

    def test_invalid_arguments_fail_before_reading_the_ledger(self):
        cases = (
            (["--sicne=2026-11-09"], "unknown option: --sicne"),
            (["--since"], "--since requires"),
            (["--since", "2026-11-09"], "--since requires"),
            (["--ops"], "--ops requires"),
            (["--ops="], "--ops requires"),
            (["--ops=,,"], "--ops requires"),
            (["--since=2026-11-31"], "--since requires a valid date"),
            (["--since=20261109"], "--since requires a valid date"),
            (["one", "two"], "expected at most one repository root"),
        )
        with tempfile.TemporaryDirectory() as directory:
            for args, diagnostic in cases:
                with self.subTest(args=args):
                    result = subprocess.run([sys.executable, str(SCRIPT), *args], cwd=directory,
                                            capture_output=True, text=True, encoding="utf-8", timeout=15)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertEqual(result.stdout, "")
                    self.assertIn(diagnostic, result.stderr)


if __name__ == "__main__":
    unittest.main()
