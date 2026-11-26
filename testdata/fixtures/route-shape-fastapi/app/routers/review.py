"""Forms of the SH-F review: each route is a
class of the review, checked by the spec and runtime oracles."""
from collections.abc import AsyncIterable
from enum import Enum
from typing import Annotated

from fastapi import APIRouter, Depends, Form, HTTPException, Request, Response, UploadFile
from fastapi.responses import JSONResponse, RedirectResponse
from fastapi.routing import APIRoute
from fastapi.security import OAuth2PasswordBearer
from pydantic import AfterValidator, BaseModel, BeforeValidator, ConfigDict, Field, field_validator

from app.models import Item

router = APIRouter()


class Shade(str, Enum):
    red = "red"
    blue = "blue"

    @classmethod
    def _missing_(cls, value):
        return cls(str(value).lower())


class Plain(BaseModel):
    name: str
    x: int | None


class Aliased(BaseModel):
    user_name: str = Field(alias="userName")


class ByName(BaseModel):
    model_config = ConfigDict(populate_by_name=True)

    user_name: str = Field(alias="userName")


class Tags(BaseModel):
    tags: list[str]

    @field_validator("tags", mode="before")
    @classmethod
    def split(cls, value):
        return value.split(",") if isinstance(value, str) else value


class Checked(BaseModel):
    tags: list[str]

    @field_validator("tags")
    @classmethod
    def strip(cls, value):
        return [v.strip() for v in value]


class FormData(BaseModel):
    username: str
    age: int


def make_response():
    return JSONResponse({"a": 1}, status_code=202)


def make_plain():
    return {"name": "a", "x": 1}


@router.get("/url")
def url_read(request: Request):
    return {"q": request.url.query}


@router.get("/state")
def state_read(request: Request):
    return {"user": getattr(request.state, "user", None)}


@router.get("/indirect")
def indirect():
    return make_response()


@router.get("/indirect2", response_model=Plain)
def indirect2():
    return make_response()


@router.get("/proven-plain", response_model=Plain)
def proven_plain():
    return make_plain()


@router.get("/exclnone", response_model=Plain, response_model_exclude_none=True)
def exclude_none():
    return {"name": "a", "x": None}


@router.get("/exclude", response_model=Plain, response_model_exclude={"x"})
def exclude():
    return {"name": "a", "x": 1}


@router.get("/byalias", response_model=Aliased, response_model_by_alias=False)
def by_alias():
    return {"userName": "a"}


@router.get("/unset", response_model=Plain, response_model_exclude_unset=True)
def exclude_unset():
    return {"name": "a", "x": 1}


@router.post("/formmodel")
def form_model(data: Annotated[FormData, Form()]):
    return {"username": data.username}


@router.post("/popname")
def populate(body: ByName):
    return {"name": body.user_name}


@router.get("/redir", response_class=RedirectResponse)
def redirect():
    return "/url"


@router.get("/gen")
def generate():
    yield {"a": 1}


@router.get("/stream-items")
async def stream_items() -> AsyncIterable[Item]:
    yield Item(id=1, name="a", note=None)


@router.get("/bv")
def before_validator(q: Annotated[int, BeforeValidator(len)]):
    return q


@router.get("/av")
def after_validator(q: Annotated[int, AfterValidator(abs)]):
    return q


@router.post("/fv")
def field_before(body: Tags):
    return body.tags


@router.post("/fv-after")
def field_after(body: Checked):
    return body.tags


@router.get("/enum-missing")
def enum_missing(c: Shade):
    return c


@router.get("/opt422")
def optional_only(q: str | None = None):
    return {"q": q}


def after_yield_dep():
    yield 1
    raise HTTPException(status_code=409)


@router.get("/after-yield")
def after_yield(v: Annotated[int, Depends(after_yield_dep)]):
    return v


class CookieOAuth(OAuth2PasswordBearer):
    async def __call__(self, request: Request):
        token = request.cookies.get("access_token")
        if not token:
            raise HTTPException(status_code=401)
        return token


cookie_scheme = CookieOAuth(tokenUrl="/token")


@router.get("/cookie-oauth")
def cookie_oauth(token: Annotated[str, Depends(cookie_scheme)]):
    return token


@router.post("/files-list")
def files_list(files: list[UploadFile]):
    return len(files)


def make_request(request: Request) -> Request:
    return request


@router.get("/reassign")
def reassign(request: Request):
    request = make_request(request)
    return request.headers.get("x-b")


@router.get("/cookie-set", status_code=201)
def cookie_set(response: Response):
    response.set_cookie("seen", "1")
    response.headers["X-Seen"] = "1"
    return {"ok": True}


class CatchAll(APIRoute):
    def get_route_handler(self):
        original = super().get_route_handler()

        async def handler(request):
            try:
                return await original(request)
            except HTTPException:
                return Response(status_code=299)

        return handler


custom = APIRouter(route_class=CatchAll)


@custom.get("/custom")
def custom_route(q: int):
    raise HTTPException(status_code=404)
