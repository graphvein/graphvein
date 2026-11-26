"""Project-level exception handlers (row SH-F37): they rewrite the raised
statuses and the 422 of every route. The manifest names no FastAPI
version, so return annotations stay `returned` (row SH-F26)."""
from fastapi import FastAPI, HTTPException, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
from pydantic import BaseModel, Field
from starlette.exceptions import HTTPException as StarletteHTTPException

app = FastAPI()


class Item(BaseModel):
    name: str


@app.exception_handler(StarletteHTTPException)
async def http_handler(request: Request, exc: StarletteHTTPException):
    return JSONResponse({"error": exc.detail}, status_code=200)


async def validation_handler(request: Request, exc: RequestValidationError):
    return JSONResponse({"error": "bad"}, status_code=400)


async def teapot_handler(request: Request, exc: HTTPException):
    return JSONResponse({"error": "teapot"}, status_code=200)


async def any_handler(request: Request, exc: Exception):
    return JSONResponse({"error": "server"}, status_code=500)


app.add_exception_handler(RequestValidationError, validation_handler)
app.add_exception_handler(418, teapot_handler)
app.add_exception_handler(Exception, any_handler)


@app.get("/missing")
def missing(q: int) -> Item:
    raise HTTPException(status_code=404)


@app.get("/teapot")
def teapot(q: int):
    raise HTTPException(status_code=418)


@app.get("/annotated")
def annotated() -> Item:
    return Item(name="a")


# Imported here so the routes above keep their lines.
from typing import Optional  # noqa: E402


class Person(BaseModel):
    """Row SH-F22 without a Pydantic version in the manifest: Pydantic 1
    gives a field that accepts None and has no default a None default, so
    `nick` and `tag` have presence unknown; `count` (`...`) and `name` are
    required under both majors."""

    name: str
    nick: Optional[str]
    tag: str | None = Field(alias="t")
    count: int | None = ...


@app.post("/people")
def add_person(p: Person):
    return {"name": p.name}
