"""Unit tests for app.services.pricing."""

from decimal import Decimal

import pytest

from app.services.pricing import (
    discount_rate,
    line_total,
    order_total,
    to_price_cents,
)


def test_to_price_cents_matches_generated_column_rounding():
    # MySQL ROUND() is half-up: 19.995 must become 2000, never 1999.
    assert to_price_cents(Decimal("19.99")) == 1999
    assert to_price_cents(Decimal("19.995")) == 2000
    assert to_price_cents(Decimal("0")) == 0


def test_to_price_cents_rejects_negative_price():
    with pytest.raises(ValueError):
        to_price_cents(Decimal("-0.01"))


@pytest.mark.parametrize(
    ("qty", "rate"),
    [
        (1, "0"),
        (9, "0"),
        (10, "0.05"),
        (24, "0.05"),
        (25, "0.10"),
        (99, "0.10"),
        (100, "0.15"),
        (5000, "0.15"),
    ],
)
def test_discount_tier_boundaries(qty, rate):
    assert discount_rate(qty) == Decimal(rate)


def test_discount_rate_rejects_non_positive_quantity():
    with pytest.raises(ValueError):
        discount_rate(0)


def test_line_total_applies_tier_and_rounds_to_cents():
    # 10 units at 9.99 earn 5% off: 99.90 * 0.95 = 94.905 -> 94.91 half-up.
    assert line_total(Decimal("9.99"), 10) == Decimal("94.91")


def test_line_total_without_tier_is_plain_multiplication():
    assert line_total(Decimal("1.50"), 2) == Decimal("3.00")


def test_order_total_sums_discounted_lines():
    lines = [(Decimal("9.99"), 10), (Decimal("1.50"), 2)]
    assert order_total(lines) == Decimal("97.91")


def test_order_total_of_no_lines_is_zero():
    assert order_total([]) == Decimal("0.00")
