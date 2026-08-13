"""Unit tests for the inventory reservation ledger."""

import pytest

from app.services.inventory import InsufficientStock, InventoryLedger


@pytest.fixture()
def ledger() -> InventoryLedger:
    led = InventoryLedger()
    led.set_on_hand(101, 5)
    return led


def test_reserve_reduces_availability(ledger):
    ledger.reserve(101, 3)
    assert ledger.available(101) == 2


def test_reserve_beyond_available_raises(ledger):
    ledger.reserve(101, 5)
    with pytest.raises(InsufficientStock) as exc_info:
        ledger.reserve(101, 1)
    assert exc_info.value.product_id == 101
    assert exc_info.value.available == 0


def test_failed_reserve_leaves_holds_untouched(ledger):
    ledger.reserve(101, 4)
    with pytest.raises(InsufficientStock):
        ledger.reserve(101, 2)
    assert ledger.available(101) == 1


def test_untracked_product_is_unconstrained(ledger):
    ledger.reserve(999, 1000)  # never tracked: no error, nothing recorded
    assert ledger.available(999) is None


def test_release_returns_stock_to_pool(ledger):
    ledger.reserve(101, 4)
    ledger.release(101, 4)
    assert ledger.available(101) == 5


def test_commit_consumes_on_hand_and_reservation(ledger):
    ledger.reserve(101, 2)
    ledger.commit(101, 2)
    assert ledger.available(101) == 3


def test_recount_overwrites_on_hand_but_keeps_holds(ledger):
    ledger.reserve(101, 2)
    ledger.set_on_hand(101, 10)
    assert ledger.available(101) == 8


def test_set_on_hand_rejects_negative():
    with pytest.raises(ValueError):
        InventoryLedger().set_on_hand(1, -1)


def test_reserve_rejects_non_positive_quantity(ledger):
    with pytest.raises(ValueError):
        ledger.reserve(101, 0)
