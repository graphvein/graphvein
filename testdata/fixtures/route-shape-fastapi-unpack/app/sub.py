"""An app constructed with keyword unpacking, mounted on the plain app."""
from fastapi import FastAPI, HTTPException

from app.responses import APP_OPTIONS
from app.routers import plain

sub = FastAPI(**APP_OPTIONS)


@sub.get("/item/{item_id}")
def read_item(item_id: int) -> dict[str, int]:
    if item_id < 0:
        raise HTTPException(status_code=404)
    return {"id": item_id}


sub.include_router(plain.shared)
