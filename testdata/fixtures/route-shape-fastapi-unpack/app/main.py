"""The plain app: its own routes and the plain router keep FastAPI's
defaults; the routers that include or are included into unpacked objects
do not."""
from fastapi import FastAPI

from app.responses import CreatedJSON
from app.routers import plain, unpacked
from app.sub import sub

app = FastAPI()


@app.get("/root")
def read_root() -> dict[str, str]:
    return {"app": "plain"}


app.include_router(plain.router)
app.include_router(plain.shared)
app.include_router(plain.styled, prefix="/styled", default_response_class=CreatedJSON)
app.include_router(unpacked.router, prefix="/unpacked")
app.mount("/sub", sub)
