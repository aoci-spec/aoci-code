"""MySQL access helpers: connections, context-managed cursors, one retry."""

import logging
from contextlib import contextmanager

import mysql.connector
from mysql.connector import Error as MySQLError

from app.settings import Settings

log = logging.getLogger("shopfloor.db")


def connect(settings: Settings):
    """Open a MySQL connection described by the settings DSN.

    A transient failure (server restarting, connection reset by a
    failover) is retried exactly once before the error propagates.
    """
    kwargs = settings.mysql_kwargs()
    try:
        return mysql.connector.connect(**kwargs)
    except MySQLError as exc:
        log.warning("connect to %s failed (%s); retrying once", kwargs["host"], exc)
        return mysql.connector.connect(**kwargs)


@contextmanager
def cursor(settings: Settings, *, commit: bool = False):
    """Yield a dictionary cursor; commit or roll back, then clean up.

    Read paths use the default ``commit=False``; write paths pass
    ``commit=True`` so every statement issued inside the block lands in
    a single transaction.
    """
    conn = connect(settings)
    cur = conn.cursor(dictionary=True)
    try:
        yield cur
        if commit:
            conn.commit()
    except Exception:
        conn.rollback()
        raise
    finally:
        cur.close()
        conn.close()


def ping(settings: Settings) -> bool:
    """Round-trip ``SELECT 1`` to prove the database is reachable."""
    with cursor(settings) as cur:
        cur.execute("SELECT 1")
        cur.fetchone()
    return True
