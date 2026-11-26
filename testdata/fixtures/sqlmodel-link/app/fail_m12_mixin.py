"""Expected import failure (Revision 3, L7/M12): the literal is in a non-table mixin."""

from typing import Optional

from sqlmodel import Field, SQLModel


class VersionMixin(SQLModel):
    VERSION = "v1"


class Build(VersionMixin, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
