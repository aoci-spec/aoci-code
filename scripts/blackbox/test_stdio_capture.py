"""Exercise every MCP harness client against noisy real subprocess pipes."""
import ast
from pathlib import Path
import subprocess
import sys
import time
import unittest
from unittest import mock

from stdio_capture import BoundedStderr, STDERR_TAIL_BYTES


HARNESS_DIR = Path(__file__).resolve().parent
HARNESSES = ("mcp_conformance.py", "mcp_scenarios.py", "mcp_upgrade.py")
SERVER = r'''
import json, sys
mode = sys.argv[1]
if mode == "flood":
    sys.stderr.buffer.write(b"x" * 1_000_000 + b"retained-tail-marker\n")
    sys.stderr.flush()
request = json.loads(sys.stdin.readline())
if mode == "eof":
    print("useful failure detail", file=sys.stderr, flush=True)
else:
    print(json.dumps({"jsonrpc":"2.0", "id":request["id"], "result":{}}), flush=True)
'''


def session_class(filename):
    tree = ast.parse((HARNESS_DIR / filename).read_text(encoding="utf-8"))
    nodes = [node for node in tree.body
             if isinstance(node, (ast.Import, ast.ImportFrom))
             or isinstance(node, ast.ClassDef) and node.name == "Session"]
    namespace = {"BIN": "unused-binary", "REPO": "unused-repo"}
    exec(compile(ast.Module(body=nodes, type_ignores=[]), filename, "exec"), namespace)
    return namespace["Session"], namespace


class StderrCaptureTests(unittest.TestCase):
    def process(self, mode):
        process = subprocess.Popen([sys.executable, "-u", "-c", SERVER, mode],
                                   stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                   stderr=subprocess.PIPE, text=True, encoding="utf-8")
        self.addCleanup(self.close_process, process)
        return process

    @staticmethod
    def close_process(process):
        if process.poll() is None:
            process.kill()
        process.wait(timeout=5)
        for pipe in (process.stdin, process.stdout, process.stderr):
            pipe.close()

    def construct(self, filename, process):
        cls, namespace = session_class(filename)
        with mock.patch.object(namespace["subprocess"], "Popen", return_value=process):
            if filename == "mcp_conformance.py":
                client = cls()
                client.rpc("initialize", timeout=3)
            elif filename == "mcp_scenarios.py":
                client = cls("unused-repo")
            else:
                client = cls("unused-binary", "unused-repo")
                client.__enter__()
        self.addCleanup(self.close_client, filename, client)
        return client

    @staticmethod
    def close_client(filename, client):
        if filename == "mcp_upgrade.py":
            client.__exit__(None, None, None)
        else:
            client.close()

    def test_large_stderr_cannot_block_rpc_and_retains_only_its_tail(self):
        for filename in HARNESSES:
            with self.subTest(harness=filename):
                client = self.construct(filename, self.process("flood"))
                deadline = time.monotonic() + 1
                while "retained-tail-marker" not in client.stderr_capture.text() \
                        and time.monotonic() < deadline:
                    time.sleep(0.01)
                tail = client.stderr_capture.text()
                self.assertIn("retained-tail-marker", tail)
                self.assertLessEqual(len(tail.encode("utf-8")), STDERR_TAIL_BYTES)

    def test_early_eof_reports_the_retained_stderr_tail(self):
        for filename in HARNESSES:
            with self.subTest(harness=filename):
                cls, _ = session_class(filename)
                client = cls.__new__(cls)
                client.p = self.process("eof")
                client.next_id = 1
                client.nonjson_stdout = []
                client.stderr_capture = BoundedStderr(client.p.stderr)
                with self.assertRaisesRegex(RuntimeError, "useful failure detail"):
                    client.rpc("initialize", timeout=3)

    def test_early_eof_without_a_capture_preserves_the_original_diagnostic(self):
        for filename in HARNESSES:
            with self.subTest(harness=filename):
                cls, _ = session_class(filename)
                client = cls.__new__(cls)
                client.p = self.process("eof")
                client.next_id = 1
                client.nonjson_stdout = []
                with self.assertRaisesRegex(RuntimeError, "server closed stdout"):
                    client.rpc("initialize", timeout=3)

    def test_rejects_an_unbounded_configuration(self):
        process = self.process("eof")
        with self.assertRaisesRegex(ValueError, "positive"):
            BoundedStderr(process.stderr, limit=0)


if __name__ == "__main__":
    unittest.main()
