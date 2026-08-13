"""Persistence for the customers aggregate."""

from app.db import cursor
from app.models import Customer
from app.settings import Settings

_SELECT = "SELECT id, email, full_name, created_at FROM customers"


def _to_model(row: dict) -> Customer:
    return Customer(
        id=row["id"],
        email=row["email"],
        full_name=row["full_name"],
        created_at=row["created_at"],
    )


def get(settings: Settings, customer_id: int) -> Customer | None:
    """Primary-key lookup; ``None`` when the id is unknown."""
    with cursor(settings) as cur:
        cur.execute(_SELECT + " WHERE id = %s", (customer_id,))
        row = cur.fetchone()
    return _to_model(row) if row else None


def find_by_email(settings: Settings, email: str) -> Customer | None:
    """Point lookup through the unique index on ``email``."""
    with cursor(settings) as cur:
        cur.execute(_SELECT + " WHERE email = %s", (email,))
        row = cur.fetchone()
    return _to_model(row) if row else None


def create(settings: Settings, email: str, full_name: str) -> int:
    """Insert a customer and return the generated id.

    Uniqueness of ``email`` is enforced by the database; callers that
    want a friendly error should probe :func:`find_by_email` first.
    """
    with cursor(settings, commit=True) as cur:
        cur.execute(
            "INSERT INTO customers (email, full_name) VALUES (%s, %s)",
            (email, full_name),
        )
        return cur.lastrowid


def recent(settings: Settings, limit: int = 50) -> list[Customer]:
    """Most recently registered customers, newest first."""
    with cursor(settings) as cur:
        cur.execute(_SELECT + " ORDER BY created_at DESC, id DESC LIMIT %s", (limit,))
        rows = cur.fetchall()
    return [_to_model(row) for row in rows]
