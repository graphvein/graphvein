"""Row U2: SQLAlchemy declarative members inside a SQLModel class."""

from typing import Optional

from sqlalchemy import Column, Integer, Table
from sqlalchemy.orm import declared_attr
from sqlmodel import Field, SQLModel


class Hybrid(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)

    @declared_attr
    def __tablename__(cls) -> str:
        return "hybrid"


class Explicit(SQLModel):
    __table__ = Table("explicit", SQLModel.metadata, Column("id", Integer, primary_key=True))
