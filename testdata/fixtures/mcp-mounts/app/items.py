from fastapi import APIRouter

router = APIRouter()


@router.get("/items")
def list_items():
    return []


@router.get("/items/{item_id}")
def read_item(item_id: int):
    return {"id": item_id}
