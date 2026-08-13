"""Runtime configuration for shopfloor, sourced from the environment."""

import os
from urllib.parse import urlparse

from pydantic import BaseModel, Field

DEFAULT_DSN = "mysql://shopfloor:shopfloor@localhost:3306/shopfloor"


class Settings(BaseModel):
    """Validated process configuration.

    The MySQL DSN comes from ``SHOP_DATABASE_DSN`` and follows the
    ``mysql://user:password@host:port/database`` shape. HTTP bind
    options come from ``SHOP_BIND_HOST`` / ``SHOP_BIND_PORT``.
    """

    dsn: str = Field(default=DEFAULT_DSN)
    host: str = Field(default="0.0.0.0")
    port: int = Field(default=8000, ge=1, le=65535)
    connect_timeout: float = Field(default=5.0, gt=0)

    @classmethod
    def from_env(cls) -> "Settings":
        """Build settings from process environment, falling back to defaults."""
        return cls(
            dsn=os.environ.get("SHOP_DATABASE_DSN", DEFAULT_DSN),
            host=os.environ.get("SHOP_BIND_HOST", "0.0.0.0"),
            port=int(os.environ.get("SHOP_BIND_PORT", "8000")),
        )

    def mysql_kwargs(self) -> dict:
        """Translate the DSN into ``mysql.connector.connect`` keyword args."""
        parts = urlparse(self.dsn)
        if parts.scheme != "mysql":
            raise ValueError(f"unsupported DSN scheme: {parts.scheme!r}")
        if not parts.path or parts.path == "/":
            raise ValueError("DSN must name a database, e.g. .../shopfloor")
        return {
            "user": parts.username or "shopfloor",
            "password": parts.password or "",
            "host": parts.hostname or "localhost",
            "port": parts.port or 3306,
            "database": parts.path.lstrip("/"),
            "connection_timeout": self.connect_timeout,
        }
