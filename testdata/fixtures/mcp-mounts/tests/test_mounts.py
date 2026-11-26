from fastapi import FastAPI

from app import items

test_app = FastAPI()
test_app.include_router(items.router, prefix="/testing")


def test_items():
    assert test_app
