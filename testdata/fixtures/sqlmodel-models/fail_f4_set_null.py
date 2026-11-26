"""Expected import failure. Row F4: ondelete="SET NULL" on a column that is not nullable."""

from typing import Optional

from sqlmodel import Field, SQLModel


class Target(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)


class Pointer(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    target_id: int = Field(foreign_key="target.id", ondelete="SET NULL")
