"""Regression coverage for complete and broken Overview cursor chains."""
import ast
import json
from pathlib import Path
import unittest


# The conformance script runs real MCP sessions at import time. Load its actual
# parsing and traversal functions without executing those top-level sessions.
source = Path(__file__).with_name("mcp_conformance.py")
tree = ast.parse(source.read_text(encoding="utf-8"))
nodes = [n for n in tree.body if isinstance(n, ast.FunctionDef)
         or isinstance(n, ast.Assign) and any(
             isinstance(t, ast.Name) and t.id == "MARK" for t in n.targets)]
namespace = {"json": json}
exec(compile(ast.Module(body=nodes, type_ignores=[]), str(source), "exec"), namespace)
overview_chunks = namespace["overview_chunks"]


class Session:
    def __init__(self, metadata):
        self.metadata = metadata
        self.calls = []

    def call(self, tool, args):
        self.calls.append((tool, args))
        meta = self.metadata[len(self.calls) - 1]
        text = json.dumps(meta) + "\n" + namespace["MARK"] + "\nexact body\n"
        return {"result": {"content": [{"type": "text", "text": text}]}}


def chain(count):
    return [{"chunk_count": count, "completed": i == count,
             "next_cursor": f"opaque:{i}:cursor" if i < count else ""}
            for i in range(1, count + 1)]


class OverviewChunksTests(unittest.TestCase):
    def test_reads_every_chunk_and_forwards_exact_cursors(self):
        for count in (1, 11, 12, 16, 50):
            with self.subTest(count=count):
                session = Session(chain(count))
                chunks = list(overview_chunks(session))
                self.assertEqual(len(chunks), count)
                self.assertTrue(chunks[-1][0]["completed"])
                self.assertTrue(all(body == "exact body\n" for _, body in chunks))
                self.assertEqual(session.calls, [("aoci_overview", {})] + [
                    ("aoci_overview", {"cursor": f"opaque:{i}:cursor"})
                    for i in range(1, count)])

    def test_rejects_missing_cursor_without_restarting(self):
        for cursor in (None, "", 42):
            with self.subTest(cursor=cursor):
                session = Session([{"chunk_count": 2, "completed": False,
                                    "next_cursor": cursor}])
                with self.assertRaisesRegex(RuntimeError, "next_cursor"):
                    list(overview_chunks(session))
                self.assertEqual(len(session.calls), 1)

    def test_rejects_immediate_and_cyclic_cursor_repetition(self):
        for cursors in (("a", "a"), ("a", "b", "a")):
            with self.subTest(cursors=cursors):
                session = Session([{"chunk_count": 10, "completed": False,
                                    "next_cursor": c} for c in cursors])
                with self.assertRaisesRegex(RuntimeError, "repeated"):
                    list(overview_chunks(session))
                self.assertEqual(len(session.calls), len(cursors))

    def test_stops_at_declared_count_even_with_unique_cursors(self):
        metadata = chain(12)
        metadata[-1].update(completed=False, next_cursor="one-too-many")
        session = Session(metadata)
        with self.assertRaisesRegex(RuntimeError, "did not complete"):
            list(overview_chunks(session))
        self.assertEqual(len(session.calls), 12)

    def test_rejects_early_completion_and_changed_counts(self):
        for metadata, diagnostic in (([{"chunk_count": 2, "completed": True}], "before"),
                                     (chain(2)[:1] + chain(3)[:1], "changed")):
            with self.subTest(diagnostic=diagnostic):
                with self.assertRaisesRegex(RuntimeError, diagnostic):
                    list(overview_chunks(Session(metadata)))

    def test_rejects_invalid_counts(self):
        for count in (None, 0, -1, True, "16"):
            with self.subTest(count=count):
                with self.assertRaisesRegex(RuntimeError, "positive integer"):
                    list(overview_chunks(Session([{"chunk_count": count}])))


if __name__ == "__main__":
    unittest.main()
