"""Revision 1 derived case: int with a None default, and a JSON union."""

from sqlalchemy import JSON

from typing import Optional

from sqlmodel import Field, SQLModel


class IntNone(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    count: int = None
    value: int | str | None = Field(default=None, sa_type=JSON)
