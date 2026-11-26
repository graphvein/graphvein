from fastapi import FastAPI

from app import items

app = FastAPI()
app.include_router(items.router, prefix="/v1")
app.include_router(items.router, prefix="/v2")


@app.get("/health")
def health():
    return "ok"
