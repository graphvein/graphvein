"""Expected import failure: a keyword outside sqlmodel.Field's signature (rule P1, L7)."""

from typing import Optional

from sqlmodel import Field, SQLModel


class Broken(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    code: str = Field(pattern="^[a-z]+$")
