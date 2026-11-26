"""Revision 1 derived case: inherited __table_args__ and sa_column used by one table, a plain mixin."""

from sqlalchemy import Column, Index, String

from typing import Optional

from sqlmodel import Field, SQLModel


class Stamp:
    pass


class SingleBase(SQLModel):
    __table_args__ = (Index("ix_single_code", "code"),)
    code: str
    note: Optional[str] = Field(default=None, sa_column=Column(String(10)))


class Single(Stamp, SingleBase, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
