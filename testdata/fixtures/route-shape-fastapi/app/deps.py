from typing import Annotated

from fastapi import Depends, Header, Query, Request, Security

from app.helpers import read_token
from app.models import Color
from app.security import oauth2


def pagination(skip: int = 0, limit: int = Query(100, le=500)) -> dict:
    return {"skip": skip, "limit": limit}


PageDep = Annotated[dict, Depends(pagination)]
TokenHeader = Annotated[str, Header(alias="X-Token")]


def item_owner(item_id: int, x_owner: Annotated[str, Header()]) -> str:
    return x_owner


def common(page: PageDep, token: TokenHeader) -> dict:
    return page


def reader(request: Request) -> str | None:
    session = request.cookies["session"]
    return read_token(request) or session


def current_user(token: Annotated[str, Depends(oauth2)]) -> str:
    return token


def scoped_user(token: Annotated[str, Security(oauth2, scopes=["items:read"])]) -> str:
    return token


class Filters:
    def __init__(self, q: str | None = None, color: Color = Color.red):
        self.q = q
        self.color = color


def level_one(a: int, nxt: Annotated[int, Depends(lambda: 1)] = 0) -> int:
    return a


def level_four(d: int) -> int:
    return d


def level_three(c: int, nxt: Annotated[int, Depends(level_four)]) -> int:
    return c


def level_two(b: int, nxt: Annotated[int, Depends(level_three)]) -> int:
    return b


def level_root(z: int, nxt: Annotated[int, Depends(level_two)]) -> int:
    return z
