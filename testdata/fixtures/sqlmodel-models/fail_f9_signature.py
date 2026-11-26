"""Expected import failure. Row F9: a keyword outside sqlmodel.Field's signature."""

from typing import Optional

from sqlmodel import Field, SQLModel


class Patterned(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    code: str = Field(pattern="^a")
