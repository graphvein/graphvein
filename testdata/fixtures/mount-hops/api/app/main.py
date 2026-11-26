import os

from fastapi import APIRouter, FastAPI

PREFIX = os.environ.get("PREFIX", "/v1")

app = FastAPI()
items = APIRouter()


@items.get("/items")
def list_items():
    return []


@items.get("/items/{item_id:int}")
def get_item(item_id: int):
    return {}


app.include_router(items, prefix=PREFIX)

own = APIRouter(prefix=os.environ["OWN"])


@own.get("/")
def own_root():
    return {}


@own.get("/status")
def own_status():
    return {}


app.include_router(own)

# A router prefix below a constant include prefix: /x is read above it.
own2 = APIRouter(prefix=os.environ["OWN2"])


@own2.get("/s2")
def own2_s2():
    return {}


app.include_router(own2, prefix="/x")


@app.get("/health")
def health():
    return {}
