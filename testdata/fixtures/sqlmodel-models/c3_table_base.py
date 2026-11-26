"""Row C3: a class whose base is a table class is not mapped as a table (no import failure)."""

from typing import Optional

from sqlmodel import Field, SQLModel


class Parent(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)


class Child(Parent, table=True):
    extra: str
