"""A primary key declared in __table_args__ (Revision 3, L7): the tables import."""

from typing import Optional

from sqlalchemy import Column, Integer, PrimaryKeyConstraint
from sqlmodel import Field, SQLModel


class Pair(SQLModel, table=True):
    __table_args__ = (PrimaryKeyConstraint("left", "right"),)
    left: int
    right: int
    note: Optional[str] = None


class Slot(SQLModel, table=True):
    __table_args__ = (PrimaryKeyConstraint("slot"),)
    slot: int = Field(sa_column=Column(Integer))
    size: int = Field(sa_column=Column(Integer))
