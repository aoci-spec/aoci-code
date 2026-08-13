"""FastAPI application factory and process lifecycle for shopfloor."""

import logging
from contextlib import asynccontextmanager

from fastapi import FastAPI

from app.api import customers, health, orders
from app.db import ping
from app.services.inventory import InventoryLedger
from app.settings import Settings

log = logging.getLogger("shopfloor")


@asynccontextmanager
async def lifespan(application: FastAPI):
    """Prove MySQL is reachable once before the server accepts traffic."""
    settings: Settings = application.state.settings
    ping(settings)
    log.info("database reachable via %s", settings.dsn.rsplit("@", 1)[-1])
    yield
    log.info("shopfloor shutting down")


def create_app(settings: Settings | None = None) -> FastAPI:
    """Assemble the application: process state, routers, lifecycle hooks."""
    application = FastAPI(title="shopfloor", version="1.2.0", lifespan=lifespan)
    application.state.settings = settings or Settings.from_env()
    application.state.inventory = InventoryLedger()
    application.include_router(health.router)
    application.include_router(customers.router, prefix="/v1")
    application.include_router(orders.router, prefix="/v1")
    return application


app = create_app()
