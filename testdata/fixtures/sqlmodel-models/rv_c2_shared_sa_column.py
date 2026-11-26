"""Expected import failure. Revision 1 case: a chain sa_column effective in two tables."""

from sqlalchemy import Column, String

from typing import Optional

from sqlmodel import Field, SQLModel


class SharedBase(SQLModel):
    note: Optional[str] = Field(default=None, sa_column=Column(String(10)))


class FirstShared(SharedBase, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)


class SecondShared(SharedBase, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
