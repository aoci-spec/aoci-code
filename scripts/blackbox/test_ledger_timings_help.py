"""Help is a successful CLI operation even without a repository or Ledger."""
import pathlib
import subprocess
import sys
import tempfile
import unittest


SCRIPT = pathlib.Path(__file__).with_name("ledger_timings.py").resolve()


class LedgerTimingsHelpTests(unittest.TestCase):
    def test_help_aliases_succeed_on_stdout_without_a_ledger(self):
        with tempfile.TemporaryDirectory() as directory:
            for flag in ("--help", "-h"):
                with self.subTest(flag=flag):
                    result = subprocess.run([sys.executable, str(SCRIPT), flag], cwd=directory,
                                            capture_output=True, text=True, encoding="utf-8", timeout=15)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertIn("usage: ledger_timings.py", result.stdout)
                    self.assertEqual(result.stderr, "")
                    self.assertEqual(list(pathlib.Path(directory).iterdir()), [])


if __name__ == "__main__":
    unittest.main()
