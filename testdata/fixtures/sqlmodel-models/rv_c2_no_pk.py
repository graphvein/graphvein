"""Expected import failure. Revision 1 case: a complete column set without a primary key."""

from sqlmodel import SQLModel


class NoKey(SQLModel, table=True):
    name: str
