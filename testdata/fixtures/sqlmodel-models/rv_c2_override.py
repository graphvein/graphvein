"""Revision 1 case: the nearest definition wins wholesale (C2, R3)."""

from typing import Optional

from sqlmodel import Field, SQLModel


class CodeBase(SQLModel):
    code: str = Field(index=True, unique=True, max_length=5)


class Coded(CodeBase, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    code: str
