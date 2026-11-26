"""Expected import failure. Row F5: a type the mapping does not know, without sa_type."""

from typing import Optional

from sqlmodel import Field, SQLModel


class Listed(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    tags: list[str] = Field(default_factory=list)
