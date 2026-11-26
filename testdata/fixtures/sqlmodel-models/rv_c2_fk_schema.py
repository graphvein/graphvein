"""Revision 1 case: a schema-qualified foreign-key target (F4, R4)."""

from typing import Optional

from sqlmodel import Field, SQLModel


class Session(SQLModel, table=True):
    __tablename__ = "s"
    __table_args__ = ({"schema": "auth"},)
    id: Optional[int] = Field(default=None, primary_key=True)


class Token(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    session_id: Optional[int] = Field(default=None, foreign_key="auth.s.id")
