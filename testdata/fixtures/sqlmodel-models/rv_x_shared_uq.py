"""Revision 1 derived case: a shared unnamed UniqueConstraint imports."""

from sqlalchemy import UniqueConstraint

from typing import Optional

from sqlmodel import Field, SQLModel


class UniqueBase(SQLModel):
    __table_args__ = (UniqueConstraint("code"),)
    code: str


class FirstUnique(UniqueBase, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)


class SecondUnique(UniqueBase, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
