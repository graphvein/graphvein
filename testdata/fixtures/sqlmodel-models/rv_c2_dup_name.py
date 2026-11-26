"""Expected import failure. Revision 1 case: two tables with the same name."""

from typing import Optional

from sqlmodel import Field, SQLModel


class First(SQLModel, table=True):
    __tablename__ = "same"
    id: Optional[int] = Field(default=None, primary_key=True)


class Second(SQLModel, table=True):
    __tablename__ = "same"
    id: Optional[int] = Field(default=None, primary_key=True)
