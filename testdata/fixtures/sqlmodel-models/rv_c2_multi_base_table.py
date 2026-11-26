"""Revision 1 case: several bases, one of them a table class (C3, C6)."""

from typing import Optional

from sqlmodel import Field, SQLModel


class Mix:
    pass


class Root(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)


class Leaf(Mix, Root, table=True):
    extra: Optional[str] = None
