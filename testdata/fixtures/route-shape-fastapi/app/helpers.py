import json

from fastapi import HTTPException, Request
from starlette.datastructures import Headers

from app.plain import untyped_reader


def read_token(request: Request) -> str | None:
    return token_from(request.headers)


def token_from(headers: Headers) -> str | None:
    auth = headers.get("Authorization")
    if auth is None:
        raise HTTPException(status_code=401, detail="missing token")
    return auth


def deep_one(request: Request) -> str:
    return deep_two(request)


def deep_two(request: Request) -> str:
    return deep_three(request)


def deep_three(request: Request) -> str:
    return request.query_params.get("deep", "")


def ping(request: Request) -> str:
    return pong(request)


def pong(request: Request) -> str:
    return ping(request)


def spread(**kwargs) -> dict:
    return kwargs


def dumped(request: Request) -> str:
    return json.dumps(dict(request.headers))


def via_plain(request: Request) -> str:
    return untyped_reader(request)


def twice() -> int:
    return 1


def twice() -> int:  # noqa: F811 - redefined on purpose (ambiguous target)
    return 2
