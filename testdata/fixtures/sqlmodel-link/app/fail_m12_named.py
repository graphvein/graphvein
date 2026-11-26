"""Expected import failure (Revision 3, L7/M12): a non-annotated literal after __tablename__."""

from typing import Optional

from sqlmodel import Field, SQLModel


class Release(SQLModel, table=True):
    __tablename__ = "releases"
    id: Optional[int] = Field(default=None, primary_key=True)
    VERSION = 3
