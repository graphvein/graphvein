"""SQLAlchemy declarative classes (row U1): both Registry and Account are counted."""

from sqlalchemy import Integer
from sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column


class Registry(DeclarativeBase):
    pass


class Account(Registry):
    __tablename__ = "accounts"
    id: Mapped[int] = mapped_column(Integer, primary_key=True)
