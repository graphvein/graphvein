"""Not imported by the app (the oracle needs no sqlmodel): a SQLModel
table class used as a body (its schema is not stated)."""
from fastapi import APIRouter
from sqlmodel import Field, SQLModel

router = APIRouter()


class Hero(SQLModel, table=True):
    id: int | None = Field(default=None, primary_key=True)
    name: str


@router.post("/heroes")
def create_hero(hero: Hero) -> Hero:
    return hero
