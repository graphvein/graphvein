"""Column(...) arguments inside sa_column: rows K1-K6 and T1, including PF1."""

import datetime
from typing import Optional

import sqlalchemy as sa
from sqlalchemy import JSON, Column, DateTime, ForeignKey, Integer, Sequence, String, Text, func, text
from sqlalchemy.types import TypeDecorator
from sqlmodel import Field, SQLModel
from sqlmodel.sql.sqltypes import AutoString

JsonVariant = JSON().with_variant(JSON(), "postgresql")
SIZE = 40


class Trimmed(TypeDecorator):
    impl = String
    cache_ok = True


class Parent(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)


class Columns(SQLModel, table=True):
    id: Optional[int] = Field(default=None, sa_column=Column(Integer, primary_key=True))
    renamed: Optional[str] = Field(default=None, sa_column=Column("renamed_col", String(30), nullable=True, index=True))
    parent_id: Optional[int] = Field(default=None, sa_column=Column(ForeignKey("parent.id", ondelete="CASCADE"), nullable=True))
    typed_fk: Optional[int] = Field(default=None, sa_column=Column(Integer, ForeignKey("parent.id"), unique=False))
    variant: Optional[dict] = Field(default=None, sa_column=Column(JsonVariant, nullable=True))
    trimmed: Optional[str] = Field(default=None, sa_column=Column(Trimmed(), nullable=True))
    sized: Optional[str] = Field(default=None, sa_column=Column(String(SIZE), nullable=True))
    created: Optional[datetime.datetime] = Field(default=None, sa_column=Column(DateTime(timezone=True), server_default=func.now(), onupdate=func.now()))
    literal_default: Optional[str] = Field(default=None, sa_column=Column(Text, server_default=text("'x'"), default=lambda: "y"))
    noted: Optional[str] = Field(default=None, sa_column=Column(AutoString(16), comment="a note", nullable=True))
    sequenced: Optional[int] = Field(default=None, sa_column=Column(Integer, Sequence("seq_x"), nullable=True))
    keyed: Optional[str] = Field(default=None, sa_column=Column(String, key="keyed_attr", nullable=True))
    spelled: Optional[str] = Field(default=None, sa_column=sa.Column(sa.Unicode(8), nullable=True))
