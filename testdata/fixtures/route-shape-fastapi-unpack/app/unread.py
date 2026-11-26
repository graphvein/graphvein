"""Not imported by the app: an include_router call with keyword unpacking
(its prefix is not read either) and an include on an object not read."""
from fastapi import APIRouter, FastAPI

INCLUDE_OPTIONS = {"prefix": "/opt"}

other = FastAPI()
opt = APIRouter()


@opt.get("/opt")
def read_opt() -> dict[str, str]:
    return {}


other.include_router(opt, **INCLUDE_OPTIONS)


def build():
    return FastAPI()


orphan = APIRouter()


@orphan.get("/orphan")
def read_orphan() -> dict[str, str]:
    return {}


build().include_router(orphan)
