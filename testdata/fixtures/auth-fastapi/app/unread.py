"""Forms FastAPI refuses at startup; the oracle does not import this module."""
from fastapi import APIRouter, Depends, FastAPI

from .security import oauth2

DEPENDENCIES = [Depends(oauth2)]

app = FastAPI()
listed = APIRouter(dependencies=DEPENDENCIES)


@listed.get("/listed")
def read_listed():
    return []


@app.get("/item", dependencies=[oauth2])
def read_item():
    return []


@app.get("/cycle")
def read_cycle(value=Depends(loop_b)):
    return value


def loop_b(value=Depends(loop_c)):
    return value


def loop_c(value=Depends(loop_b)):
    return value


app.include_router(listed)

SETTINGS = {"title": "unread"}
# Keyword unpacking may supply dependencies.
unpacked = FastAPI(**SETTINGS)


@unpacked.get("/unpacked")
def read_unpacked():
    return []


@app.get("/untyped")
def read_untyped(value=Depends()):
    """Depends() without a callable and without an annotation."""
    return value
