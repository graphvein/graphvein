from typing import Annotated, Literal

from fastapi import APIRouter, Body, Cookie, Depends, File, Form, Header, HTTPException, Path, Query, Request, Response, UploadFile, status
from fastapi.encoders import jsonable_encoder
from fastapi.responses import HTMLResponse, JSONResponse, StreamingResponse
from fastapi.security import OAuth2PasswordRequestForm
from pydantic import Field

from app import crud
from app.deps import Filters, common, current_user, item_owner, level_one, level_root, pagination, reader, scoped_user
from app.helpers import deep_one, dumped, ping, spread, twice, via_plain
from app.models import Color, Computed, Generated, Item, ItemCreate, Message, Owner, Reshaped, Serialized
from app.security import api_key, basic, bearer, computed_url, oidc, optional_key

router = APIRouter()


@router.get("/items/{item_id}", response_model=Item)
def read_item(item_id: int, q: str | None = None, verbose: bool = False):
    if item_id == 0:
        raise HTTPException(status_code=404, detail="Item not found")
    return crud.get_item(item_id)


@router.get("/files/{file_path:path}/{version:int}")
def read_file():
    return {"ok": True}


@router.post("/items", status_code=status.HTTP_201_CREATED, response_model=Item, responses={404: {"model": Message}, 418: {"description": "teapot"}})
def create_item(item: ItemCreate):
    return Item(id=1, name=item.name, note=item.note)


@router.post("/embed")
def embed(item: Annotated[Item, Body(embed=True)]) -> Message:
    return Message(message=item.name)


@router.post("/multi")
def multi(item: Item, other: ItemCreate, importance: int = Body(), ids: list[int] | None = None) -> dict[str, int]:
    return {"importance": importance}


@router.post("/ids")
def ids(message_ids: list[int]) -> list[Item]:
    return []


@router.post("/form")
def form(name: Annotated[str, Form()], age: int = Form(18)):
    return {"name": name}


@router.post("/upload")
def upload(file: UploadFile, title: str = Form(), extra: bytes = File(None)):
    return {"title": title}


@router.post("/login")
def login(form_data: Annotated[OAuth2PasswordRequestForm, Depends()]):
    return {"access_token": form_data.username}


@router.get("/me")
def me(user: Annotated[str, Depends(current_user)]) -> Message:
    return Message(message=user)


@router.get("/keys", dependencies=[Depends(api_key), Depends(bearer), Depends(basic)])
def keys():
    return {}


@router.get("/optional")
def optional(key: Annotated[str | None, Depends(optional_key)]):
    return {}


@router.get("/oidc")
def oidc_route(token: Annotated[str, Depends(oidc)], other: Annotated[str, Depends(computed_url)]):
    return {}


@router.get("/scoped")
def scoped(user: Annotated[str, Depends(scoped_user)]):
    return {}


@router.get("/search")
def search(
    q: Annotated[str | None, Query(alias="query")] = None,
    tags: list[str] = Query([]),
    color: Color = Color.red,
    size: Literal["s", "m", "l"] = "m",
    raw=None,
    user_agent: Annotated[str | None, Header()] = None,
    x_keep_under: str = Header(None, convert_underscores=False),
    session_id: Annotated[str | None, Cookie()] = None,
    page: int = Query(1, ge=1),
):
    return {}


@router.get("/deps/{item_id}")
def with_deps(page: Annotated[dict, Depends(common)], filters: Annotated[Filters, Depends()], owner: Annotated[str, Depends(item_owner)]):
    return {}


@router.get("/raw")
def raw(request: Request, background: Response):
    agent = request.headers["User-Agent"]
    lang = request.headers.get("Accept-Language")
    sort = request.query_params.get("sort")
    trace = deep_one(request)
    return {"agent": agent, "lang": lang, "sort": sort, "trace": trace}


@router.get("/reader")
def from_reader(token: Annotated[str | None, Depends(reader)]):
    return {}


