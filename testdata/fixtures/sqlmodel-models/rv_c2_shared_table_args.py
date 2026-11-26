"""Expected import failure. Revision 1 case: chain __table_args__ effective in two tables."""

from sqlalchemy import Index

from typing import Optional

from sqlmodel import Field, SQLModel


class ArgsBase(SQLModel):
    __table_args__ = (Index("ix_shared_code", "code"),)
    code: str


class FirstArgs(ArgsBase, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)


class SecondArgs(ArgsBase, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
