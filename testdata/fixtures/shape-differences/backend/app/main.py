from fastapi import FastAPI, Request
from pydantic import BaseModel

app = FastAPI()


class ItemCreate(BaseModel):
    title: str
    description: str | None = None


class Address(BaseModel):
    city: str


class ProfileUpdate(BaseModel):
    name: str
    bio: str | None = None
    address: Address | None = None


# SD-10: the generated client's type agrees with the model.
@app.post("/w4/items/")
def create_item(item_in: ItemCreate):
    return {"title": item_in.title}


# SD-8: the client's type has `nickname`, the model does not.
@app.put("/w4/profile")
def update_profile(update: ProfileUpdate):
    return {"name": update.name}


# SD-7 and SD-8: the client's query type has `search`.
@app.get("/w4/items/")
def list_items(skip: int = 0, limit: int = 100):
    return []


# D: the body is read from the request, not declared (partial).
@app.post("/w4/notes")
async def create_note(request: Request):
    data = await request.json()
    return {"text": data["text"]}
