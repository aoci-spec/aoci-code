"""In-process stock reservation guarding order placement."""

from dataclasses import dataclass
from threading import Lock


class InsufficientStock(RuntimeError):
    """Raised when a reservation would exceed the available quantity."""

    def __init__(self, product_id: int, requested: int, available: int):
        super().__init__(
            f"product {product_id}: requested {requested}, available {available}"
        )
        self.product_id = product_id
        self.requested = requested
        self.available = available


@dataclass
class StockLevel:
    """Tracked quantity for one product: physical stock minus holds."""

    on_hand: int
    reserved: int = 0

    @property
    def available(self) -> int:
        return self.on_hand - self.reserved


class InventoryLedger:
    """Thread-safe reservation ledger keyed by product id.

    Products are opt-in: a quantity exists only after ``set_on_hand``.
    Untracked products are treated as unconstrained, so the ordering
    API keeps working for catalog entries without stock counts.
    """

    def __init__(self) -> None:
        self._levels: dict[int, StockLevel] = {}
        self._lock = Lock()

    def set_on_hand(self, product_id: int, on_hand: int) -> None:
        """Record the physical count, e.g. after a warehouse recount."""
        if on_hand < 0:
            raise ValueError("on_hand must be non-negative")
        with self._lock:
            level = self._levels.setdefault(product_id, StockLevel(on_hand=0))
            level.on_hand = on_hand

    def available(self, product_id: int) -> int | None:
        """Sellable quantity, or ``None`` for untracked products."""
        with self._lock:
            level = self._levels.get(product_id)
            return None if level is None else level.available

    def reserve(self, product_id: int, qty: int) -> None:
        """Place a hold; raises :class:`InsufficientStock` on shortfall."""
        if qty <= 0:
            raise ValueError("qty must be positive")
        with self._lock:
            level = self._levels.get(product_id)
            if level is None:
                return
            if level.available < qty:
                raise InsufficientStock(product_id, qty, level.available)
            level.reserved += qty

    def release(self, product_id: int, qty: int) -> None:
        """Undo part of a hold, e.g. after a failed order insert."""
        with self._lock:
            level = self._levels.get(product_id)
            if level is not None:
                level.reserved = max(0, level.reserved - qty)

    def commit(self, product_id: int, qty: int) -> None:
        """Turn a hold into a shipment: stock physically leaves."""
        with self._lock:
            level = self._levels.get(product_id)
            if level is not None:
                level.reserved = max(0, level.reserved - qty)
                level.on_hand = max(0, level.on_hand - qty)
