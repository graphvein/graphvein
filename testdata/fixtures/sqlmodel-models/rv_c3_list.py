"""Expected import failure. Revision 1 case: a generic list without sa_type (A2, F5)."""

from typing import Optional

from sqlmodel import Field, SQLModel


class Listing(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    values: list[int]
