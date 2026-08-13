"""Repository layer: one module per aggregate root.

Every function takes :class:`app.settings.Settings` as its first
argument and goes through :func:`app.db.cursor`, so transaction scope
and connection cleanup are uniform across the layer. Two invariants
hold everywhere:

* SQL text is constant -- values travel only through ``%s``
  placeholders, never through string interpolation;
* row dicts from the driver are mapped into the dataclasses declared
  in :mod:`app.models` before they leave this package, so callers
  never see driver-specific shapes.
"""

from app.repositories import customers, orders, products

__all__ = ["customers", "orders", "products"]
