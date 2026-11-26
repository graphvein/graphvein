import os

from fastapi import APIRouter, FastAPI

# N1: this project requires FastAPI >= 0.137.0, which serves a mount on an
# included APIRouter below the include's prefix, without the router's own
# prefix: /i/rm/s.
gateway = FastAPI()
router_app = FastAPI()


@router_app.get("/s")
def router_app_route():
    return {}


mounting = APIRouter(prefix="/p")
mounting.mount("/rm", router_app)
gateway.include_router(mounting, prefix="/i")

# N2: the same with a prefix that is not a constant: no path.
router_env_app = FastAPI()


@router_env_app.get("/se")
def router_env_route():
    return {}


mounting_env = APIRouter()
mounting_env.mount(os.environ["RM"], router_env_app)
gateway.include_router(mounting_env)

# N3: the pyproject.toml entrypoint names this app (fastapi run): it is
# served at its own root although the gateway mounts it.
app = FastAPI()


@app.get("/hello")
def hello():
    return {}


gateway.mount("/gw", app)
