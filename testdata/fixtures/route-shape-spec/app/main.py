from fastapi import Depends, FastAPI, Header, Query
from fastapi.security import HTTPBearer
from pydantic import BaseModel

app = FastAPI()
bearer = HTTPBearer(bearerFormat="JWT")


class Item(BaseModel):
    name: str
    price: float


# OpenAPI 3.0 (openapi30.yaml): typed parameters the spec contradicts.
@app.get("/v30/items/{item_id}")
def read_item(item_id: int, q: int | None = None):
    return {"item_id": item_id}


# Body and responses: the code answers 201, the spec documents 200.
@app.post("/v30/items", status_code=201)
def create_item(item: Item) -> Item:
    return item


# No parameters in the code; the spec declares a query parameter and a
# request body.
@app.get("/v30/plain")
def plain():
    return {}


# The code requires a bearer token; the spec says no authentication.
@app.get("/v30/secure")
def secure(token=Depends(bearer)):
    return {}


# The spec names its scheme HTTPBearer too: the mapped one is renamed.
@app.get("/v30/secure2")
def secure2(token=Depends(bearer)):
    return {}


# OpenAPI 3.1 (openapi31.json).
@app.put("/v31/things/{thing_id}")
def put_thing(thing_id: str, item: Item, mode: str | None = None):
    return {}


@app.get("/v31/auth")
def auth_variants():
    return {}


# Swagger 2.0 (swagger.yaml).
@app.post("/v2/pets")
def add_pet(item: Item):
    return {}


@app.post("/v2/pets/{pet_id}/photo")
def upload_photo(pet_id: int):
    return {}


@app.get("/v2/pets")
def list_pets(tags: list[str] | None = Query(None), status: str = "available", x_request_id: str = Header()):
    return []


# Documented by openapi30.yaml and swagger.yaml: no mapped items.
@app.get("/dup")
def dup():
    return {}


# Fields marked readOnly or writeOnly, an enum number float64 cannot
# hold; the code reads nothing.
@app.post("/v30/accounts")
def create_account():
    return {}


# The code reads a JSON body; the spec names it with a charset parameter
# or as any media type: no "spec differs" note.
@app.post("/v30/charset")
def charset(item: Item):
    return {}


@app.post("/v30/wild")
def wild(item: Item):
    return {}
