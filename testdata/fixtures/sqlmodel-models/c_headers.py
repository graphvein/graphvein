"""Class headers: rows C1, C1b, C1c, C2, C5, C6 and the conditional and decorated D forms."""

from typing import Optional

from sqlmodel import Field, SQLModel

FLAG = True


def keep(cls):
    return cls


class HeroBase(SQLModel):
    name: str = Field(index=True)
    model_config = {"validate_assignment": True}


class Hero(HeroBase, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)


class HeroPublic(HeroBase):
    id: int


class Plain(SQLModel, table=False):
    note: str


class Flagged(SQLModel, table=FLAG):
    id: Optional[int] = Field(default=None, primary_key=True)


class Mixin:
    pass


class Mixed(Mixin, SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)


class Configured(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)

    class Config:
        arbitrary_types_allowed = True


if FLAG:

    class Maybe(SQLModel, table=True):
        id: Optional[int] = Field(default=None, primary_key=True)


@keep
class Decorated(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
