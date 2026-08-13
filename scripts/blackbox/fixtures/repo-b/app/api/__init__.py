"""HTTP layer: FastAPI routers, one module per resource.

Modules here translate between wire types (pydantic request/response
models) and the dataclasses returned by :mod:`app.repositories`.
Conventions shared by every router:

* routers never build SQL -- persistence stays behind the repository
  layer, domain math stays behind :mod:`app.services`;
* process state (settings, the inventory ledger) is read from
  ``request.app.state``, so the same modules serve every application
  instance produced by :func:`app.main.create_app`;
* error mapping is local: lookups translate ``None`` into 404,
  duplicate registrations into 409, stock shortfalls into 409.
"""

from app.api import customers, health, orders

__all__ = ["customers", "health", "orders"]
