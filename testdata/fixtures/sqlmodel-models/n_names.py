"""Table names: rows N1, N2 and the N3 forms."""

from typing import Optional

from sqlmodel import Field, SQLModel

PREFIX = "app_"


class Named(SQLModel, table=True):
    __tablename__ = "named_table"
    id: Optional[int] = Field(default=None, primary_key=True)


class DefaultName(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)


class Computed(SQLModel, table=True):
    __tablename__ = PREFIX + "computed"
    id: Optional[int] = Field(default=None, primary_key=True)


class TableBase(SQLModel):
    __tablename__ = "inherited_name"
    code: str


class Inherits(TableBase, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)


class Größe(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
