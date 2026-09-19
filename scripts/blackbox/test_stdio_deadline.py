"""Exercise every harness RPC client against real, deliberately slow pipes."""
import ast
import pathlib
import subprocess
import sys
import time
import unittest

from stdio_capture import BoundedStderr


HARNESS_DIR = pathlib.Path(__file__).resolve().parent
HARNESSES = ("mcp_conformance.py", "mcp_scenarios.py", "mcp_upgrade.py")
# The slow server sleeps far longer than any deadline below, and the elapsed-time
# bound sits far below that sleep. Both margins used to be under a second (a 1 s
# sleep against a 0.1 s deadline, and "done within 0.8 s"), which a loaded Windows
# runner broke twice in three runs: the timer thread fired more than 0.9 s late, so
# the response arrived first and no TimeoutError was raised, or killing and
# reaping the child alone took 2.84 s. What the tests prove is unchanged: the
# deadline interrupts the call long before the server would have answered, and
# the timed-out server is reaped. A timed-out child is killed, never waited for.
SLOW_SERVER_SECONDS = 30
INTERRUPTED_WITHIN_SECONDS = 10
SERVER = r'''
import json, sys, time
mode = sys.argv[1]
SLOW = float(sys.argv[2])
if mode == "blocked_write":
    time.sleep(SLOW)
for line in sys.stdin:
    request = json.loads(line)
    response = json.dumps({"jsonrpc": "2.0", "id": request["id"], "result": {}})
    if mode == "eof":
        break
    if mode == "partial":
        sys.stdout.write(response[:5]); sys.stdout.flush()
        time.sleep(SLOW)
        print(response[5:], flush=True)
    else:
        if mode == "noise":
            print('\nnot-json\n{"jsonrpc":"2.0","method":"notice"}', flush=True)
        if mode == "stderr_timeout":
            print("deadline diagnostic", file=sys.stderr, flush=True)
        if mode in ("silent", "noise", "stderr_timeout"):
            time.sleep(SLOW)
        print(response, flush=True)
'''


def session_class(filename):
    # Conformance runs its suite at import time. Load its actual client and
    # imports without starting a real AOCI suite or constructing fixture trees.
    tree = ast.parse((HARNESS_DIR / filename).read_text(encoding="utf-8"))
    nodes = [node for node in tree.body
             if isinstance(node, (ast.Import, ast.ImportFrom))
             or isinstance(node, ast.ClassDef) and node.name == "Session"]
    namespace = {}
    exec(compile(ast.Module(body=nodes, type_ignores=[]), filename, "exec"), namespace)
    return namespace["Session"]


class RPCDeadlineTests(unittest.TestCase):
    def client(self, filename, mode):
        process = subprocess.Popen([sys.executable, "-u", "-c", SERVER, mode, str(SLOW_SERVER_SECONDS)],
                                   stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                   stderr=subprocess.PIPE, text=True, encoding="utf-8")
        self.addCleanup(self.close_process, process)
        cls = session_class(filename)
        client = cls.__new__(cls)
        client.p, client.next_id, client.nonjson_stdout = process, 1, []
        client.stderr_capture = BoundedStderr(process.stderr)
        return client

    @staticmethod
    def close_process(process):
        if process.poll() is None:
            process.kill()
        process.wait(timeout=5)
        for pipe in (process.stdin, process.stdout, process.stderr):
            pipe.close()

    def test_silent_partial_and_noisy_servers_obey_the_rpc_deadline(self):
        for filename in HARNESSES:
            for mode in ("silent", "partial", "noise"):
                with self.subTest(harness=filename, mode=mode):
                    client = self.client(filename, mode)
                    started = time.monotonic()
                    with self.assertRaisesRegex(TimeoutError, "initialize"):
                        client.rpc("initialize", timeout=0.1)
                    self.assertLess(time.monotonic() - started, INTERRUPTED_WITHIN_SECONDS)
                    self.assertIsNotNone(client.p.poll(), "timed-out server must be reaped")

    def test_deadline_also_interrupts_a_blocked_request_write(self):
        for filename in HARNESSES:
            with self.subTest(harness=filename):
                client = self.client(filename, "blocked_write")
                started = time.monotonic()
                with self.assertRaisesRegex(TimeoutError, "tools/call"):
                    client.rpc("tools/call", {"payload": "x" * 1_000_000}, timeout=0.1)
                self.assertLess(time.monotonic() - started, INTERRUPTED_WITHIN_SECONDS)
                self.assertIsNotNone(client.p.poll())

    def test_timeout_preserves_the_stderr_tail(self):
        for filename in HARNESSES:
            with self.subTest(harness=filename):
                client = self.client(filename, "stderr_timeout")
                with self.assertRaisesRegex(
                        TimeoutError,
                        r"timeout waiting for initialize; stderr tail:\ndeadline diagnostic"):
                    client.rpc("initialize", timeout=0.1)

    def test_successful_requests_leave_the_session_reusable(self):
        for filename in HARNESSES:
            with self.subTest(harness=filename):
                client = self.client(filename, "normal")
                self.assertEqual(client.rpc("initialize", timeout=3)["id"], 1)
                self.assertEqual(client.rpc("tools/list", timeout=0.2)["id"], 2)
                time.sleep(0.3)  # The completed request's timer must be cancelled.
                self.assertEqual(client.rpc("tools/list", timeout=3)["id"], 3)
                self.assertIsNone(client.p.poll())

    def test_early_eof_preserves_the_server_closed_diagnostic(self):
        for filename in HARNESSES:
            with self.subTest(harness=filename):
                client = self.client(filename, "eof")
                with self.assertRaisesRegex(RuntimeError, "server closed stdout"):
                    client.rpc("initialize", timeout=3)


if __name__ == "__main__":
    unittest.main()