@router.post("/webhook")
async def webhook(request: Request):
    payload = await request.json()
    return payload


@router.get("/escape")
def escape(request: Request):
    return dumped(request)


@router.get("/cycle")
def cycle(request: Request):
    return ping(request)


@router.get("/spread")
def spread_route(request: Request):
    return spread(request=request)


@router.get("/plain")
def plain(request: Request):
    return via_plain(request)


@router.get("/dynamic")
def dynamic(request: Request, name: str):
    return request.headers.get(name)


@router.get("/hidden", include_in_schema=False)
def hidden(q: int):
    return {}


@router.get("/nomodel", response_model=None)
def nomodel() -> Item:
    return crud.get_item(1)


@router.get("/stream")
def stream() -> StreamingResponse:
    return StreamingResponse(iter([b"x"]))


@router.delete("/items/{item_id}", status_code=204)
def delete_item(item_id: Annotated[int, Path(ge=1)]) -> None:
    return None


@router.get("/none")
def returns_none() -> None:
    return None


@router.get("/html", response_class=HTMLResponse)
def html():
    return "<p>hi</p>"


@router.get("/returned")
def returned():
    return crud.make_item()


@router.get("/returned-direct/{item_id}")
def returned_direct(item_id: int):
    return crud.get_item(item_id)


@router.get("/local")
def local():
    item = crud.make_message()
    return item


@router.get("/guarded/{item_id}")
def guarded(item_id: int):
    return crud.guarded(item_id)


@router.get("/try/{item_id}")
def tried(item_id: int):
    try:
        return crud.get_item(item_id)
    except HTTPException:
        raise HTTPException(status_code=status.HTTP_400_BAD_REQUEST)


@router.get("/ambiguous")
def ambiguous():
    return twice()


@router.get("/external")
def external():
    return jsonable_encoder({"a": 1})


@router.get("/unresolved")
def unresolved():
    return undefined_function()  # noqa: F821 - never defined on purpose


@router.get("/annotated-list")
def annotated_list() -> list[Item]:
    return []


@router.get("/computed")
def computed() -> Computed:
    return Computed(a=1)


@router.post("/request-model")
def request_model(request: ItemCreate):
    return {}


@router.get("/levels")
def levels(root: Annotated[int, Depends(level_root)]):
    return {}


@router.get("/lambda")
def lambda_dep(one: Annotated[int, Depends(level_one)]):
    return {}


@router.get("/joined")
def joined(a: Annotated[dict, Depends(pagination)], b: Annotated[dict, Depends(common)]):
    return {}


@router.get("/field")
def field_param(n: Annotated[int, Query(), Field(gt=0)] = 1):
    return {}


@router.put("/owners/{login}", response_model=Owner, status_code=status.HTTP_202_ACCEPTED)
def put_owner(login: Annotated[str, Path(min_length=2)], owner: Owner, x_request_id: Annotated[str | None, Header(alias="X-Request-ID")] = None):
    return owner


def item_check(item: ItemCreate) -> bool:
    return bool(item.name)


@router.post("/checked", response_model=Generated)
def checked(item: ItemCreate, ok: Annotated[bool, Depends(item_check)]):
    return Generated(VALUE=1)


@router.post("/direct", status_code=201, response_model=Message)
def direct(flag: bool = False):
    if flag:
        return JSONResponse({"detail": "accepted later"}, status_code=202)
    return Message(message="created")


@router.get("/status-param", response_model=Message)
def status_param(response: Response, slow: bool = False):
    if slow:
        response.status_code = 203
    return Message(message="ok")


@router.post("/serialized")
def serialized(body: Reshaped) -> Serialized:
    return Serialized(when=1)


@router.get("/caught/{item_id}")
def caught(item_id: int):
    try:
        helper_raise(item_id)
    except HTTPException:
        return {"caught": True}
    return {"caught": False}


def helper_raise(item_id: int) -> None:
    if item_id == 7:
        raise HTTPException(status_code=418)
