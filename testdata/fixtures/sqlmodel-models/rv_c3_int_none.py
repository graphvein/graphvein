"""Revision 1 case: a string annotation with a None default (A3)."""

from typing import Optional

from sqlmodel import Field, SQLModel


class Counted(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    count: "int" = None
