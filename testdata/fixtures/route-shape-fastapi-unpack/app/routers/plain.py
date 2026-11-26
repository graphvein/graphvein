"""Routers without options of their own."""
from fastapi import APIRouter, HTTPException

# Included only into the plain app: not affected by the unpacked objects.
router = APIRouter()


@router.get("/plain/{item_id}")
def read_plain(item_id: int) -> dict[str, int]:
    if item_id < 0:
        raise HTTPException(status_code=404)
    return {"id": item_id}


# Included into the plain app and into the unpacked sub-app.
shared = APIRouter()


@shared.get("/shared")
def read_shared() -> dict[str, str]:
    return {"router": "shared"}


# Included with an explicit default_response_class.
styled = APIRouter()


@styled.get("/page")
def read_page() -> dict[str, str]:
    return {"router": "styled"}
