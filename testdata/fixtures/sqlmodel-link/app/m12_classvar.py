"""A literal assigned to a base's ClassVar imports (Revision 3, M12 without P1)."""

from typing import ClassVar, Optional

from sqlmodel import Field, SQLModel


class Versioned(SQLModel):
    VERSION: ClassVar[int] = 1


class Patch(Versioned, table=True):
    VERSION = 2
    id: Optional[int] = Field(default=None, primary_key=True)
