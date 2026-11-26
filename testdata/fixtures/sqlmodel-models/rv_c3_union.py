"""Expected import failure. Revision 1 case: a union of two types without sa_type (A1, F5)."""

from typing import Optional

from sqlmodel import Field, SQLModel


class Either(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    value: int | str
