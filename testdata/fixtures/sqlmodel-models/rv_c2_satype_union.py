"""Revision 1 case: a union with sa_type imports and is nullable (A1)."""

from sqlalchemy import JSON

from typing import Optional

from sqlmodel import Field, SQLModel


class Unioned(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    value: int | str | None = Field(default=None, sa_type=JSON)
