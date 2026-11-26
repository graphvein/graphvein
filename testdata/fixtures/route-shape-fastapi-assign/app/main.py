"""default_response_class set by assignment (row SH-F24): on the app's
router and on a router before it is included. The routes then send 307;
no route of the project states its default success status or content
type. A route with an explicit response_class keeps its claim."""
from fastapi import APIRouter, FastAPI
from fastapi.responses import JSONResponse, RedirectResponse

app = FastAPI()
app.router.default_response_class = RedirectResponse

router = APIRouter()
router.default_response_class = RedirectResponse


@app.get("/go")
def go() -> str:
    return "/target"


@router.get("/hop")
def hop() -> str:
    return "/target"


@app.get("/target", response_class=JSONResponse)
def target() -> dict[str, int]:
    return {"ok": 1}


app.include_router(router)
