"""Persistence for the orders aggregate, including line items."""

from decimal import Decimal

from app.db import cursor
from app.models import Order, OrderItem
from app.settings import Settings

_ORDER_INSERT = (
    "INSERT INTO orders (customer_id, external_ref, status, total) "
    "VALUES (%s, %s, %s, %s)"
)
_ITEM_INSERT = (
    "INSERT INTO order_items (order_id, line_no, product_id, qty, unit_price) "
    "VALUES (%s, %s, %s, %s, %s)"
)


def create_with_items(
    settings: Settings,
    customer_id: int,
    external_ref: str,
    total: Decimal,
    items: list[tuple[int, int, Decimal]],
) -> int:
    """Insert an order and its lines in one transaction; return the id.

    ``items`` holds ``(product_id, qty, unit_price)`` tuples; line
    numbers are assigned from 1 in list order to satisfy the composite
    primary key on ``order_items``. The unique key on
    ``(customer_id, external_ref)`` makes retries of the same upstream
    reference fail loudly instead of double-charging.
    """
    with cursor(settings, commit=True) as cur:
        cur.execute(_ORDER_INSERT, (customer_id, external_ref, "new", total))
        order_id = cur.lastrowid
        for line_no, (product_id, qty, unit_price) in enumerate(items, start=1):
            cur.execute(_ITEM_INSERT, (order_id, line_no, product_id, qty, unit_price))
    return order_id


def get(settings: Settings, order_id: int) -> Order | None:
    """Primary-key lookup of the order header."""
    with cursor(settings) as cur:
        cur.execute(
            "SELECT id, customer_id, external_ref, status, total, created_at "
            "FROM orders WHERE id = %s",
            (order_id,),
        )
        row = cur.fetchone()
    return Order(**row) if row else None


def list_items(settings: Settings, order_id: int) -> list[OrderItem]:
    """All lines of one order in line-number order."""
    with cursor(settings) as cur:
        cur.execute(
            "SELECT order_id, line_no, product_id, qty, unit_price "
            "FROM order_items WHERE order_id = %s ORDER BY line_no",
            (order_id,),
        )
        rows = cur.fetchall()
    return [OrderItem(**row) for row in rows]


def set_status(settings: Settings, order_id: int, status: str) -> bool:
    """Move an order through its lifecycle; ``False`` if the id is unknown."""
    with cursor(settings, commit=True) as cur:
        cur.execute("UPDATE orders SET status = %s WHERE id = %s", (status, order_id))
        return cur.rowcount == 1
