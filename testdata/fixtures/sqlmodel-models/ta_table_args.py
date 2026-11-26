"""__table_args__ elements: rows M11, TA1, TA2, TA3 and TA-D."""

from typing import Optional

from sqlalchemy import CheckConstraint, Index, PrimaryKeyConstraint, UniqueConstraint, text
from sqlalchemy.sql.naming import conv
from sqlmodel import Field, SQLModel

CHECK_NAME = "ck_args_dynamic"


class Args(SQLModel, table=True):
    __table_args__ = (
        UniqueConstraint("a", "b", name="uq_args_a_b"),
        Index("ix_args_a", "a", unique=True, postgresql_where=text("a > 0")),
        CheckConstraint("a > 0", name="ck_args_a"),
        CheckConstraint("b > 0", name=conv("ck_args_b")),
        CheckConstraint("b < 100", name=CHECK_NAME),
    )
    id: Optional[int] = Field(default=None, primary_key=True)
    a: int
    b: int


class OptionsDict(SQLModel, table=True):
    __table_args__ = {"comment": "options only"}
    id: Optional[int] = Field(default=None, primary_key=True)


class Composite(SQLModel, table=True):
    __table_args__ = (PrimaryKeyConstraint("left", "right"), {"comment": "trailing options"})
    left: int = Field(primary_key=True)
    right: int = Field(primary_key=True)
