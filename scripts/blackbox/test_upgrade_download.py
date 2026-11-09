"""Offline regressions for release download retries and cache admission."""
import hashlib
import io
import pathlib
import ssl
import tempfile
import unittest
import urllib.error
import zipfile
from unittest import mock

import mcp_upgrade as upgrade


class InterruptedResponse(io.BytesIO):
    def read(self, size=-1):
        if self.tell():
            raise ConnectionResetError("transfer interrupted")
        return super().read(size)


class UpgradeDownloadTests(unittest.TestCase):
    def setUp(self):
        work = tempfile.TemporaryDirectory()
        self.addCleanup(work.cleanup)
        self.root = pathlib.Path(work.name)
        self.dest = self.root / "download"
        open_patch = mock.patch.object(upgrade.urllib.request, "urlopen")
        self.open = open_patch.start()
        self.addCleanup(open_patch.stop)
        sleep_patch = mock.patch.object(upgrade.time, "sleep")
        self.sleep = sleep_patch.start()
        self.addCleanup(sleep_patch.stop)

    @staticmethod
    def http_error(code):
        return urllib.error.HTTPError("https://example.invalid/asset", code, "fixture", {}, io.BytesIO())

    def test_success_downloads_once_without_waiting(self):
        self.open.return_value = io.BytesIO(b"release bytes")
        upgrade._download("https://example.invalid/asset", self.dest)
        self.assertEqual(self.dest.read_bytes(), b"release bytes")
        self.open.assert_called_once_with("https://example.invalid/asset", timeout=180)
        self.sleep.assert_not_called()

    def test_transient_http_errors_retry_and_close_the_error_response(self):
        for code in (408, 500, 502, 503, 504):
            with self.subTest(code=code):
                self.open.reset_mock()
                self.sleep.reset_mock()
                error = self.http_error(code)
                self.open.side_effect = [error, io.BytesIO(b"recovered")]
                upgrade._download("https://example.invalid/asset", self.dest)
                self.assertTrue(error.fp.closed)
                self.assertEqual(self.dest.read_bytes(), b"recovered")
                self.assertEqual(self.open.call_count, 2)
                self.sleep.assert_called_once_with(1)

    def test_three_attempts_preserve_the_last_error(self):
        errors = [self.http_error(504) for _ in range(3)]
        self.open.side_effect = errors
        with self.assertRaises(urllib.error.HTTPError) as raised:
            upgrade._download("https://example.invalid/asset", self.dest)
        self.assertIs(raised.exception, errors[-1])
        self.assertEqual(self.open.call_count, 3)
        self.assertEqual(self.sleep.call_args_list, [mock.call(1), mock.call(2)])

    def test_permanent_errors_do_not_retry(self):
        errors = [self.http_error(403), self.http_error(404),
                  urllib.error.URLError(ssl.SSLCertVerificationError("invalid certificate"))]
        for error in errors:
            with self.subTest(error=error):
                self.open.reset_mock()
                self.sleep.reset_mock()
                self.open.side_effect = error
                with self.assertRaises(type(error)) as raised:
                    upgrade._download("https://example.invalid/asset", self.dest)
                self.assertIs(raised.exception, error)
                self.open.assert_called_once()
                self.sleep.assert_not_called()

    def test_timeouts_and_connection_failures_retry(self):
        for error in (TimeoutError("timed out"), ConnectionResetError("reset"),
                      urllib.error.URLError(TimeoutError("connect timeout"))):
            with self.subTest(error=error):
                self.open.reset_mock()
                self.sleep.reset_mock()
                self.open.side_effect = [error, io.BytesIO(b"recovered")]
                upgrade._download("https://example.invalid/asset", self.dest)
                self.assertEqual(self.dest.read_bytes(), b"recovered")
                self.assertEqual(self.open.call_count, 2)
                self.sleep.assert_called_once_with(1)

    def test_interrupted_transfer_is_replaced_without_retaining_a_prefix(self):
        self.open.side_effect = [InterruptedResponse(b"partial prefix"), io.BytesIO(b"ok")]
        upgrade._download("https://example.invalid/asset", self.dest)
        self.assertEqual(self.dest.read_bytes(), b"ok")
        self.assertEqual(self.open.call_count, 2)

    def test_local_write_failure_is_not_retried(self):
        self.open.return_value = io.BytesIO(b"release bytes")
        with self.assertRaises(OSError):
            upgrade._download("https://example.invalid/asset", self.root)
        self.open.assert_called_once()
        self.sleep.assert_not_called()

    def test_fetch_retries_the_archive_then_caches_verified_bytes(self):
        archive = io.BytesIO()
        with zipfile.ZipFile(archive, "w") as target:
            target.writestr("aoci.exe", b"verified binary")
        payload = archive.getvalue()
        sums = hashlib.sha256(payload).hexdigest().encode() + b"  fixture.zip\n"
        self.open.side_effect = [self.http_error(503), io.BytesIO(payload), io.BytesIO(sums)]
        with mock.patch.object(upgrade, "CACHE", str(self.root)), \
             mock.patch.object(upgrade, "platform_asset", return_value=("fixture.zip", "aoci.exe")):
            result = pathlib.Path(upgrade.fetch_release_binary("v0.0.0"))
            self.assertEqual(result.read_bytes(), b"verified binary")
            self.assertEqual(self.open.call_count, 3)
            self.assertEqual(upgrade.fetch_release_binary("v0.0.0"), str(result))
            self.assertEqual(self.open.call_count, 3)

    def test_fetch_failure_never_publishes_a_cached_binary(self):
        for failure in ("exhausted", "checksum"):
            with self.subTest(failure=failure):
                self.open.reset_mock()
                self.sleep.reset_mock()
                if failure == "exhausted":
                    self.open.side_effect = [self.http_error(504) for _ in range(3)]
                    expected = urllib.error.HTTPError
                    requests = 3
                else:
                    self.open.side_effect = [io.BytesIO(b"archive"), io.BytesIO(b"0" * 64 + b"  fixture.zip\n")]
                    expected = RuntimeError
                    requests = 2
                with mock.patch.object(upgrade, "CACHE", str(self.root)), \
                     mock.patch.object(upgrade, "platform_asset", return_value=("fixture.zip", "aoci.exe")):
                    with self.assertRaises(expected):
                        upgrade.fetch_release_binary("v0.0.0")
                self.assertEqual(self.open.call_count, requests)
                self.assertEqual(list(self.root.iterdir()), [])
                if failure == "checksum":
                    self.sleep.assert_not_called()


if __name__ == "__main__":
    unittest.main()
