"""HTTP endpoints for order placement and retrieval."""

from decimal import Decimal

from fastapi import APIRouter, HTTPException, Request, status
from pydantic import BaseModel, Field

from app.repositories import customers, orders, products
from app.services import pricing
from app.services.inventory import InsufficientStock, InventoryLedger

router = APIRouter(prefix="/orders", tags=["orders"])


class LineIn(BaseModel):
    sku: str = Field(min_length=1, max_length=64)
    qty: int = Field(gt=0)


class OrderIn(BaseModel):
    customer_id: int = Field(gt=0)
    external_ref: str = Field(min_length=1, max_length=64)
    lines: list[LineIn] = Field(min_length=1)


class OrderOut(BaseModel):
    id: int
    status: str
    total: Decimal


def _reserve_all(
    ledger: InventoryLedger, items: list[tuple[int, int, Decimal]]
) -> list[tuple[int, int]]:
    """Reserve every line, releasing earlier holds if one falls short."""
    held: list[tuple[int, int]] = []
    try:
        for product_id, qty, _price in items:
            ledger.reserve(product_id, qty)
            held.append((product_id, qty))
    except InsufficientStock:
        for product_id, qty in held:
            ledger.release(product_id, qty)
        raise
    return held


@router.post("", response_model=OrderOut, status_code=status.HTTP_201_CREATED)
def place(request: Request, payload: OrderIn) -> OrderOut:
    """Resolve SKUs, reserve stock, price the lines, persist the order."""
    settings = request.app.state.settings
    ledger: InventoryLedger = request.app.state.inventory
    if customers.get(settings, payload.customer_id) is None:
        raise HTTPException(status.HTTP_404_NOT_FOUND, "customer not found")
    items: list[tuple[int, int, Decimal]] = []
    for line in payload.lines:
        product = products.find_by_sku(settings, line.sku)
        if product is None:
            raise HTTPException(
                status.HTTP_422_UNPROCESSABLE_ENTITY, f"unknown sku {line.sku}"
            )
        items.append((product.id, line.qty, product.price))
    try:
        held = _reserve_all(ledger, items)
    except InsufficientStock as exc:
        raise HTTPException(status.HTTP_409_CONFLICT, str(exc)) from exc
    total = pricing.order_total([(price, qty) for _pid, qty, price in items])
    try:
        order_id = orders.create_with_items(
            settings, payload.customer_id, payload.external_ref, total, items
        )
    except Exception:
        for product_id, qty in held:
            ledger.release(product_id, qty)
        raise
    return OrderOut(id=order_id, status="new", total=total)


@router.get("/{order_id}", response_model=OrderOut)
def fetch(request: Request, order_id: int) -> OrderOut:
    """Point lookup of the order header."""
    settings = request.app.state.settings
    found = orders.get(settings, order_id)
    if found is None:
        raise HTTPException(status.HTTP_404_NOT_FOUND, "order not found")
    return OrderOut(id=found.id, status=found.status, total=found.total)
