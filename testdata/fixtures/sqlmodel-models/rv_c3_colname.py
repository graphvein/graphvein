"""Revision 1 case: a Column's written name differs from the attribute (K1)."""

from sqlalchemy import Column, String

from typing import Optional

from sqlmodel import Field, SQLModel


class Renamed(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    attr: Optional[str] = Field(default=None, sa_column=Column("db_name", String(8)))
