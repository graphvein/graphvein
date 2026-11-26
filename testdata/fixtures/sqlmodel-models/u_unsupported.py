"""Recognized but unsupported forms: rows U1, U3 and U4."""

from sqlalchemy import Column, Integer, MetaData, Table
from sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column, registry

metadata = MetaData()

events = Table("events", metadata, Column("id", Integer, primary_key=True))


class Base(DeclarativeBase):
    pass


class Declared(Base):
    __tablename__ = "declared"
    id: Mapped[int] = mapped_column(primary_key=True)


class Imperative:
    pass


mapper_registry = registry()
mapper_registry.map_imperatively(Imperative, events)
