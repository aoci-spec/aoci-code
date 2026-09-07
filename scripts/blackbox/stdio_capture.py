"""Continuously drain MCP stderr while retaining a bounded diagnostic tail."""
from threading import Lock, Thread


STDERR_TAIL_BYTES = 16 * 1024


class BoundedStderr:
    """Drain a subprocess pipe in the background and retain its latest bytes."""

    def __init__(self, stream, limit=STDERR_TAIL_BYTES):
        if limit < 1:
            raise ValueError("stderr limit must be positive")
        self._reader = getattr(stream, "buffer", stream)
        self._limit = limit
        self._tail = bytearray()
        self._lock = Lock()
        self._thread = Thread(target=self._drain, name="mcp-stderr", daemon=True)
        self._thread.start()

    def _drain(self):
        read = getattr(self._reader, "read1", self._reader.read)
        try:
            while True:
                chunk = read(4096)
                if not chunk:
                    return
                if isinstance(chunk, str):
                    chunk = chunk.encode("utf-8", errors="replace")
                with self._lock:
                    self._tail.extend(chunk)
                    if len(self._tail) > self._limit:
                        del self._tail[:-self._limit]
        except (OSError, ValueError):
            # Process teardown may close the pipe while this daemon is reading.
            return

    def finish(self, timeout=1):
        """Give an exited process's pipe reader time to consume its final bytes."""
        self._thread.join(timeout)

    def text(self):
        with self._lock:
            tail = bytes(self._tail)
        return tail.decode("utf-8", errors="replace")

    def failure(self, message):
        self.finish(timeout=0.2)
        tail = self.text().strip()
        return message if not tail else f"{message}; stderr tail:\n{tail}"


def stderr_failure(capture, message):
    """Add captured diagnostics when a session owns a stderr collector."""
    return message if capture is None else capture.failure(message)
