"""Persistence for the product catalog."""

from decimal import Decimal

from app.db import cursor
from app.models import Product
from app.settings import Settings

_SELECT = "SELECT id, sku, name, price, price_cents, name_upper FROM products"


def _to_model(row: dict) -> Product:
    return Product(
        id=row["id"],
        sku=row["sku"],
        name=row["name"],
        price=Decimal(str(row["price"])),
        price_cents=row["price_cents"],
        name_upper=row["name_upper"],
    )


def find_by_sku(settings: Settings, sku: str) -> Product | None:
    """Point lookup through ``uq_products_sku``."""
    with cursor(settings) as cur:
        cur.execute(_SELECT + " WHERE sku = %s", (sku,))
        row = cur.fetchone()
    return _to_model(row) if row else None


def search_by_name(settings: Settings, prefix: str, limit: int = 20) -> list[Product]:
    """Name prefix search; served by ``idx_products_name`` once v2 flips
    that index visible again."""
    with cursor(settings) as cur:
        cur.execute(
            _SELECT + " WHERE name LIKE CONCAT(%s, '%%') ORDER BY name LIMIT %s",
            (prefix, limit),
        )
        rows = cur.fetchall()
    return [_to_model(row) for row in rows]


def create(settings: Settings, sku: str, name: str, price: Decimal) -> int:
    """Insert a product; ``price_cents`` and ``name_upper`` are computed
    by the server, so only the three natural columns are written."""
    with cursor(settings, commit=True) as cur:
        cur.execute(
            "INSERT INTO products (sku, name, price) VALUES (%s, %s, %s)",
            (sku, name, price),
        )
        return cur.lastrowid
