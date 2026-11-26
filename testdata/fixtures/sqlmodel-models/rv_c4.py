"""Revision 1 case c4 (A5, A1m): aliases with and without Field metadata."""

from typing import Annotated, Optional

from sqlmodel import Field, SQLModel

Pk = Annotated[int, Field(primary_key=True)]
OptStr = Optional[str]


class Aliased(SQLModel, table=True):
    id: Pk
    name: OptStr = None
