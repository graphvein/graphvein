"""Row C4: a class keyword other than table (registry=)."""

from typing import Optional

from sqlalchemy.orm import registry
from sqlmodel import Field, SQLModel

other = registry()


class Registered(SQLModel, registry=other, table=True):
    id: Optional[int] = Field(default=None, primary_key=True)
