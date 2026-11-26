import os

from fastapi import APIRouter, FastAPI

from .settings import settings

app = FastAPI()

# UP-6, include_router(r, prefix=p): the prefix is read from settings.
items = APIRouter()


@items.get("/items")
def list_items():
    return []


@items.get("/items/{item_id}")
def get_item(item_id: int):
    return {}


@items.get("/files/{rest:path}")
def get_file(rest: str):
    return {}


# Not stored: the path "" right at the hop is the prefix itself (D).
@items.get("")
def items_root():
    return []


app.include_router(items, prefix=settings.api_prefix)

# UP-6, an included router's own prefix= that is not a constant, below
# the read include prefix /x (above "/x").
own = APIRouter(prefix=os.environ["OWN"])


@own.get("/own/status")
def own_status():
    return {}


app.include_router(own, prefix="/x")

# UP-6, mount(p, ...): a FastAPI sub-application mounted at a prefix read
# from the environment.
sub = FastAPI()


@sub.get("/sub/ping")
def sub_ping():
    return {}


app.mount(os.environ["SUB"], sub)

# UP-6, conditional: an include inside an if.
if os.environ.get("BETA"):
    beta = APIRouter()

    @beta.get("/beta/feature")
    def beta_feature():
        return {}

    app.include_router(beta, prefix=os.environ["BETA_PREFIX"])

# UP-9: a router included at a read prefix and at an unknown one keeps its
# known path and gets no entry.
shared = APIRouter()


@shared.get("/shared/item")
def shared_item():
    return {}


app.include_router(shared, prefix="/known")
app.include_router(shared, prefix=settings.api_prefix)

# UP-10: a router with an unread prefix that nothing includes.
orphan = APIRouter(prefix=os.environ["ORPHAN"])


@orphan.get("/orphan/page")
def orphan_page():
    return {}


@app.get("/health")
def health():
    return {}


# UP-9 per scope: included below a configured prefix here and
# at "/t" by tests/test_scoped.py: the route keeps its production entry.
scoped = APIRouter()


@scoped.get("/scoped/item")
def scoped_item():
    return {}


app.include_router(scoped, prefix=settings.api_prefix)

# RO-V1: a static mount at a configured URL before
# the routes of another app. The mount may take a later path or not: the
# paths are kept, conditional, with assumption RO-V1 naming it.
from fastapi.staticfiles import StaticFiles  # noqa: E402

site = FastAPI()
site.mount(os.environ["STATIC"], StaticFiles(directory="static"))
site_api = APIRouter()


@site_api.get("/items/list")
def site_items():
    return []


site.include_router(site_api, prefix="/api")


@site.get("/health/live")
def site_live():
    return {}
