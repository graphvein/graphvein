import os

import uvicorn
from fastapi import APIRouter, FastAPI, routing
from starlette.routing import Mount
from starlette.staticfiles import StaticFiles

from . import local

app = FastAPI()


@app.get("/health")
def health():
    return {}


# M1: app.mount with a constant prefix. The sub-app answers only below the
# mount: /sub/sub/ping, never /sub/ping.
sub = FastAPI()


@sub.get("/sub/ping")
def sub_ping():
    return {}


app.mount("/sub", sub)

# M2: a mount with a prefix read from the environment: no path, the hop
# is stored in unresolved_mounts. (On an app of its own: a mount whose
# prefix is not read may take any later route of the app it is on, M19.)
env = FastAPI()


@env.get("/env/ping")
def env_ping():
    return {}


env_host = FastAPI()
env_host.mount(os.environ["SUB"], env)

# M3: include_router inside the mounted sub-app.
inner = FastAPI()
items = APIRouter(prefix="/r")


@items.get("/x")
def inner_x():
    return {}


inner.include_router(items, prefix="/inc")
app.mount("/m", inner)

# M4: nested mounts.
mid = FastAPI()
leaf = FastAPI()


@leaf.get("/leaf")
def leaf_route():
    return {}


mid.mount("/b", leaf)
app.mount("/a", mid)

# M5: a conditional mount.
admin = FastAPI()


@admin.get("/panel")
def admin_panel():
    return {}


if os.environ.get("ADMIN"):
    app.mount("/admin", admin)

# M6: Starlette Mount in the routes list of a FastAPI app: composed as
# app.mount would be, /lst/listed only.
listed = FastAPI()


@listed.get("/listed")
def listed_route():
    return {}


outer = FastAPI(routes=[Mount("/lst", app=listed)])

# M7: Mount through fastapi.routing, positional app argument: /lst2/listed2.
listed2 = FastAPI()


@listed2.get("/listed2")
def listed2_route():
    return {}


outer2 = FastAPI(routes=[routing.Mount("/lst2", listed2)])

# M8: app.router.mount is app.mount: /raw/raw only.
raw = FastAPI()


@raw.get("/raw")
def raw_route():
    return {}


app.router.mount("/raw", raw)

# M9: Mount of something that is not a FastAPI app, and a project class
# named Mount: neither consumes an app.
static = Mount("/static", app=StaticFiles(directory="static"))
kept = FastAPI()


@kept.get("/kept")
def kept_route():
    return {}


holder = local.Mount("/kept", kept)

# M10: an app only a test mounts keeps its own root.
standalone = FastAPI()


@standalone.get("/alone")
def alone():
    return {}


# M11: a routes-list Mount whose prefix is not a constant: no path, the hop
# is stored in unresolved_mounts.
listed_env = FastAPI()


@listed_env.get("/listed-env")
def listed_env_route():
    return {}


outer3 = FastAPI(routes=[Mount(os.environ["LST"], app=listed_env)])

# M12: a Mount in an APIRouter's routes list, the router included in an app:
# not composed (FastAPI's include_router copies no Mount), no path.
in_router = FastAPI()


@in_router.get("/in-router")
def in_router_route():
    return {}


mount_router = APIRouter(routes=[Mount("/mr", app=in_router)])
outer4 = FastAPI()
outer4.include_router(mount_router)

# M13: a routes list bound to a name first: not read, no path.
by_name = FastAPI()


@by_name.get("/by-name")
def by_name_route():
    return {}


named_routes = [Mount("/nm", app=by_name)]
outer5 = FastAPI(routes=named_routes)

# M14: x.router.mount with a prefix that is not a constant: the hop.
raw_env = FastAPI()


@raw_env.get("/raw-env")
def raw_env_route():
    return {}


raw_host = FastAPI()
raw_host.router.mount(os.environ["RAW"], raw_env)


# M15: a mount on an APIRouter the app includes. FastAPI serves it only
# from 0.137.0, and this project's manifest proves no version: no path.
router_mounted = FastAPI()


@router_mounted.get("/rs")
def router_mounted_route():
    return {}


mounting_router = APIRouter(prefix="/rp")
mounting_router.mount("/rm", router_mounted)
app.include_router(mounting_router, prefix="/ri")

# M16: a route added after a mount at its prefix is never reached (the
# mounted app answers, 404 included); one added before it, the prefix
# itself and a parameter segment below a literal prefix are.
@app.get("/sh/early")
def shadow_early():
    return {}


inc_before = APIRouter()


@inc_before.get("/sh/inc-before")
def shadow_inc_before():
    return {}


app.include_router(inc_before)
shadow_sub = FastAPI()


@shadow_sub.get("/a")
def shadow_sub_a():
    return {}


app.mount("/sh", shadow_sub)


@app.get("/sh/a")
def shadow_late_a():
    return {}


@app.get("/sh")
def shadow_prefix():
    return {}


@app.get("/sh/")
def shadow_prefix_slash():
    return {}


@app.get("/{v}/c")
def shadow_param():
    return {}


# M17: a route included after the mount is taken too.
inc_after = APIRouter()


@inc_after.get("/sh/inc-after")
def shadow_inc_after():
    return {}


app.include_router(inc_after)

# M18: a mount of something that is not a FastAPI app takes its prefix
# all the same.
files_app = FastAPI()
files_app.mount("/files", StaticFiles(directory="static"))


@files_app.get("/files/list")
def files_list():
    return {}


@files_app.get("/listing")
def files_listing():
    return {}


# M19: a mount whose prefix is not read may take any later path; an
# earlier route keeps its path.
dyn_app = FastAPI()


@dyn_app.get("/dyn/early")
def dyn_early():
    return {}


dyn_app.mount(os.environ["DYN"], FastAPI())


@dyn_app.get("/dyn/late")
def dyn_late():
    return {}


# M20: an app mounted and also served on its own by uvicorn.run under the
# main guard: its own-root path is kept, conditional.
served_sub = FastAPI()


@served_sub.get("/own")
def served_own():
    return {}


app.mount("/srv", served_sub)

# M21: an app mounted here and served only by a test's uvicorn.run: no
# own-root path (a test does not serve a production app).
served_by_test = FastAPI()


@served_by_test.get("/by-test")
def served_by_test_route():
    return {}


app.mount("/sbt", served_by_test)

if __name__ == "__main__":
    uvicorn.run(served_sub, host="127.0.0.1", port=8000)
