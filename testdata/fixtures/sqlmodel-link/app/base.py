"""A chain class in another module (rows C2, L2)."""

from datetime import datetime
from typing import Optional

from sqlmodel import Field, SQLModel


class TimestampBase(SQLModel):
    created: Optional[datetime] = None
    label: str = Field(default="new", max_length=20)
