"""Wall-clock deadlines for blocking MCP subprocess I/O on every platform."""
from contextlib import contextmanager
from threading import Event, Timer

from stdio_capture import stderr_failure


@contextmanager
def rpc_deadline(process, method, timeout, stderr_capture=None):
    """A timed-out harness session is unusable; kill and reap its server.

    Checking the clock around readline cannot interrupt a silent or partial
    line, and select does not support Windows pipes. Killing the test process
    also releases a blocked stdin write without leaving a reader thread behind.
    """
    expired = Event()

    def expire():
        expired.set()
        process.kill()

    timer = Timer(timeout, expire)
    timer.daemon = True
    timer.start()
    try:
        yield
    finally:
        timer.cancel()
        timer.join()  # A completed request must not leave a timer killing the next one.
        if expired.is_set():
            process.wait(timeout=5)
            message = stderr_failure(stderr_capture, f"timeout waiting for {method}")
            raise TimeoutError(message) from None
