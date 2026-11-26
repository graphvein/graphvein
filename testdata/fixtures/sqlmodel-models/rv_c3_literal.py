"""Expected import failure. Revision 1 case: a Literal annotation without sa_type (A2, F5)."""

from typing import Literal

from typing import Optional

from sqlmodel import Field, SQLModel


class Chosen(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    choice: Literal["a", "b"]
