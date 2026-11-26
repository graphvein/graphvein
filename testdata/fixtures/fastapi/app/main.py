import fastapi
from fastapi.applications import FastAPI as App
from app.api.routes import items, exported
from app.prefixes import PREFIX
from app.config import settings, overridden

app = App()
sub = fastapi.FastAPI()
app.include_router(items.router, prefix=settings.API)
app.include_router(exported, prefix=PREFIX)
if enabled:
    app.include_router(items.router, prefix="/conditional")
app.include_router(items.router, prefix="/conditional")
if enabled:
    app.include_router(items.router, prefix="/private")
app.include_router(items.router, prefix=f"/{dynamic}")
app.include_router(items.router, prefix=overridden.API)
app.include_router(unknown)
app.mount("/mounted", sub)
app.mount("/static", object())

@app.get("/")
@app.post("/stack")
def direct():
    return 1

@app.get("/")
def repeated():
    pass

@sub.get("/sub")
def subhandler():
    pass

app.add_api_route("/named", direct, methods=("put", "PUT"))
app.add_api_route(path="/inline", endpoint=lambda: None)
app.add_api_route("/unknown", unavailable)

@app.api_route("/empty", methods=[])
def empty():
    pass

@app.api_route("/none", methods=None)
def none():
    pass

@app.get(dynamic)
def dynamic_path():
    pass

@app.api_route("/dynamic-methods", methods=METHODS)
def dynamic_methods():
    pass

@app.websocket("/socket")
@app.websocket_route("/socket2")
def socket():
    pass

def factory():
    local = fastapi.FastAPI()
    local.include_router(items.router, prefix="/factory")
    @local.get("/local" "/path")
    def localhandler():
        pass
    def nested():
        @local.get("/outside-owner")
        def ignored():
            pass
    return local

class APIRouter:
    def get(self, path):
        pass

fake = APIRouter()
@fake.get("/fake")
def fakehandler():
    pass

kw = fastapi.APIRouter(**router_options)
app.include_router(kw)
app.include_router(items.router, **include_options)

@kw.get("/unpack")
def unpacked():
    pass

@app.api_route("/unpack-methods", **route_options)
def unpacked_methods():
    pass

@app.api_route("/empty-tuple", methods=())
def empty_tuple():
    pass

@app.api_route("/empty-set-call", methods=set())
def empty_set_call():
    pass

app.add_api_route("/add-empty-list", direct, methods=[])
app.add_api_route("/add-empty-tuple", direct, methods=())
app.add_api_route("/add-empty-set-call", direct, methods=set())
