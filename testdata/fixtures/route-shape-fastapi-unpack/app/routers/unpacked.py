"""A router constructed with keyword unpacking, and a router included
into it (it inherits the unpacked default_response_class)."""
from fastapi import APIRouter

from app.responses import ROUTER_OPTIONS

router = APIRouter(**ROUTER_OPTIONS)


@router.get("/made")
def make() -> dict[str, str]:
    return {"router": "unpacked"}


nested = APIRouter()


@nested.get("/nested")
def read_nested() -> dict[str, str]:
    return {"router": "nested"}


router.include_router(nested)
