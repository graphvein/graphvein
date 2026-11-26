"""Revision 1 case: max_length=0 gives no length (F5)."""

from typing import Optional

from sqlmodel import Field, SQLModel


class Unbounded(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    text: str = Field(max_length=0)
