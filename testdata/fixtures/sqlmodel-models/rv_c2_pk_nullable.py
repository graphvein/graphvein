"""Revision 1 case: an Optional primary key is not nullable (F2)."""

from typing import Optional

from sqlmodel import Field, SQLModel


class OptionalKey(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    other: Optional[int] = Field(default=None, nullable=False)
