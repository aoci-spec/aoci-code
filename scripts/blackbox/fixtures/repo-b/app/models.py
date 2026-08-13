"""Dataclasses mirroring the five v1 tables declared in schema/init.sql."""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime
from decimal import Decimal
from typing import Any


@dataclass(frozen=True)
class Customer:
    """Row of ``customers``; ``email`` is unique across the table."""

    id: int
    email: str
    full_name: str
    created_at: datetime


@dataclass(frozen=True)
class Product:
    """Row of ``products``; the cents/upper-case columns are DB-generated."""

    id: int
    sku: str
    name: str
    price: Decimal
    price_cents: int
    name_upper: str


@dataclass(frozen=True)
class Order:
    """Row of ``orders``; ``(customer_id, external_ref)`` is unique."""

    id: int
    customer_id: int
    external_ref: str
    status: str
    total: Decimal
    created_at: datetime


@dataclass(frozen=True)
class OrderItem:
    """Row of ``order_items``; keyed by ``(order_id, line_no)``."""

    order_id: int
    line_no: int
    product_id: int
    qty: int
    unit_price: Decimal


@dataclass(frozen=True)
class Event:
    """Row of ``events``; range-partitioned by ``event_year`` server-side."""

    id: int
    event_year: int
    kind: str
    payload: dict[str, Any] | None = field(default=None)
