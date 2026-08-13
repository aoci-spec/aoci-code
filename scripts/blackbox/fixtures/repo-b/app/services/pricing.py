"""Pricing rules: cents conversion and volume discount tiers."""

from decimal import ROUND_HALF_UP, Decimal

# (minimum quantity, discount as a fraction of list price), best tier first.
DISCOUNT_TIERS: tuple[tuple[int, Decimal], ...] = (
    (100, Decimal("0.15")),
    (25, Decimal("0.10")),
    (10, Decimal("0.05")),
)


def to_price_cents(price: Decimal) -> int:
    """Mirror of the ``products.price_cents`` stored generated column.

    MySQL computes ``ROUND(price * 100)`` with half-up semantics, so
    this helper must round half-up as well; drifting from the server
    formula would make cached carts disagree with the catalog.
    """
    if price < 0:
        raise ValueError("price must be non-negative")
    cents = (price * 100).quantize(Decimal("1"), rounding=ROUND_HALF_UP)
    return int(cents)


def discount_rate(qty: int) -> Decimal:
    """Return the discount fraction earned by ordering ``qty`` units."""
    if qty <= 0:
        raise ValueError("quantity must be positive")
    for threshold, rate in DISCOUNT_TIERS:
        if qty >= threshold:
            return rate
    return Decimal("0")


def line_total(unit_price: Decimal, qty: int) -> Decimal:
    """Extended price of one order line after its tier discount.

    The result is quantized to whole cents (half-up) so that summing
    lines can never produce sub-cent residue in ``orders.total``.
    """
    rate = discount_rate(qty)
    gross = unit_price * qty
    net = gross * (Decimal("1") - rate)
    return net.quantize(Decimal("0.01"), rounding=ROUND_HALF_UP)


def order_total(lines: list[tuple[Decimal, int]]) -> Decimal:
    """Sum of discounted line totals for ``(unit_price, qty)`` pairs.

    Always non-negative, matching the ``CHECK (total >= 0)`` constraint
    on the ``orders`` table.
    """
    return sum((line_total(price, qty) for price, qty in lines), Decimal("0.00"))
