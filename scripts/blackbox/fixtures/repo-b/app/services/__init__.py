"""Domain services independent of HTTP and of persistence.

* :mod:`app.services.pricing` -- money arithmetic on ``Decimal``: the
  application-side mirror of the ``products.price_cents`` generated
  column, plus the volume discount tiers applied to order lines.
* :mod:`app.services.inventory` -- an in-process reservation ledger
  that guards order placement against overselling tracked products.

Nothing in this package opens a database connection; callers pass
plain values in and get plain values (or exceptions) back, which
keeps the whole layer unit-testable without a MySQL server. The
pytest suites under ``tests/`` exercise exactly these two modules.
"""

from app.services import inventory, pricing

__all__ = ["inventory", "pricing"]
