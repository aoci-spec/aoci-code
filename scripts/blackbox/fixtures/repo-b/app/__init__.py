"""shopfloor: a small order-management service backed by MySQL.

The package is organised in thin layers:

* :mod:`app.settings` and :mod:`app.db` -- configuration and MySQL
  access (DSN parsing, context-managed cursors, retry-once connect);
* :mod:`app.models` -- dataclasses mirroring the v1 schema tables;
* :mod:`app.repositories` -- parameterized SQL per aggregate root;
* :mod:`app.services` -- pricing and inventory domain logic;
* :mod:`app.api` -- FastAPI routers over the layers above.

Deployment entrypoints import the factory from here
(``uvicorn "app:create_app" --factory``) or use the ready-made
module-level instance ``app.main:app``.
"""

from app.main import create_app

__all__ = ["create_app"]
