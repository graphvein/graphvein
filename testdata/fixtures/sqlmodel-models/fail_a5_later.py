"""Expected import failure. Row A5: an alias bound after the class that uses it is A3."""

from typing import Optional

from sqlmodel import Field, SQLModel


class Early(SQLModel, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
    later: "LaterType" = None


LaterType = Optional[int]
