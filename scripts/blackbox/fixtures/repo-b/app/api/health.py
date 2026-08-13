"""Liveness and readiness endpoints."""

from fastapi import APIRouter, Request, Response, status

from app.db import ping

router = APIRouter(tags=["health"])


@router.get("/healthz")
def healthz() -> dict:
    """Process liveness: always cheap, never touches the database."""
    return {"status": "ok"}


@router.get("/readyz")
def readyz(request: Request, response: Response) -> dict:
    """Readiness: proves the MySQL round trip still works.

    Load balancers poll this endpoint; a degraded answer flips the
    instance out of rotation without killing the process.
    """
    settings = request.app.state.settings
    try:
        ping(settings)
    except Exception:  # pragma: no cover - needs a live, then dead, server
        response.status_code = status.HTTP_503_SERVICE_UNAVAILABLE
        return {"status": "degraded", "database": "unreachable"}
    return {"status": "ok", "database": "reachable"}
