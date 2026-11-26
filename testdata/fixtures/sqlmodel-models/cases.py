"""Investigation synthetic cases: Field defaults with sa_column (PF4) and None defaults (PF5)."""

from sqlalchemy import Column, String

from typing import Optional

from sqlmodel import Field, SQLModel


class Cases(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    shadowed: str = Field(default="x", sa_column=Column(String(4)))
    none_int: int = None
    none_default: Optional[str] = Field(default=None)
