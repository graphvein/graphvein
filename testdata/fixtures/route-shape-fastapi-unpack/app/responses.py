"""A response class whose default status is 201, an HTTPException handler,
and the option dictionaries the app and a router unpack (row SH-F24,
SH-F37, SH-F38)."""
from fastapi import Request
from fastapi.responses import JSONResponse
from starlette.exceptions import HTTPException as StarletteHTTPException


class CreatedJSON(JSONResponse):
    """JSON sent with status 201 when FastAPI passes no status."""

    def __init__(self, content=None, status_code=201, **kwargs):
        super().__init__(content, status_code=status_code, **kwargs)


async def teapot(request: Request, exc: StarletteHTTPException):
    return JSONResponse({"detail": exc.detail}, status_code=418)


ROUTER_OPTIONS = {"default_response_class": CreatedJSON}
APP_OPTIONS = {"default_response_class": CreatedJSON, "exception_handlers": {StarletteHTTPException: teapot}}
