"""Expected import failure (Revision 3, L7): no primary key, and __table_args__ holds none."""

from sqlalchemy import UniqueConstraint
from sqlmodel import SQLModel


class Unkeyed(SQLModel, table=True):
    __table_args__ = (UniqueConstraint("code", name="uq_unkeyed_code"),)
    code: str
