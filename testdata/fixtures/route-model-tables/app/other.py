from pydantic import BaseModel


# Another class named Hero in another file: not the table's model.
class Hero(BaseModel):
    id: int
    name: str
