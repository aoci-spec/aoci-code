"""HTTP endpoints for customer registration and lookup."""

from fastapi import APIRouter, HTTPException, Request, status
from pydantic import BaseModel, Field

from app.repositories import customers

router = APIRouter(prefix="/customers", tags=["customers"])


class CustomerIn(BaseModel):
    """Registration payload; the length caps match the DDL columns."""

    email: str = Field(min_length=3, max_length=190, pattern=r"^[^@\s]+@[^@\s]+$")
    full_name: str = Field(min_length=1, max_length=120)


class CustomerOut(BaseModel):
    id: int
    email: str
    full_name: str


@router.post("", response_model=CustomerOut, status_code=status.HTTP_201_CREATED)
def register(request: Request, payload: CustomerIn) -> CustomerOut:
    """Create a customer; duplicate email answers 409 instead of 500."""
    settings = request.app.state.settings
    if customers.find_by_email(settings, payload.email) is not None:
        raise HTTPException(status.HTTP_409_CONFLICT, "email already registered")
    customer_id = customers.create(settings, payload.email, payload.full_name)
    return CustomerOut(
        id=customer_id, email=payload.email, full_name=payload.full_name
    )


@router.get("/{customer_id}", response_model=CustomerOut)
def fetch(request: Request, customer_id: int) -> CustomerOut:
    """Point lookup by primary key."""
    settings = request.app.state.settings
    found = customers.get(settings, customer_id)
    if found is None:
        raise HTTPException(status.HTTP_404_NOT_FOUND, "customer not found")
    return CustomerOut(id=found.id, email=found.email, full_name=found.full_name)
