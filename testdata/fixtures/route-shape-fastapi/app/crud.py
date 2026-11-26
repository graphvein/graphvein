from fastapi import HTTPException, status

from app.models import Item, Message


def get_item(item_id: int) -> Item:
    if item_id < 0:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="not found")
    return Item(id=item_id, name="x", note=None)


def build_item():
    return Item(id=1, name="built", note=None)


def make_item():
    return build_item()


def make_message():
    return Message(message="hi")


def guarded(item_id: int):
    try:
        if item_id == 0:
            raise HTTPException(status_code=409)
    except ValueError:
        pass
    return get_item(item_id)
